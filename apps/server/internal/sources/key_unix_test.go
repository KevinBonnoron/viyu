//go:build unix

package sources

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Unix modes say who may read the key; Windows files carry none (see key_windows.go).
func TestMasterKeyIsOwnerOnlyOnUnix(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := masterKey(dir, ""); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "source_connections.key"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the created key is not 0600: %v %v", info, err)
	}

	readable := filepath.Join(dir, "readable.key")
	if err := os.WriteFile(readable, bytes.Repeat([]byte{7}, 32), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := masterKey(dir, readable); err == nil {
		t.Fatal("a world-readable key was accepted")
	}
}
