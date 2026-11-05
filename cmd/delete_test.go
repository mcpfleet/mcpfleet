package cmd

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDelete_UnknownServerFails(t *testing.T) {
	fakeRegistry(t, map[string]struct {
		status int
		body   string
	}{
		"DELETE /v1/servers/nope": {http.StatusNotFound, `{"detail":"server not found"}`},
	})

	var stdout, stderr bytes.Buffer
	code := runForTest(t, []string{"delete", "nope"}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Equal(t, "Error: server \"nope\" not found in registry\n", stderr.String())
	assert.Empty(t, stdout.String())
}

func TestDelete_Success(t *testing.T) {
	seen, mu := fakeRegistry(t, map[string]struct {
		status int
		body   string
	}{
		"DELETE /v1/servers/github": {http.StatusNoContent, ""},
	})

	var stdout, stderr bytes.Buffer
	code := runForTest(t, []string{"delete", "github"}, &stdout, &stderr)
	assert.Equal(t, 0, code, stderr.String())
	assert.Empty(t, stderr.String())
	assert.Equal(t, "Server \"github\" removed from registry.\n", stdout.String())

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"DELETE /v1/servers/github"}, *seen)
}
