// Package skillspec implements the Agent Skills specification
// (https://agentskills.io/specification): it parses a skill directory,
// validates it with the same verdict as the reference validator skills-ref,
// and computes a deterministic hash of its content.
//
// The frontmatter is read the way skills-ref reads it, with strict YAML
// semantics: every scalar is a string, and flow style, anchors, aliases, tags
// and duplicate keys are errors.
package skillspec

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Field names used in violations, besides the frontmatter keys themselves.
const (
	FieldDirectory = "directory"
	FieldSkillMD   = "SKILL.md"
)

// Rules a violation can name.
const (
	RuleDirectory          = "directory"
	RuleRequiredFile       = "required-file"
	RuleRead               = "read"
	RuleFrontmatter        = "frontmatter"
	RuleYAML               = "yaml"
	RuleUnknownField       = "unknown-field"
	RuleRequired           = "required"
	RuleType               = "type"
	RuleNonEmpty           = "non-empty"
	RuleMaxLength          = "max-length"
	RuleLowercase          = "lowercase"
	RuleHyphenEdge         = "hyphen-edge"
	RuleConsecutiveHyphens = "consecutive-hyphens"
	RuleCharset            = "charset"
	RuleDirectoryMatch     = "directory-match"
	RuleLineCount          = "line-count"
	RuleStringValues       = "string-values"
)

// Violation is one broken rule: the field it concerns, the rule's name and a
// message for people.
type Violation struct {
	Field   string
	Rule    string
	Message string
}

func (v Violation) Error() string {
	return v.Field + ": " + v.Message
}

// MetadataRequiresKey is the metadata key that lists the MCP servers a skill
// needs, separated by whitespace.
const MetadataRequiresKey = "mcpfleet-requires"

// Skill is a parsed skill directory.
type Skill struct {
	Name          string
	Description   string
	License       string
	Compatibility string
	AllowedTools  string
	// Metadata holds the string values of the metadata mapping; a value that
	// is not a string is left out (Validate warns about it).
	Metadata map[string]string
	// Extra keeps every top-level frontmatter key the spec does not define,
	// so nothing is lost on a round-trip.
	Extra map[string]yaml.Node
	// Requires lists the MCP servers from metadata.mcpfleet-requires.
	Requires []string
	// Body is the markdown after the frontmatter, trimmed.
	Body string
	// SHA256 is Hash of the skill directory.
	SHA256 string
}

// document is SKILL.md split into frontmatter fields and body.
type document struct {
	keys   []string
	fields map[string]*yaml.Node
	body   string
	lines  int
}

// Parse reads the skill in dir. It fails with a Violation when SKILL.md is
// missing or its frontmatter cannot be read, and with Hash's error when the
// directory cannot be hashed; it does not check the field rules, which is
// Validate's job.
func Parse(dir string) (*Skill, error) {
	doc, err := readDocument(dir)
	if err != nil {
		return nil, err
	}
	s := &Skill{Body: doc.body}
	for _, key := range doc.keys {
		node := doc.fields[key]
		switch key {
		case "name":
			s.Name = strings.TrimSpace(scalar(node))
		case "description":
			s.Description = strings.TrimSpace(scalar(node))
		case "license":
			s.License = scalar(node)
		case "compatibility":
			s.Compatibility = scalar(node)
		case "allowed-tools":
			s.AllowedTools = scalar(node)
		case "metadata":
			s.Metadata = stringMap(node)
		default:
			if s.Extra == nil {
				s.Extra = map[string]yaml.Node{}
			}
			s.Extra[key] = *node
		}
	}
	if req, ok := s.Metadata[MetadataRequiresKey]; ok {
		s.Requires = strings.Fields(req)
	}
	if s.SHA256, err = Hash(dir); err != nil {
		return nil, err
	}
	return s, nil
}

// scalar returns the value of a scalar node, "" for anything else.
func scalar(n *yaml.Node) string {
	if n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

// stringMap returns the scalar values of a mapping node, nil when the node is
// not a mapping or holds no scalar value.
func stringMap(n *yaml.Node) map[string]string {
	if n.Kind != yaml.MappingNode {
		return nil
	}
	var m map[string]string
	for i := 0; i+1 < len(n.Content); i += 2 {
		v := n.Content[i+1]
		if v.Kind != yaml.ScalarNode {
			continue
		}
		if m == nil {
			m = map[string]string{}
		}
		m[n.Content[i].Value] = v.Value
	}
	return m
}

// findSkillMD returns the path of SKILL.md in dir, or of skill.md when only
// that exists, as skills-ref does.
func findSkillMD(dir string) (string, error) {
	info, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return "", Violation{FieldDirectory, RuleDirectory, fmt.Sprintf("path does not exist: %s", dir)}
	}
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", Violation{FieldDirectory, RuleDirectory, fmt.Sprintf("not a directory: %s", dir)}
	}
	for _, name := range []string{"SKILL.md", "skill.md"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", Violation{FieldSkillMD, RuleRequiredFile, "missing required file SKILL.md"}
}

func readDocument(dir string) (*document, error) {
	path, err := findSkillMD(dir)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := strings.ReplaceAll(string(raw), "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	lines := strings.Count(content, "\n")
	if content != "" && !strings.HasSuffix(content, "\n") {
		lines++
	}

	if !strings.HasPrefix(content, "---") {
		return nil, Violation{FieldSkillMD, RuleFrontmatter, "must start with YAML frontmatter (---)"}
	}
	// Split on the first two "---" anywhere in the file, as skills-ref does.
	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		return nil, Violation{FieldSkillMD, RuleFrontmatter, "frontmatter is not closed with ---"}
	}

	var root yaml.Node
	if err := yaml.Unmarshal([]byte(parts[1]), &root); err != nil {
		return nil, Violation{FieldSkillMD, RuleYAML, fmt.Sprintf("invalid YAML in frontmatter: %v", err)}
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, Violation{FieldSkillMD, RuleFrontmatter, "frontmatter must be a YAML mapping"}
	}
	mapping := root.Content[0]
	if err := checkStrict(mapping); err != nil {
		return nil, Violation{FieldSkillMD, RuleYAML, fmt.Sprintf("invalid YAML in frontmatter: %v", err)}
	}

	doc := &document{fields: map[string]*yaml.Node{}, body: strings.TrimSpace(parts[2]), lines: lines}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := mapping.Content[i].Value
		doc.keys = append(doc.keys, key)
		doc.fields[key] = mapping.Content[i+1]
	}
	return doc, nil
}

// checkStrict rejects what strict YAML (strictyaml, used by skills-ref)
// rejects: flow style, anchors, aliases, explicit tags, non-scalar keys and
// duplicate keys.
func checkStrict(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode {
		return fmt.Errorf("line %d: aliases are not allowed", n.Line)
	}
	if n.Anchor != "" {
		return fmt.Errorf("line %d: anchors are not allowed", n.Line)
	}
	if n.Style&yaml.TaggedStyle != 0 {
		return fmt.Errorf("line %d: tags are not allowed", n.Line)
	}
	if (n.Kind == yaml.MappingNode || n.Kind == yaml.SequenceNode) && n.Style&yaml.FlowStyle != 0 {
		return fmt.Errorf("line %d: flow style is not allowed", n.Line)
	}
	if n.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind != yaml.ScalarNode {
				return fmt.Errorf("line %d: keys must be strings", k.Line)
			}
			if seen[k.Value] {
				return fmt.Errorf("line %d: duplicate key %q", k.Line, k.Value)
			}
			seen[k.Value] = true
		}
	}
	for _, c := range n.Content {
		if err := checkStrict(c); err != nil {
			return err
		}
	}
	return nil
}
