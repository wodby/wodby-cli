package wodby1

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrationStateLockSerializesProcesses(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "migration-state.json")
	first, err := AcquireMigrationStateLock(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	if _, err := AcquireMigrationStateLock(statePath); !errors.Is(err, ErrMigrationStateLocked) {
		t.Fatalf("second lock error = %v", err)
	}
	info, err := os.Stat(statePath + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != migrationStateFileMode {
		t.Fatalf("lock mode = %04o, want %04o", info.Mode().Perm(), migrationStateFileMode)
	}

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := AcquireMigrationStateLock(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationStateLockRejectsInsecureFile(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "migration-state.json")
	lockPath := statePath + ".lock"
	if err := os.WriteFile(lockPath, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireMigrationStateLock(statePath); !errors.Is(err, ErrMigrationStateInsecure) {
		t.Fatalf("insecure lock error = %v", err)
	}
}
