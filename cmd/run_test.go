package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun_PrintsErrorOnceToStderr(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantMsg string
	}{
		{"unknown command", []string{"nie-istnieje"}, `unknown command "nie-istnieje"`},
		{"unknown flag", []string{"list", "--bogus"}, "unknown flag: --bogus"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(tt.args, &stdout, &stderr)
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			got := stderr.String()
			if !strings.HasPrefix(got, "Error: ") {
				t.Errorf("stderr = %q, want prefix %q", got, "Error: ")
			}
			if !strings.Contains(got, tt.wantMsg) {
				t.Errorf("stderr = %q, want it to contain %q", got, tt.wantMsg)
			}
			if n := strings.Count(got, "Error:"); n != 1 {
				t.Errorf("stderr has %d \"Error:\" occurrences, want 1: %q", n, got)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
		})
	}
}

func TestRun_SuccessReturnsZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"help"}, &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
	if stdout.Len() == 0 {
		t.Error("stdout is empty, want help text")
	}
}
