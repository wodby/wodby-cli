//go:build !windows

package run

import (
	"fmt"
	"syscall"
)

func defaultCurrentHostUser() (string, error) {
	return fmt.Sprintf("%d:%d", syscall.Getuid(), syscall.Getgid()), nil
}
