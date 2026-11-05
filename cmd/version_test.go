package cmd

import (
	"bytes"
	"testing"
)

func TestVersion_PrintsBuildInfo(t *testing.T) {
	tests := []struct {
		name                  string
		version, commit, date string
		want                  string
	}{
		{
			name:    "ldflags values",
			version: "0.1.0", commit: "abc1234", date: "2026-11-05T10:00:00Z",
			want: "mcpfleet version 0.1.0 (commit abc1234, built 2026-11-05T10:00:00Z)\n",
		},
		{
			name:    "defaults without ldflags",
			version: "dev", commit: "none", date: "unknown",
			want: "mcpfleet version dev (commit none, built unknown)\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(func() {
				// Disable --version again so later tests see the root command
				// as it is without SetVersionInfo.
				rootCmd.Version = ""
				if f := rootCmd.Flags().Lookup("version"); f != nil {
					_ = f.Value.Set("false")
				}
			})
			SetVersionInfo(tt.version, tt.commit, tt.date)

			var stdout, stderr bytes.Buffer
			code := runForTest(t, []string{"--version"}, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr.String())
			}
			if got := stdout.String(); got != tt.want {
				t.Errorf("stdout = %q, want %q", got, tt.want)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}
