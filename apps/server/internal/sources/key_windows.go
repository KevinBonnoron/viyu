package sources

import (
	"errors"
	"io/fs"
	"os"
)

// openKeyFile opens the key, refusing a symbolic link planted in its place (Windows has no O_NOFOLLOW).
func openKeyFile(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, errors.New("source encryption key must not be a symbolic link")
	}
	return os.Open(path)
}

// privateKeyMode accepts any mode: Windows files carry no Unix permission bits, so the key is as private as
// the access control list of the directory it lives in.
func privateKeyMode(fs.FileMode) bool {
	return true
}
