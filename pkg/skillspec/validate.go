package skillspec

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
	"gopkg.in/yaml.v3"
)

// Limits from the Agent Skills specification.
const (
	MaxNameLength          = 64
	MaxDescriptionLength   = 1024
	MaxCompatibilityLength = 500
	// RecommendedMaxLines is the spec's recommended length of SKILL.md.
	RecommendedMaxLines = 500
)

// allowedFields are the top-level frontmatter keys the spec defines.
var allowedFields = []string{"allowed-tools", "compatibility", "description", "license", "metadata", "name"}

// Result is the outcome of Validate. Errors make a skill invalid; warnings
// are the spec's recommendations and leave it valid.
type Result struct {
	Errors   []Violation
	Warnings []Violation
}

// Valid reports whether the skill has no errors.
func (r Result) Valid() bool {
	return len(r.Errors) == 0
}

// Validate checks the skill in dir against every rule of the spec.
func Validate(dir string) Result {
	var res Result
	doc, err := readDocument(dir)
	if err != nil {
		var v Violation
		if !errors.As(err, &v) {
			v = Violation{FieldSkillMD, RuleRead, err.Error()}
		}
		res.Errors = append(res.Errors, v)
		return res
	}

	for _, key := range doc.keys {
		if !isAllowedField(key) {
			res.Errors = append(res.Errors, Violation{key, RuleUnknownField,
				fmt.Sprintf("unknown field (allowed: %s)", strings.Join(allowedFields, ", "))})
		}
	}

	if node, ok := doc.fields["name"]; !ok {
		res.Errors = append(res.Errors, Violation{"name", RuleRequired, "missing required field"})
	} else {
		res.Errors = append(res.Errors, validateName(node, dir)...)
	}

	if node, ok := doc.fields["description"]; !ok {
		res.Errors = append(res.Errors, Violation{"description", RuleRequired, "missing required field"})
	} else {
		res.Errors = append(res.Errors, validateDescription(node)...)
	}

	if node, ok := doc.fields["compatibility"]; ok {
		res.Errors = append(res.Errors, validateCompatibility(node)...)
	}

	res.Warnings = warnings(doc)
	return res
}

// warnings reports the spec's recommendations and the type rules skills-ref
// does not enforce: a non-string license, allowed-tools or metadata value
// breaks the spec text, but skills-ref accepts the skill, so the verdict stays
// valid.
func warnings(doc *document) []Violation {
	var ws []Violation
	if doc.lines > RecommendedMaxLines {
		ws = append(ws, Violation{FieldSkillMD, RuleLineCount,
			fmt.Sprintf("has %d lines, the spec recommends at most %d", doc.lines, RecommendedMaxLines)})
	}
	for _, key := range []string{"license", "allowed-tools"} {
		if node, ok := doc.fields[key]; ok && node.Kind != yaml.ScalarNode {
			ws = append(ws, Violation{key, RuleType, "should be a string"})
		}
	}
	if node, ok := doc.fields["metadata"]; ok {
		switch {
		case node.Kind == yaml.ScalarNode && node.Tag == "!!null":
			// "metadata:" with no value is an empty mapping.
		case node.Kind != yaml.MappingNode:
			ws = append(ws, Violation{"metadata", RuleType, "should be a mapping of string keys to string values"})
		default:
			for i := 0; i+1 < len(node.Content); i += 2 {
				if node.Content[i+1].Kind != yaml.ScalarNode {
					ws = append(ws, Violation{"metadata." + node.Content[i].Value, RuleStringValues, "should be a string"})
				}
			}
		}
	}
	return ws
}

func isAllowedField(key string) bool {
	i := sort.SearchStrings(allowedFields, key)
	return i < len(allowedFields) && allowedFields[i] == key
}

func validateName(node *yaml.Node, dir string) []Violation {
	if node.Kind != yaml.ScalarNode {
		return []Violation{{"name", RuleType, "must be a string"}}
	}
	name := norm.NFKC.String(strings.TrimSpace(node.Value))
	if name == "" {
		return []Violation{{"name", RuleNonEmpty, "must not be empty"}}
	}

	var vs []Violation
	if n := utf8.RuneCountInString(name); n > MaxNameLength {
		vs = append(vs, Violation{"name", RuleMaxLength,
			fmt.Sprintf("must be at most %d characters (got %d)", MaxNameLength, n)})
	}
	if name != strings.ToLower(name) {
		vs = append(vs, Violation{"name", RuleLowercase, fmt.Sprintf("must be lowercase (got %q)", name)})
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		vs = append(vs, Violation{"name", RuleHyphenEdge, "must not start or end with a hyphen"})
	}
	if strings.Contains(name, "--") {
		vs = append(vs, Violation{"name", RuleConsecutiveHyphens, "must not contain consecutive hyphens"})
	}
	for _, r := range name {
		if r != '-' && !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			vs = append(vs, Violation{"name", RuleCharset,
				fmt.Sprintf("may contain only letters, digits and hyphens (got %q)", name)})
			break
		}
	}
	if base := dirName(dir); base != name {
		vs = append(vs, Violation{"name", RuleDirectoryMatch,
			fmt.Sprintf("must match the directory name %q (got %q)", base, name)})
	}
	return vs
}

// dirName is the NFKC-normalized base name of dir, resolved to an absolute
// path first so that "." and "skill/" name the directory they point at.
func dirName(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return norm.NFKC.String(filepath.Base(dir))
}

func validateDescription(node *yaml.Node) []Violation {
	if node.Kind != yaml.ScalarNode {
		return []Violation{{"description", RuleType, "must be a string"}}
	}
	if strings.TrimSpace(node.Value) == "" {
		return []Violation{{"description", RuleNonEmpty, "must not be empty"}}
	}
	if n := utf8.RuneCountInString(node.Value); n > MaxDescriptionLength {
		return []Violation{{"description", RuleMaxLength,
			fmt.Sprintf("must be at most %d characters (got %d)", MaxDescriptionLength, n)}}
	}
	return nil
}

func validateCompatibility(node *yaml.Node) []Violation {
	if node.Kind != yaml.ScalarNode {
		return []Violation{{"compatibility", RuleType, "must be a string"}}
	}
	if n := utf8.RuneCountInString(node.Value); n > MaxCompatibilityLength {
		return []Violation{{"compatibility", RuleMaxLength,
			fmt.Sprintf("must be at most %d characters (got %d)", MaxCompatibilityLength, n)}}
	}
	return nil
}
