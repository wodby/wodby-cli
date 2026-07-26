package wodby1

import (
	"errors"
	"fmt"
	"os"
)

var ErrMigrationStateLocked = errors.New("migration state is locked by another process")

// MigrationStateLock serializes one complete CLI invocation for a state path.
// The revision check in SaveMigrationState detects stale in-memory writers,
// while this OS lock closes the check/rename race between separate processes.
type MigrationStateLock struct {
	file *os.File
}

func AcquireMigrationStateLock(statePath string) (*MigrationStateLock, error) {
	if statePath == "" {
		return nil, invalidStateError("state path is required")
	}
	lockPath := statePath + ".lock"
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, migrationStateFileMode)
	if err != nil {
		return nil, fmt.Errorf("open migration state lock: %w", err)
	}
	closeOnError := func(lockErr error) (*MigrationStateLock, error) {
		_ = file.Close()
		return nil, lockErr
	}
	info, err := file.Stat()
	if err != nil {
		return closeOnError(fmt.Errorf("inspect migration state lock: %w", err))
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != migrationStateFileMode {
		return closeOnError(ErrMigrationStateInsecure)
	}
	if err := lockMigrationStateFile(file); err != nil {
		if errors.Is(err, errMigrationStateLockContended) {
			return closeOnError(ErrMigrationStateLocked)
		}
		return closeOnError(fmt.Errorf("lock migration state: %w", err))
	}
	return &MigrationStateLock{file: file}, nil
}

func (l *MigrationStateLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	unlockErr := unlockMigrationStateFile(file)
	closeErr := file.Close()
	if unlockErr != nil {
		return fmt.Errorf("unlock migration state: %w", unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close migration state lock: %w", closeErr)
	}
	return nil
}
