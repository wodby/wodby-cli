//go:build !windows

package run

import (
	"fmt"
	"os"
	"syscall"

	"github.com/pkg/errors"
)

func hostUserFromFileInfo(p string, info os.FileInfo) (string, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.Errorf("could not determine owner for path %s", p)
	}

	return fmt.Sprintf("%d:%d", stat.Uid, stat.Gid), nil
}
