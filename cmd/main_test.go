package cmd

import (
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestMain swaps the OS keyring for go-keyring's in-memory mock, so the cmd
// tests never read or write the developer's keyring.
func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}
