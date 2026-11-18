package skillspec

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFiles writes files (slash paths relative to root) with the given modes.
func writeFiles(t *testing.T, root string, files map[string]fileSpec) {
	t.Helper()
	for rel, f := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		mode := f.mode
		if mode == 0 {
			mode = 0o644
		}
		require.NoError(t, os.WriteFile(p, []byte(f.body), mode))
		require.NoError(t, os.Chmod(p, mode))
	}
}

type fileSpec struct {
	body string
	mode os.FileMode
}

// tarEntry is one header of a hand-built archive.
type tarEntry struct {
	name     string
	body     string
	mode     int64
	typeflag byte
	linkname string
}

func buildArchive(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typeflag := e.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		hdr := &tar.Header{Name: e.name, Mode: mode, Typeflag: typeflag, Linkname: e.linkname, Format: tar.FormatPAX}
		if typeflag == tar.TypeXGlobalHeader {
			hdr = &tar.Header{Name: e.name, Typeflag: typeflag, PAXRecords: map[string]string{"comment": "x"}}
		}
		if typeflag == tar.TypeReg {
			hdr.Size = int64(len(e.body))
		}
		require.NoError(t, tw.WriteHeader(hdr))
		if typeflag == tar.TypeReg {
			_, err := tw.Write([]byte(e.body))
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

// listArchive returns "name mode" for every entry of a tar.gz archive.
func listArchive(t *testing.T, archive []byte) []string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	require.NoError(t, err)
	tr := tar.NewReader(gz)
	var out []string
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		out = append(out, hdr.Name+" "+fs.FileMode(hdr.Mode).Perm().String())
	}
	return out
}

const notesMD = "---\nname: notes\ndescription: Take notes.\n---\n# Notes\n"

func notesTree(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "notes")
	writeFiles(t, root, map[string]fileSpec{
		"SKILL.md":          {body: notesMD},
		"scripts/run.sh":    {body: "#!/bin/sh\necho hi\n", mode: 0o755},
		"references/a.md":   {body: "a\n", mode: 0o600},
		".git/HEAD":         {body: "ref: refs/heads/main\n"},
		"sub/.git/config":   {body: "x\n"},
		"assets/z.txt":      {body: "z\n"},
		"assets/café.txt":   {body: "c\n"},
		"assets/umask.txt":  {body: "u\n", mode: 0o664},
		"assets/group-x.sh": {body: "g\n", mode: 0o710},
	})
	require.NoError(t, os.MkdirAll(filepath.Join(root, "empty"), 0o700))
	return root
}

func TestPack_LayoutAndModes(t *testing.T) {
	archive, err := Pack(notesTree(t), "notes")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"notes/ -rwxr-xr-x",
		"notes/SKILL.md -rw-r--r--",
		"notes/assets/ -rwxr-xr-x",
		"notes/assets/café.txt -rw-r--r--",
		"notes/assets/group-x.sh -rwxr-xr-x",
		"notes/assets/umask.txt -rw-r--r--",
		"notes/assets/z.txt -rw-r--r--",
		"notes/empty/ -rwxr-xr-x",
		"notes/references/ -rwxr-xr-x",
		"notes/references/a.md -rw-r--r--",
		"notes/scripts/ -rwxr-xr-x",
		"notes/scripts/run.sh -rwxr-xr-x",
		"notes/sub/ -rwxr-xr-x",
	}, listArchive(t, archive), ".git is skipped at any depth, only the executable bit is kept")
}

func TestPack_Reproducible(t *testing.T) {
	a, err := Pack(notesTree(t), "notes")
	require.NoError(t, err)
	b, err := Pack(notesTree(t), "notes")
	require.NoError(t, err)
	assert.Equal(t, a, b, "two packs of the same content are byte-identical")

	gz, err := gzip.NewReader(bytes.NewReader(a))
	require.NoError(t, err)
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		assert.Zero(t, hdr.Uid, hdr.Name)
		assert.Zero(t, hdr.Gid, hdr.Name)
		assert.Empty(t, hdr.Uname, hdr.Name)
		assert.Empty(t, hdr.Gname, hdr.Name)
		assert.Equal(t, int64(0), hdr.ModTime.Unix(), hdr.Name)
	}
}

func TestPack_RootNameIsTheSkillName(t *testing.T) {
	archive, err := Pack(notesTree(t), "other")
	require.NoError(t, err)
	assert.Equal(t, "other/ -rwxr-xr-x", listArchive(t, archive)[0])
}

func TestPack_FollowsSymlinkedRoot(t *testing.T) {
	root := notesTree(t)
	link := filepath.Join(t.TempDir(), "notes")
	require.NoError(t, os.Symlink(root, link))
	a, err := Pack(link, "notes")
	require.NoError(t, err)
	b, err := Pack(root, "notes")
	require.NoError(t, err)
	assert.Equal(t, b, a)
}

func TestPack_Rejects(t *testing.T) {
	t.Run("symlink inside", func(t *testing.T) {
		root := notesTree(t)
		require.NoError(t, os.Symlink("SKILL.md", filepath.Join(root, "link.md")))
		_, err := Pack(root, "notes")
		assert.ErrorContains(t, err, "link.md is not a regular file")
	})
	t.Run("too many entries", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "notes")
		files := map[string]fileSpec{}
		for i := 0; i < MaxArchiveEntries; i++ { // + the root directory
			files["f"+strings.Repeat("x", i%7)+"/"+string(rune('a'+i%26))+strings.Repeat("y", i/26)] = fileSpec{body: "x"}
		}
		writeFiles(t, root, files)
		_, err := Pack(root, "notes")
		assert.ErrorContains(t, err, "more than 1000 files and directories")
	})
	t.Run("too big unpacked", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "notes")
		writeFiles(t, root, map[string]fileSpec{"big.bin": {body: strings.Repeat("a", MaxUnpackedBytes+1)}})
		_, err := Pack(root, "notes")
		assert.ErrorContains(t, err, "more than 10485760 bytes")
	})
	t.Run("too big compressed", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "notes")
		// Random-looking bytes do not compress.
		b := make([]byte, MaxArchiveBytes+1024)
		x := uint32(1)
		for i := range b {
			x ^= x << 13
			x ^= x >> 17
			x ^= x << 5
			b[i] = byte(x)
		}
		writeFiles(t, root, map[string]fileSpec{"noise.bin": {body: string(b)}})
		_, err := Pack(root, "notes")
		assert.ErrorContains(t, err, "more than 1048576 bytes compressed")
	})
	t.Run("unusable name", func(t *testing.T) {
		for _, name := range []string{"", ".", "..", "a/b", `a\b`, "a\x00b"} {
			_, err := Pack(notesTree(t), name)
			assert.Error(t, err, "%q", name)
		}
	})
}

func TestUnpack_RoundTrip(t *testing.T) {
	src := notesTree(t)
	archive, err := Pack(src, "notes")
	require.NoError(t, err)

	dest := t.TempDir()
	root, err := Unpack(bytes.NewReader(archive), "notes", dest)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dest, "notes"), root)

	want, err := Hash(src)
	require.NoError(t, err)
	got, err := Hash(root)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	for rel, mode := range map[string]os.FileMode{
		"SKILL.md": 0o644, "scripts/run.sh": 0o755, "references/a.md": 0o644, "assets/group-x.sh": 0o755,
	} {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		require.NoError(t, err)
		assert.Equal(t, mode, info.Mode().Perm(), rel)
	}
	info, err := os.Stat(filepath.Join(root, "empty"))
	require.NoError(t, err)
	assert.True(t, info.IsDir(), "empty directories survive")
	_, err = os.Stat(filepath.Join(root, ".git"))
	assert.True(t, errors.Is(err, os.ErrNotExist))
}

func TestUnpack_AcceptsCommonTarLayouts(t *testing.T) {
	archive := buildArchive(t,
		tarEntry{name: "./", typeflag: tar.TypeDir},
		tarEntry{name: "./notes/", typeflag: tar.TypeDir, mode: 0o700},
		tarEntry{name: "./notes/SKILL.md", body: notesMD, mode: 0o664},
		tarEntry{name: "notes/scripts/run.sh", body: "x", mode: 0o744},
		tarEntry{name: "pax_global_header", typeflag: tar.TypeXGlobalHeader},
	)
	root, err := Unpack(bytes.NewReader(archive), "notes", t.TempDir())
	require.NoError(t, err)
	info, err := os.Stat(filepath.Join(root, "scripts", "run.sh"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm(), "missing parents are created")
}

func TestUnpackMalicious(t *testing.T) {
	cases := map[string]struct {
		archive []byte
		want    string
	}{
		"not gzip":       {[]byte("not an archive"), "not gzip-compressed"},
		"parent dir":     {buildArchive(t, tarEntry{name: "notes/../../etc/x", body: "x"}), "must be relative and stay inside"},
		"dot-dot only":   {buildArchive(t, tarEntry{name: "../x", body: "x"}), "must be relative and stay inside"},
		"absolute":       {buildArchive(t, tarEntry{name: "/etc/passwd", body: "x"}), "must be relative and stay inside"},
		"backslash":      {buildArchive(t, tarEntry{name: `notes\..\x`, body: "x"}), "must be relative and stay inside"},
		"empty segment":  {buildArchive(t, tarEntry{name: "notes//x", body: "x"}), "must be relative and stay inside"},
		"symlink":        {buildArchive(t, tarEntry{name: "notes/link", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"}), "only regular files and directories"},
		"hard link":      {buildArchive(t, tarEntry{name: "notes/link", typeflag: tar.TypeLink, linkname: "notes/SKILL.md"}), "only regular files and directories"},
		"device":         {buildArchive(t, tarEntry{name: "notes/dev", typeflag: tar.TypeChar}), "only regular files and directories"},
		"fifo":           {buildArchive(t, tarEntry{name: "notes/fifo", typeflag: tar.TypeFifo}), "only regular files and directories"},
		"wrong root":     {buildArchive(t, tarEntry{name: "other/SKILL.md", body: notesMD}), `must hold only the directory "notes"`},
		"second root":    {buildArchive(t, tarEntry{name: "notes/SKILL.md", body: notesMD}, tarEntry{name: "extra.txt", body: "x"}), `must hold only the directory "notes"`},
		"root is a file": {buildArchive(t, tarEntry{name: "notes", body: "x"}), `must hold the directory "notes", not a file`},
		"empty":          {buildArchive(t), `must hold the directory "notes"`},
		"duplicate file": {buildArchive(t, tarEntry{name: "notes/a", body: "1"}, tarEntry{name: "notes/a", body: "2"}), `holds "notes/a" more than once`},
		"duplicate dir":  {buildArchive(t, tarEntry{name: "notes/d/", typeflag: tar.TypeDir}, tarEntry{name: "notes/d", typeflag: tar.TypeDir}), `holds "notes/d" more than once`},
		"file then dir":  {buildArchive(t, tarEntry{name: "notes/a", body: "1"}, tarEntry{name: "notes/a/", typeflag: tar.TypeDir}), `holds "notes/a" more than once`},
		"too many": {func() []byte {
			entries := []tarEntry{{name: "notes/", typeflag: tar.TypeDir}}
			for i := 0; i < MaxArchiveEntries; i++ {
				entries = append(entries, tarEntry{name: "notes/f" + strings.Repeat("a", i%50) + string(rune('A'+i/50)), body: ""})
			}
			return buildArchive(t, entries...)
		}(), "more than 1000 files and directories"},
		"zip bomb": {buildArchive(t, tarEntry{name: "notes/a", body: strings.Repeat("a", MaxUnpackedBytes/2)},
			tarEntry{name: "notes/b", body: strings.Repeat("b", MaxUnpackedBytes/2+1)}), "unpacks to more than 10485760 bytes"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dest := t.TempDir()
			_, err := Unpack(bytes.NewReader(tc.archive), "notes", dest)
			require.Error(t, err)
			var ae *ArchiveError
			assert.True(t, errors.As(err, &ae), "%T: %v", err, err)
			assert.ErrorContains(t, err, tc.want)
			_, statErr := os.Lstat(filepath.Join(filepath.Dir(dest), "etc"))
			assert.True(t, errors.Is(statErr, os.ErrNotExist), "nothing escapes dest")
		})
	}
}

func TestUnpack_CompressedSizeLimit(t *testing.T) {
	big := bytes.Repeat([]byte{0}, MaxArchiveBytes+1)
	_, err := Unpack(bytes.NewReader(big), "notes", t.TempDir())
	assert.ErrorContains(t, err, "more than 1048576 bytes")
}

func TestUnpack_RefusesExistingTarget(t *testing.T) {
	archive, err := Pack(notesTree(t), "notes")
	require.NoError(t, err)
	dest := t.TempDir()
	require.NoError(t, os.Symlink("/tmp", filepath.Join(dest, "notes")))
	_, err = Unpack(bytes.NewReader(archive), "notes", dest)
	assert.ErrorContains(t, err, "already exists")
}

func TestUnpack_RejectsUnusableName(t *testing.T) {
	archive := buildArchive(t, tarEntry{name: "x/SKILL.md", body: notesMD})
	for _, name := range []string{"", ".", "..", "a/b", `a\b`} {
		_, err := Unpack(bytes.NewReader(archive), name, t.TempDir())
		assert.Error(t, err, "%q", name)
	}
}
