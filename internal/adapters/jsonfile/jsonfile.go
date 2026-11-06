// Package jsonfile updates agent config files without destroying the parts
// mcpfleet does not own: unknown keys and unrelated server entries are kept,
// unparsable files are never overwritten, the previous content is saved to
// <file>.bak and the new content is written atomically.
package jsonfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Permissions of what mcpfleet creates. Agent configs hold env values
// resolved from the vault, i.e. secrets in plain text, so a new config file
// is readable by its owner only, and a new config directory is private too
// (as the XDG Base Directory spec recommends). Existing files and
// directories keep their modes.
const (
	NewFileMode fs.FileMode = 0o600
	NewDirMode  fs.FileMode = 0o700
)

// HomeVar returns the environment variable os.UserHomeDir reads on goos:
// USERPROFILE on Windows, home on Plan 9 and HOME elsewhere.
func HomeVar(goos string) string {
	switch goos {
	case "windows":
		return "USERPROFILE"
	case "plan9":
		return "home"
	}
	return "HOME"
}

// RequireAbs returns path when it is absolute. A relative path means the
// directory it was built from is unknown (an empty home or config variable),
// so reading or writing it would hit the working directory instead; the
// error names agent and the variables that would fix it.
func RequireAbs(agent, path string, vars ...string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	set := strings.Join(vars, " or ")
	if n := len(vars); n > 2 {
		set = strings.Join(vars[:n-1], ", ") + " or " + vars[n-1]
	}
	return "", fmt.Errorf("%s: cannot determine config directory (set %s)", agent, set)
}

// UpsertEntries sets entries[name] under the top-level object key of the
// JSON file at path, leaving every other key and every other entry intact.
//
// A missing file, an empty file, or a missing/null key count as empty. A file
// that is not valid JSON, whose root is not an object, or whose key is not an
// object yields an error and the file is left untouched. A new file is
// created with NewFileMode; an existing file keeps its mode.
func UpsertEntries(path, key string, entries map[string]any) error {
	prev, err := Read(path)
	if err != nil {
		return err
	}

	root := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(prev)) > 0 {
		if err := json.Unmarshal(prev, &root); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if root == nil { // literal null
			return fmt.Errorf("parse %s: root is not a JSON object", path)
		}
	}

	section := map[string]json.RawMessage{}
	if rawSection, ok := root[key]; ok && !isNull(rawSection) {
		if err := json.Unmarshal(rawSection, &section); err != nil {
			return fmt.Errorf("parse %s: %q is not a JSON object: %w", path, key, err)
		}
	}

	for name, entry := range entries {
		enc, err := Marshal(entry, "", "")
		if err != nil {
			return fmt.Errorf("encode entry %q: %w", name, err)
		}
		section[name] = enc
	}
	encSection, err := Marshal(section, "", "")
	if err != nil {
		return fmt.Errorf("encode %q: %w", key, err)
	}
	root[key] = encSection

	out, err := Marshal(root, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return WriteFile(path, out, prev)
}

// Marshal encodes v like json.MarshalIndent, but leaves &, < and > as they
// are (config files are not HTML) and ends indented output with a newline.
// Embedded json.RawMessage values are compacted without escaping too.
func Marshal(v any, prefix, indent string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent(prefix, indent)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	if indent == "" && prefix == "" {
		out = bytes.TrimSuffix(out, []byte("\n"))
	}
	return out, nil
}

// Read returns the content of path, or nil when the file does not exist.
// An existing empty file yields a non-nil empty slice, so callers can tell
// the two apart when passing the result to WriteFile as prev.
func Read(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if data == nil {
		data = []byte{}
	}
	return data, nil
}

// WriteFile replaces the file at path with data.
//
// prev is the content the caller read earlier (nil when the file did not
// exist). If the file no longer matches prev, WriteFile fails instead of
// discarding a concurrent change. A symlink at path is followed, so the link
// itself is kept. An existing file is first copied to <file>.bak with its
// mode, and keeps that mode; a new file gets NewFileMode and missing parent
// directories get NewDirMode. The write goes to a temporary file in the same
// directory that is then renamed over the target.
func WriteFile(path string, data, prev []byte) error {
	target, err := resolve(path)
	if err != nil {
		return err
	}

	mode := NewFileMode
	cur, err := Read(target)
	if err != nil {
		return err
	}
	switch {
	case prev == nil && cur != nil:
		return fmt.Errorf("%s changed during apply (file appeared); re-run apply", path)
	case prev != nil && (cur == nil || !bytes.Equal(cur, prev)):
		return fmt.Errorf("%s changed during apply; re-run apply", path)
	}

	if cur != nil {
		st, err := os.Stat(target)
		if err != nil {
			return fmt.Errorf("stat %s: %w", target, err)
		}
		mode = st.Mode().Perm()
		if err := atomicWrite(target+".bak", cur, mode); err != nil {
			return fmt.Errorf("write backup: %w", err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(target), NewDirMode); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return atomicWrite(target, data, mode)
}

// resolve follows symlinks at path. A path that does not exist yet is
// returned unchanged.
func resolve(path string) (string, error) {
	real, err := filepath.EvalSymlinks(path)
	if err == nil {
		return real, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		if fi, lerr := os.Lstat(path); lerr == nil && fi.Mode()&fs.ModeSymlink != 0 {
			return "", fmt.Errorf("%s is a symlink to a missing file", path)
		}
		return path, nil
	}
	return "", fmt.Errorf("resolve %s: %w", path, err)
}

func atomicWrite(path string, data []byte, mode fs.FileMode) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func isNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}
