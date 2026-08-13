//go:build !windows

package ciuser

import (
	"fmt"
	"os"
	"syscall"

	"github.com/pkg/errors"
)

func currentHostUser() (string, error) {
	return fmt.Sprintf("%d:%d", syscall.Getuid(), syscall.Getgid()), nil
}

func workspaceOwner(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.Errorf("could not determine owner for path %s", path)
	}
	return fmt.Sprintf("%d:%d", stat.Uid, stat.Gid), nil
}

// CanChownDirectories reports whether newly created cache directories can be
// reassigned to the mapped container identity.
func CanChownDirectories() bool {
	return os.Geteuid() == 0
}
