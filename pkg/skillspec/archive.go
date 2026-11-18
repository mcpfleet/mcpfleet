package skillspec

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Limits of a skill archive. The registry enforces the same ones, so a skill
// Pack accepts is one the registry accepts.
const (
	// MaxArchiveBytes is the largest compressed (tar.gz) archive.
	MaxArchiveBytes = 1 << 20
	// MaxUnpackedBytes caps the total size of the files in an archive.
	MaxUnpackedBytes = 10 << 20
	// MaxArchiveEntries caps the number of files and directories in an
	// archive, the top-level directory included.
	MaxArchiveEntries = 1000
)

// ArchiveError is a skill archive that breaks one of the archive rules (see
// Unpack). Pack reports the limits it would break the same way.
type ArchiveError struct {
	msg string
}

func (e *ArchiveError) Error() string { return e.msg }

func archiveErrorf(format string, args ...any) error {
	return &ArchiveError{fmt.Sprintf(format, args...)}
}

// usableName reports whether name can be the single top-level directory of
// an archive: one path segment, not "." or "..".
func usableName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") {
		return fmt.Errorf("skill name %q cannot be a directory name: it must be one path segment, not \".\" or \"..\"", name)
	}
	return nil
}

// Pack returns the skill directory dir as a tar.gz archive whose single
// top-level directory is name, the form the registry stores. The archive is
// reproducible: entries in byte order of their path, files 0644 or 0755 (only
// the executable bit is kept, as in Hash), directories 0755, no owners and a
// zero modification time. .git directories are left out at any depth; a
// symlink or another non-regular file inside dir is an error, and so is an
// archive above the limits (an *ArchiveError). dir itself may be a symlink.
func Pack(dir, name string) ([]byte, error) {
	if err := usableName(name); err != nil {
		return nil, err
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	type entry struct {
		rel  string
		abs  string
		dir  bool
		mode int64
		size int64
	}
	var (
		entries  []entry
		unpacked int64
	)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			entries = append(entries, entry{rel: rel, abs: path, dir: true, mode: 0o755})
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("pack %s: %s is not a regular file (symlinks are not allowed)", dir, rel)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := int64(0o644)
		if info.Mode().Perm()&0o111 != 0 {
			mode = 0o755
		}
		unpacked += info.Size()
		entries = append(entries, entry{rel: rel, abs: path, mode: mode, size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(entries)+1 > MaxArchiveEntries {
		return nil, archiveErrorf("skill %s holds more than %d files and directories", dir, MaxArchiveEntries)
	}
	if unpacked > MaxUnpackedBytes {
		return nil, archiveErrorf("skill %s holds more than %d bytes", dir, MaxUnpackedBytes)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })

	var buf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	tw := tar.NewWriter(gz)
	header := func(name string, typeflag byte, mode, size int64) *tar.Header {
		return &tar.Header{Name: name, Typeflag: typeflag, Mode: mode, Size: size, ModTime: time.Unix(0, 0)}
	}
	if err := tw.WriteHeader(header(name+"/", tar.TypeDir, 0o755, 0)); err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.dir {
			if err := tw.WriteHeader(header(name+"/"+e.rel+"/", tar.TypeDir, e.mode, 0)); err != nil {
				return nil, err
			}
			continue
		}
		if err := tw.WriteHeader(header(name+"/"+e.rel, tar.TypeReg, e.mode, e.size)); err != nil {
			return nil, err
		}
		if err := copyFile(tw, e.abs, e.size); err != nil {
			return nil, fmt.Errorf("pack %s: %s: %w", dir, e.rel, err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	if buf.Len() > MaxArchiveBytes {
		return nil, archiveErrorf("skill %s packs to %d bytes, more than %d bytes compressed", dir, buf.Len(), MaxArchiveBytes)
	}
	return buf.Bytes(), nil
}

// copyFile writes exactly size bytes of the file at path to w; a file that
// changed size since it was listed is an error.
func copyFile(w io.Writer, path string, size int64) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.CopyN(w, f, size); err != nil {
		return fmt.Errorf("changed while it was read: %w", err)
	}
	if n, _ := f.Read(make([]byte, 1)); n != 0 {
		return errors.New("changed while it was read")
	}
	return nil
}

// Unpack extracts the tar.gz archive read from r into dest and returns the
// skill directory, dest/name, which must not exist yet. The archive rules,
// the same as the registry's:
//
//   - at most MaxArchiveBytes compressed, MaxUnpackedBytes of file content
//     and MaxArchiveEntries entries;
//   - exactly one top-level directory, name, holding only directories and
//     regular files (no symlinks, hard links or devices);
//   - entry paths are relative and stay inside that directory: no absolute
//     paths, "..", ".", empty segments, backslashes or NUL; a leading "./"
//     and pax global headers are allowed;
//   - each path appears once;
//   - files get 0755 when any executable bit is set and 0644 otherwise,
//     directories 0755.
//
// A broken rule is an *ArchiveError, and nothing is left under dest/name.
func Unpack(r io.Reader, name, dest string) (_ string, err error) {
	if err := usableName(name); err != nil {
		return "", err
	}
	root := filepath.Join(dest, name)
	if _, err := os.Lstat(root); err == nil {
		return "", fmt.Errorf("unpack: %s already exists", root)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	archive, err := io.ReadAll(io.LimitReader(r, MaxArchiveBytes+1))
	if err != nil {
		return "", err
	}
	if len(archive) > MaxArchiveBytes {
		return "", archiveErrorf("archive is more than %d bytes compressed", MaxArchiveBytes)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return "", archiveErrorf("archive is not gzip-compressed: %v", err)
	}
	defer gz.Close()

	defer func() {
		if err != nil {
			_ = os.RemoveAll(root)
		}
	}()

	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	var unpacked int64
	entries := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", archiveErrorf("archive is not a valid tar.gz: %v", err)
		}
		if hdr.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		entries++
		if entries > MaxArchiveEntries {
			return "", archiveErrorf("archive holds more than %d files and directories", MaxArchiveEntries)
		}

		parts, err := entryPath(hdr.Name)
		if err != nil {
			return "", err
		}
		if len(parts) == 0 {
			continue // the archive root, "./"
		}
		if parts[0] != name {
			return "", archiveErrorf("archive must hold only the directory %q (found %q)", name, hdr.Name)
		}
		clean := strings.Join(parts, "/")
		if seen[clean] {
			return "", archiveErrorf("archive holds %q more than once", clean)
		}
		seen[clean] = true
		target := filepath.Join(append([]string{dest}, parts...)...)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", archiveErrorf("archive entry %q: %v", hdr.Name, err)
			}
			if err := os.Chmod(target, 0o755); err != nil {
				return "", err
			}
		case tar.TypeReg:
			if len(parts) == 1 {
				return "", archiveErrorf("archive must hold the directory %q, not a file", name)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return "", archiveErrorf("archive entry %q: %v", hdr.Name, err)
			}
			mode := os.FileMode(0o644)
			if hdr.Mode&0o111 != 0 {
				mode = 0o755
			}
			n, err := writeEntry(target, clean, mode, tr, MaxUnpackedBytes-unpacked)
			if err != nil {
				return "", err
			}
			unpacked += n
		default:
			return "", archiveErrorf("archive entry %q: only regular files and directories are allowed", hdr.Name)
		}
	}

	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() {
		return "", archiveErrorf("archive must hold the directory %q", name)
	}
	return root, nil
}

// entryPath splits a tar entry name into its path components, dropping a
// leading "./" and a trailing "/". It rejects absolute paths, backslashes,
// NUL and empty, "." or ".." components.
func entryPath(name string) ([]string, error) {
	bad := archiveErrorf("archive entry %q: path must be relative and stay inside the skill directory", name)
	if strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\\x00") {
		return nil, bad
	}
	name = strings.TrimSuffix(strings.TrimPrefix(name, "./"), "/")
	if name == "" || name == "." {
		return nil, nil
	}
	parts := strings.Split(name, "/")
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			return nil, bad
		}
	}
	return parts, nil
}

// writeEntry creates target, which must not exist, with mode and copies at
// most limit bytes of r into it.
func writeEntry(target, clean string, mode os.FileMode, r io.Reader, limit int64) (int64, error) {
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if errors.Is(err, os.ErrExist) {
		return 0, archiveErrorf("archive holds %q more than once", clean)
	}
	if err != nil {
		return 0, archiveErrorf("archive entry %q: %v", clean, err)
	}
	n, err := io.Copy(f, io.LimitReader(r, limit+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return n, archiveErrorf("archive is not a valid tar.gz: %v", err)
	}
	if n > limit {
		return n, archiveErrorf("archive unpacks to more than %d bytes", MaxUnpackedBytes)
	}
	// OpenFile applies the umask; set the mode the hash depends on exactly.
	return n, os.Chmod(target, mode)
}
