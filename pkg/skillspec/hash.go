package skillspec

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// Hash returns the hex sha256 of the skill directory dir. The CLI, the
// registry and apply all identify a skill's content by it, so the algorithm
// is part of the contract:
//
//   - every regular file under dir, except inside .git directories, in
//     byte order of its slash-separated path relative to dir;
//   - for each file, "<path>\x00<mode>\x00<length>\x00<content>", where
//     <mode> is "755" when any executable bit is set and "644" otherwise, so
//     a different umask leaves the hash alone but a lost executable bit does
//     not; <length> is the decimal byte count of <content>;
//   - directories are not hashed themselves, so an empty one adds nothing.
//
// dir itself may be a symlink to the skill directory; a symlink or any other
// non-regular file inside it is an error.
func Hash(dir string) (string, error) {
	type entry struct {
		rel  string
		abs  string
		mode string
	}
	// The skill directory itself may be a symlink (a skill linked into an
	// agent's skills folder); only links inside it are rejected.
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	var entries []entry
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !d.Type().IsRegular() {
			return fmt.Errorf("hash %s: %s is not a regular file (symlinks are not allowed)", dir, rel)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := "644"
		if info.Mode().Perm()&0o111 != 0 {
			mode = "755"
		}
		entries = append(entries, entry{rel, path, mode})
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })

	h := sha256.New()
	for _, e := range entries {
		f, err := os.Open(e.abs)
		if err != nil {
			return "", err
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return "", err
		}
		header := e.rel + "\x00" + e.mode + "\x00" + strconv.FormatInt(info.Size(), 10) + "\x00"
		_, _ = io.WriteString(h, header)
		n, err := io.Copy(h, f)
		f.Close()
		if err != nil {
			return "", err
		}
		if n != info.Size() {
			return "", fmt.Errorf("hash %s: %s changed while it was read", dir, e.rel)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
