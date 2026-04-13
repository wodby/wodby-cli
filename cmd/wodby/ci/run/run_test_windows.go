//go:build windows

package run

import "os"

func expectedHostUserForFileInfo(_ string, _ os.FileInfo) (string, error) {
	return "", nil
}
