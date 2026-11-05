package cmd

import (
	"fmt"
	"io"
)

// Run executes the root command with args, writing command output to stdout
// and errors to stderr. It returns the process exit code: 0 on success, 1 on
// any command error.
func Run(args []string, stdout, stderr io.Writer) int {
	rootCmd.SetArgs(args)
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)
	// rootCmd.SilenceErrors is true, so cobra prints nothing; this is the
	// single place that reports the error.
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(stderr, "Error: %s\n", err)
		return 1
	}
	return 0
}
