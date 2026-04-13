//go:build windows

package run

import "os"

func hostUserFromFileInfo(_ string, _ os.FileInfo) (string, error) {
	return "", nil
}
