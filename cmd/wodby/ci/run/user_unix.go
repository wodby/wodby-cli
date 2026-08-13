//go:build !windows

package run

import (
	"fmt"
	"os"
	"syscall"

	"github.com/pkg/errors"
)

func defaultCurrentHostUser() (string, error) {
	return fmt.Sprintf("%d:%d", syscall.Getuid(), syscall.Getgid()), nil
}

func defaultWorkspaceOwner(p string) (string, error) {
	info, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.Errorf("could not determine owner for path %s", p)
	}
	return fmt.Sprintf("%d:%d", stat.Uid, stat.Gid), nil
}

func canChownCacheDirectories() bool {
	return os.Geteuid() == 0
}
