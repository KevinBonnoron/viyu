//go:build unix

package sources

import (
	"io/fs"
	"os"
	"syscall"
)

// openKeyFile opens the key without following a symbolic link planted in its place.
func openKeyFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}

// privateKeyMode tells whether only the owner may read or write the key.
func privateKeyMode(mode fs.FileMode) bool {
	return mode.Perm()&0o077 == 0
}
