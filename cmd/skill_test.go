package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mcpfleet/mcpfleet/pkg/skillspec"
)

// storedSkill is one skill held by skillRegistry.
type storedSkill struct {
	archive []byte
	sha     string
	skill   map[string]any
}

// skillRegistry is an in-memory registry for the /v1/skills endpoints that
// judges uploads the way the real one does: it unpacks the archive with
// skillspec, validates it, checks the required servers against servers and
// computes the hash itself.
type skillRegistry struct {
	t       *testing.T
	servers map[string]bool
	// sha, when set, replaces the X-Skill-Sha256 header of every archive;
	// noSHA drops the header.
	sha   string
	noSHA bool

	mu     sync.Mutex
	skills map[string]*storedSkill
	seen   []string
}

func newSkillRegistry(t *testing.T, servers ...string) *skillRegistry {
	t.Helper()
	r := &skillRegistry{t: t, servers: map[string]bool{}, skills: map[string]*storedSkill{}}
	for _, s := range servers {
		r.servers[s] = true
	}
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	loginTo(t, srv.URL)
	t.Cleanup(func() { resetFlags(rootCmd) })
	return r
}

// loginTo points the CLI at url with a saved token, in a fresh HOME.
func loginTo(t *testing.T, url string) {
	t.Helper()
	isolateConfig(t)
	t.Setenv("MCPFLEET_REGISTRY_URL", url)
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".config", "mcpfleet")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "token"), []byte("tok"), 0o600))
}

func (r *skillRegistry) requests() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seen...)
}

func problem(w http.ResponseWriter, status int, detail string, errs ...map[string]any) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": status, "detail": detail, "errors": errs})
}

func (r *skillRegistry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, req.Method+" "+req.URL.Path)

	if req.Header.Get("Authorization") != "Bearer tok" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	rest := strings.TrimPrefix(req.URL.Path, "/v1/skills")
	name := strings.TrimPrefix(rest, "/")
	archivePath := strings.HasSuffix(name, "/archive")
	name = strings.TrimSuffix(name, "/archive")

	switch {
	case req.Method == http.MethodGet && rest == "":
		list := []map[string]any{}
		names := make([]string, 0, len(r.skills))
		for n := range r.skills {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			list = append(list, r.skills[n].skill)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	case req.Method == http.MethodPut && !archivePath:
		r.put(w, req, name, body)
	case req.Method == http.MethodGet:
		s, ok := r.skills[name]
		if !ok {
			problem(w, http.StatusNotFound, "skill not found")
			return
		}
		if !archivePath {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(s.skill)
			return
		}
		w.Header().Set("Content-Type", "application/gzip")
		switch {
		case r.noSHA:
		case r.sha != "":
			w.Header().Set("X-Skill-Sha256", r.sha)
		default:
			w.Header().Set("X-Skill-Sha256", s.sha)
		}
		_, _ = w.Write(s.archive)
	case req.Method == http.MethodDelete:
		if _, ok := r.skills[name]; !ok {
			problem(w, http.StatusNotFound, "skill not found")
			return
		}
		delete(r.skills, name)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (r *skillRegistry) put(w http.ResponseWriter, req *http.Request, name string, archive []byte) {
	if req.Header.Get("Content-Type") != "application/gzip" {
		problem(w, http.StatusUnsupportedMediaType, "want application/gzip")
		return
	}
	if len(archive) > skillspec.MaxArchiveBytes {
		problem(w, http.StatusRequestEntityTooLarge, "request body is too large")
		return
	}
	tmp, err := os.MkdirTemp("", "skill-registry-*")
	require.NoError(r.t, err)
	defer os.RemoveAll(tmp)
	dir, err := skillspec.Unpack(bytes.NewReader(archive), name, tmp)
	if err != nil {
		problem(w, http.StatusUnprocessableEntity, "invalid skill archive", map[string]any{"location": "body", "message": err.Error()})
		return
	}
	if res := skillspec.Validate(dir); !res.Valid() {
		var errs []map[string]any
		for _, v := range res.Errors {
			errs = append(errs, map[string]any{"location": v.Field, "message": v.Message + " (" + v.Rule + ")"})
		}
		problem(w, http.StatusUnprocessableEntity, "skill does not conform to the Agent Skills specification", errs...)
		return
	}
	parsed, err := skillspec.Parse(dir)
	require.NoError(r.t, err)
	var missing []map[string]any
	for _, s := range parsed.Requires {
		if !r.servers[s] {
			missing = append(missing, map[string]any{"location": "metadata.mcpfleet-requires",
				"message": `server "` + s + `" is not in the registry`, "value": s})
		}
	}
	if len(missing) > 0 {
		problem(w, http.StatusUnprocessableEntity, "skill requires servers the registry does not have", missing...)
		return
	}
	requires := parsed.Requires
	if requires == nil {
		requires = []string{}
	}
	skill := map[string]any{"name": name, "description": parsed.Description, "requires": requires,
		"sha256": parsed.SHA256, "size": len(archive)}
	r.skills[name] = &storedSkill{archive: archive, sha: parsed.SHA256, skill: skill}
	w.Header().Set("Content-Type", "application/json")
	out := map[string]any{"warnings": []any{}}
	for k, v := range skill {
		out[k] = v
	}
	_ = json.NewEncoder(w).Encode(out)
}

const notesSkillMD = "---\nname: notes\ndescription: Take notes in a markdown file.\n---\n# Notes\n\nWrite the notes to NOTES.md.\n"

// writeSkill writes a skill directory named name under a temp dir: its
// SKILL.md, an executable script, a nested reference and a .git directory.
func writeSkill(t *testing.T, name, skillMD string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	files := map[string]struct {
		body string
		mode os.FileMode
	}{
		"SKILL.md":               {skillMD, 0o644},
		"scripts/run.sh":         {"#!/bin/sh\necho hi\n", 0o755},
		"references/deep/ref.md": {"ref\n", 0o644},
		".git/HEAD":              {"ref: refs/heads/main\n", 0o644},
	}
	for rel, f := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(f.body), f.mode))
		require.NoError(t, os.Chmod(p, f.mode))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(root, "assets"), 0o755))
	return root
}

func skillHashOf(t *testing.T, dir string) string {
	t.Helper()
	h, err := skillspec.Hash(dir)
	require.NoError(t, err)
	return h
}

func TestSkillPush(t *testing.T) {
	reg := newSkillRegistry(t)
	dir := writeSkill(t, "notes", notesSkillMD)

	var stdout, stderr bytes.Buffer
	code := runForTest(t, []string{"skill", "push", dir}, &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())
	assert.Equal(t, "pushed notes "+skillHashOf(t, dir)[:12]+"\n", stdout.String())
	assert.Empty(t, stderr.String())

	want, err := skillspec.Pack(dir, "notes")
	require.NoError(t, err)
	assert.Equal(t, want, reg.skills["notes"].archive, "the packed directory is sent as is")
	assert.Equal(t, []string{"PUT /v1/skills/notes"}, reg.requests())
}

func TestSkillPush_ViolationsStopBeforeUpload(t *testing.T) {
	reg := newSkillRegistry(t)
	dir := skillFixture("invalid", "leading-hyphen")

	var stdout, stderr bytes.Buffer
	code := runForTest(t, []string{"skill", "push", dir}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Equal(t, "error: name: must not start or end with a hyphen\n"+
		`error: name: must match the directory name "leading-hyphen" (got "-leading-hyphen")`+"\n", stdout.String())
	assert.Equal(t, "Error: "+dir+" is not a valid skill (2 errors), nothing was pushed\n", stderr.String())
	assert.Empty(t, reg.requests(), "nothing leaves the machine")
}

func TestSkillPush_WarningsDoNotBlock(t *testing.T) {
	reg := newSkillRegistry(t)
	dir := skillFixture("valid", "long-body")

	var stdout, stderr bytes.Buffer
	code := runForTest(t, []string{"skill", "push", dir}, &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())
	assert.Equal(t, "warning: SKILL.md: has 514 lines, the spec recommends at most 500\n"+
		"pushed long-body "+skillHashOf(t, dir)[:12]+"\n", stdout.String())
	assert.Equal(t, []string{"PUT /v1/skills/long-body"}, reg.requests())
}

func TestSkillPush_MissingServers(t *testing.T) {
	newSkillRegistry(t, "filesystem")
	dir := skillFixture("valid", "full-fields") // requires github and filesystem

	var stdout, stderr bytes.Buffer
	code := runForTest(t, []string{"skill", "push", dir}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Empty(t, stdout.String())
	assert.Equal(t, `Error: push skill "full-fields": the registry does not have the servers it requires: github`+
		" (push them first with 'mcpfleet push <file>')\n", stderr.String())
}

func TestSkillPush_RegistryRejection(t *testing.T) {
	newSkillRegistry(t)
	// The registry's own verdict reaches the user when it differs from the
	// local one; here a hand-made server rejects every upload.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		problem(w, http.StatusUnprocessableEntity, "invalid skill archive",
			map[string]any{"location": "body", "message": "archive must hold the directory \"notes\""})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MCPFLEET_REGISTRY_URL", srv.URL)

	var stdout, stderr bytes.Buffer
	code := runForTest(t, []string{"skill", "push", writeSkill(t, "notes", notesSkillMD)}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Equal(t, `Error: push skill "notes": registry: validation failed: invalid skill archive: body: archive must hold the directory "notes"`+"\n", stderr.String())
}

func TestSkillPush_TooLargeStaysLocal(t *testing.T) {
	reg := newSkillRegistry(t)
	dir := writeSkill(t, "notes", notesSkillMD)
	noise := make([]byte, skillspec.MaxArchiveBytes+1024)
	x := uint32(7)
	for i := range noise {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		noise[i] = byte(x)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "assets", "noise.bin"), noise, 0o644))

	var stdout, stderr bytes.Buffer
	code := runForTest(t, []string{"skill", "push", dir}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "more than 1048576 bytes compressed")
	assert.Empty(t, reg.requests())
}

func TestSkillPull(t *testing.T) {
	newSkillRegistry(t)
	src := writeSkill(t, "notes", notesSkillMD)
	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, runForTest(t, []string{"skill", "push", src}, &stdout, &stderr), stderr.String())

	dest := filepath.Join(t.TempDir(), "skills")
	stdout.Reset()
	code := runForTest(t, []string{"skill", "pull", "notes", dest}, &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())
	target := filepath.Join(dest, "notes")
	assert.Equal(t, "pulled notes "+skillHashOf(t, src)[:12]+" -> "+target+"\n", stdout.String())

	info, err := os.Stat(filepath.Join(target, "scripts", "run.sh"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm(), "the executable bit survives")
	assert.Equal(t, skillHashOf(t, src), skillHashOf(t, target))
	assert.Equal(t, []string{"notes"}, dirEntries(t, dest), "no temporary directory is left behind")
}

func TestSkillPull_DefaultsToCurrentDirectory(t *testing.T) {
	newSkillRegistry(t)
	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, runForTest(t, []string{"skill", "push", writeSkill(t, "notes", notesSkillMD)}, &stdout, &stderr), stderr.String())

	wd, err := os.Getwd()
	require.NoError(t, err)
	dest := t.TempDir()
	require.NoError(t, os.Chdir(dest))
	t.Cleanup(func() { _ = os.Chdir(wd) })

	stdout.Reset()
	require.Equal(t, 0, runForTest(t, []string{"skill", "pull", "notes"}, &stdout, &stderr), stderr.String())
	_, err = os.Stat(filepath.Join(dest, "notes", "SKILL.md"))
	assert.NoError(t, err)
}

func TestSkillPull_ExistingDirectoryNeedsForce(t *testing.T) {
	newSkillRegistry(t)
	src := writeSkill(t, "notes", notesSkillMD)
	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, runForTest(t, []string{"skill", "push", src}, &stdout, &stderr), stderr.String())

	dest := t.TempDir()
	old := filepath.Join(dest, "notes", "old.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(old), 0o755))
	require.NoError(t, os.WriteFile(old, []byte("mine"), 0o644))

	stderr.Reset()
	code := runForTest(t, []string{"skill", "pull", "notes", dest}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Equal(t, "Error: "+filepath.Join(dest, "notes")+" already exists (use --force to replace it)\n", stderr.String())
	_, err := os.Stat(old)
	assert.NoError(t, err, "the existing directory is left alone")

	stderr.Reset()
	code = runForTest(t, []string{"skill", "pull", "--force", "notes", dest}, &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())
	_, err = os.Stat(old)
	assert.True(t, os.IsNotExist(err), "--force replaces the directory")
	assert.Equal(t, skillHashOf(t, src), skillHashOf(t, filepath.Join(dest, "notes")))
	assert.Equal(t, []string{"notes"}, dirEntries(t, dest))
}

func TestSkillPull_HashMismatch(t *testing.T) {
	for name, tamper := range map[string]func(r *skillRegistry){
		"wrong hash":   func(r *skillRegistry) { r.sha = strings.Repeat("0", 64) },
		"missing hash": func(r *skillRegistry) { r.noSHA = true },
	} {
		t.Run(name, func(t *testing.T) {
			reg := newSkillRegistry(t)
			var stdout, stderr bytes.Buffer
			require.Equal(t, 0, runForTest(t, []string{"skill", "push", writeSkill(t, "notes", notesSkillMD)}, &stdout, &stderr), stderr.String())
			tamper(reg)

			dest := t.TempDir()
			// An existing directory is not lost to a failed --force pull.
			keep := filepath.Join(dest, "notes", "keep.txt")
			require.NoError(t, os.MkdirAll(filepath.Dir(keep), 0o755))
			require.NoError(t, os.WriteFile(keep, []byte("mine"), 0o644))

			stderr.Reset()
			code := runForTest(t, []string{"skill", "pull", "--force", "notes", dest}, &stdout, &stderr)
			assert.Equal(t, 1, code)
			assert.Contains(t, stderr.String(), `pull skill "notes"`)
			assert.Contains(t, stderr.String(), "sha256")
			_, err := os.Stat(keep)
			assert.NoError(t, err)
			assert.Equal(t, []string{"notes"}, dirEntries(t, dest), "nothing else is left behind")
		})
	}
}

func TestSkillPull_NotFound(t *testing.T) {
	newSkillRegistry(t)
	dest := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runForTest(t, []string{"skill", "pull", "nope", dest}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Equal(t, "Error: skill \"nope\" not found in registry\n", stderr.String())
	assert.Empty(t, dirEntries(t, dest))
}

func TestSkillPull_RejectsUnusableName(t *testing.T) {
	reg := newSkillRegistry(t)
	for _, name := range []string{"..", ".", "a/b", `a\b`} {
		var stdout, stderr bytes.Buffer
		code := runForTest(t, []string{"skill", "pull", name, t.TempDir()}, &stdout, &stderr)
		assert.Equal(t, 1, code, name)
		assert.Contains(t, stderr.String(), "cannot be a directory name", name)
	}
	assert.Empty(t, reg.requests())
}

func TestSkillList(t *testing.T) {
	newSkillRegistry(t, "github", "filesystem")
	var stdout, stderr bytes.Buffer
	code := runForTest(t, []string{"skill", "list"}, &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())
	assert.Equal(t, "No skills registered.\n", stdout.String())

	full := skillFixture("valid", "full-fields")
	long := skillFixture("valid", "description-max-length")
	for _, dir := range []string{full, long} {
		require.Equal(t, 0, runForTest(t, []string{"skill", "push", dir}, &stdout, &stderr), stderr.String())
	}

	stdout.Reset()
	code = runForTest(t, []string{"skill", "list"}, &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())
	out := stdout.String()
	assert.Contains(t, out, "Skills")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 6, out)
	assert.Regexp(t, `^NAME +DESCRIPTION +REQUIRES +SHA256$`, lines[2])
	assert.Regexp(t, `^description-max-length +d{59}… +- +`+skillHashOf(t, long)[:12]+`$`, lines[4])
	assert.Regexp(t, `^full-fields +Uses every field the spec defines\. +github,filesystem +`+skillHashOf(t, full)[:12]+`$`, lines[5])
}

func TestSkillDelete(t *testing.T) {
	reg := newSkillRegistry(t)
	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, runForTest(t, []string{"skill", "push", writeSkill(t, "notes", notesSkillMD)}, &stdout, &stderr), stderr.String())

	stdout.Reset()
	code := runForTest(t, []string{"skill", "delete", "notes"}, &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())
	assert.Equal(t, "Skill \"notes\" removed from registry.\n", stdout.String())
	assert.Empty(t, reg.skills)

	stdout.Reset()
	code = runForTest(t, []string{"skill", "delete", "notes"}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Equal(t, "Error: skill \"notes\" not found in registry\n", stderr.String())
	assert.Empty(t, stdout.String())
}

// TestSkillRoundTrip pushes a skill and pulls it back: the pulled tree is
// byte-identical to the pushed one (paths, contents, executable bits, empty
// directories; .git stays local) and passes skills-ref validate. With
// MCPFLEET_REQUIRE_SKILLS_REF=1 (CI) skills-ref must be on PATH.
func TestSkillRoundTrip(t *testing.T) {
	newSkillRegistry(t)
	src := writeSkill(t, "notes", notesSkillMD)

	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, runForTest(t, []string{"skill", "push", src}, &stdout, &stderr), stderr.String())
	dest := t.TempDir()
	require.Equal(t, 0, runForTest(t, []string{"skill", "pull", "notes", dest}, &stdout, &stderr), stderr.String())

	pulled := filepath.Join(dest, "notes")
	require.NoError(t, os.RemoveAll(filepath.Join(src, ".git")))
	assert.Equal(t, treeOf(t, src), treeOf(t, pulled))

	if _, err := exec.LookPath("skills-ref"); err != nil {
		if os.Getenv("MCPFLEET_REQUIRE_SKILLS_REF") == "1" {
			t.Fatal("skills-ref is not on PATH")
		}
		t.Log("skills-ref not on PATH; the CI job skills-ref-parity runs this check")
		return
	}
	out, err := exec.Command("skills-ref", "validate", pulled).CombinedOutput()
	assert.NoError(t, err, "skills-ref validate %s: %s", pulled, out)
}

// treeOf describes every entry under root as "path mode [content]".
func treeOf(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
		rel, _ := filepath.Rel(root, path)
		info, err := d.Info()
		require.NoError(t, err)
		line := filepath.ToSlash(rel) + " " + info.Mode().String()
		if d.Type().IsRegular() {
			b, err := os.ReadFile(path)
			require.NoError(t, err)
			line += " " + string(b)
		}
		out = append(out, line)
		return nil
	}))
	return out
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
