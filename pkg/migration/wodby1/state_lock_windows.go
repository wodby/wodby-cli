//go:build windows

package wodby1

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

var errMigrationStateLockContended = errors.New("migration state lock is contended")

func lockMigrationStateFile(file *os.File) error {
	var overlapped windows.Overlapped
	err := windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		&overlapped,
	)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return errMigrationStateLockContended
	}
	return err
}

func unlockMigrationStateFile(file *os.File) error {
	var overlapped windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
}
