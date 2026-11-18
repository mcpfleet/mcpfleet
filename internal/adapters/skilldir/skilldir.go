// Package skilldir installs and lists Agent Skills in an agent's skills
// directory, where every skill is a subdirectory named after the skill. It
// is the shared implementation behind the agents whose skills support is a
// plain directory of skills (adapters.SkillAdapter).
package skilldir

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mcpfleet/mcpfleet/pkg/skillspec"
)

// tempPrefix starts the name of every temporary directory Install creates
// inside the skills directory. It starts with a dot, so List and agents
// that skip hidden entries do not take it for a skill.
const tempPrefix = ".mcpfleet-"

// InstalledSkill is one skill found in an agent's skills directory, whether
// mcpfleet installed it or not.
type InstalledSkill struct {
	// Name is the directory name.
	Name string
	// Path is the skill's directory.
	Path string
	// SHA256 is skillspec.Hash of the directory; empty when HashErr is set.
	SHA256  string
	HashErr error
	// Skill is the parsed SKILL.md; nil when it cannot be parsed.
	Skill *skillspec.Skill
	// Result is the skillspec validation of the directory.
	Result skillspec.Result
}

// checkName refuses a name that is not one path segment.
func checkName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") {
		return fmt.Errorf("skill name %q cannot be a directory name", name)
	}
	return nil
}

// checkDir refuses a relative skills directory, which an empty home
// directory gives, so nothing is written relative to the working directory.
func checkDir(dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("skills directory %q is not an absolute path", dir)
	}
	return nil
}

// Install unpacks archive (a skillspec.Pack tar.gz) into dir/name. The
// archive is unpacked into a temporary directory inside dir and its
// skillspec hash is checked against sha before dir/name is touched; an
// existing dir/name is moved aside first and put back if the new copy cannot
// be moved in. On any error dir/name is left as it was and no temporary
// directory remains.
func Install(dir, name string, archive []byte, sha string) error {
	if err := checkName(name); err != nil {
		return err
	}
	if err := checkDir(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(dir, tempPrefix+"new-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	root, err := skillspec.Unpack(bytes.NewReader(archive), name, tmp)
	if err != nil {
		return err
	}
	got, err := skillspec.Hash(root)
	if err != nil {
		return err
	}
	if got != sha {
		return fmt.Errorf("sha256 mismatch: the registry says %s, the archive holds %s", sha, got)
	}

	target := filepath.Join(dir, name)
	if _, err := os.Lstat(target); errors.Is(err, fs.ErrNotExist) {
		return os.Rename(root, target)
	} else if err != nil {
		return err
	}
	old, err := os.MkdirTemp(dir, tempPrefix+"old-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(old)
	aside := filepath.Join(old, name)
	if err := os.Rename(target, aside); err != nil {
		return err
	}
	if err := os.Rename(root, target); err != nil {
		if rerr := os.Rename(aside, target); rerr != nil {
			return fmt.Errorf("%w (and restoring the previous copy failed: %v; it is at %s)", err, rerr, aside)
		}
		return err
	}
	return nil
}

// LocalHash returns skillspec.Hash of dir/name, or "" when it does not
// exist.
func LocalHash(dir, name string) (string, error) {
	if err := checkName(name); err != nil {
		return "", err
	}
	target := filepath.Join(dir, name)
	if _, err := os.Lstat(target); errors.Is(err, fs.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	return skillspec.Hash(target)
}

// List returns every skill in dir, sorted by name: each subdirectory (or
// symlink to one) whose name does not start with a dot, validated with
// skillspec. A missing dir holds no skills.
func List(dir string) ([]InstalledSkill, error) {
	list, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []InstalledSkill
	for _, e := range list {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			continue
		}
		s := InstalledSkill{Name: e.Name(), Path: path, Result: skillspec.Validate(path)}
		s.SHA256, s.HashErr = skillspec.Hash(path)
		if parsed, err := skillspec.Parse(path); err == nil {
			s.Skill = parsed
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
