package wodby1

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMigrationStateFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration-state.json")
	state := mustNewMigrationState(t)
	if err := SaveMigrationState(path, state); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != migrationStateFileMode {
		t.Fatalf("state mode = %04o, want %04o", got, migrationStateFileMode)
	}

	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMigrationState(path, state.Identity()); !errors.Is(err, ErrMigrationStateInsecure) {
		t.Fatalf("load permissive state error = %v", err)
	}

	if err := os.Chmod(path, 0400); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMigrationState(path, state.Identity()); !errors.Is(err, ErrMigrationStateInsecure) {
		t.Fatalf("load non-standard state mode error = %v", err)
	}
}

func TestMigrationStateAtomicSaveAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "migration-state.json")
	identity := testMigrationStateIdentity()

	state, created, err := LoadOrInitializeMigrationState(path, identity, []string{"instance-b", "instance-a"})
	if err != nil {
		t.Fatal(err)
	}
	if !created || state.Revision != 1 {
		t.Fatalf("initialized state = created %t, revision %d", created, state.Revision)
	}

	if err := state.MarkAppOperationIntent("create"); err != nil {
		t.Fatal(err)
	}
	if err := SaveMigrationState(path, state); err != nil {
		t.Fatal(err)
	}
	if err := state.SetAppTarget(101, MigrationResourceReady); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkAppOperationSuccessWithIDs("create", 101, 1001); err != nil {
		t.Fatal(err)
	}
	if err := state.SetPhase(MigrationPhaseSyncData); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkInstanceOperationIntent("instance-a", "create"); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkInstanceOperationFailure("instance-a", "create", "target_timeout"); err != nil {
		t.Fatal(err)
	}
	if err := SaveMigrationState(path, state); err != nil {
		t.Fatal(err)
	}

	reloaded, created, err := LoadOrInitializeMigrationState(
		path,
		identity,
		[]string{"instance-a", "instance-b"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("existing state was reported as newly created")
	}
	if !reflect.DeepEqual(reloaded, state) {
		t.Fatalf("reloaded state differs:\n got: %#v\nwant: %#v", reloaded, state)
	}
	if reloaded.Revision != 3 {
		t.Fatalf("revision = %d, want 3", reloaded.Revision)
	}
	if got := reloaded.Instances["instance-a"].Operations["create"]; got.Attempts != 1 ||
		got.Status != MigrationOperationFailed ||
		got.FailureCode != "target_timeout" ||
		got.IntentAt.IsZero() ||
		got.UpdatedAt.Before(got.IntentAt) {
		t.Fatalf("instance operation = %#v", got)
	}
	appOperation := reloaded.App.Operations["create"]
	if appOperation.TargetID != 101 || appOperation.TaskID != 1001 ||
		appOperation.Status != MigrationOperationSucceeded ||
		appOperation.IntentAt.IsZero() ||
		appOperation.UpdatedAt.Before(appOperation.IntentAt) {
		t.Fatalf("app operation = %#v", appOperation)
	}
	if reloaded.Phase != MigrationPhaseSyncData {
		t.Fatalf("phase = %q", reloaded.Phase)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("state directory contains temporary artifacts: %#v", entries)
	}
}

func TestMigrationStateRestartSafety(t *testing.T) {
	t.Run("initialized", func(t *testing.T) {
		state := mustNewMigrationState(t)
		if !state.CanRestartSafely() {
			t.Fatal("initialized state should be restartable")
		}
	})

	t.Run("definitive API rejection", func(t *testing.T) {
		state := mustNewMigrationState(t)
		if err := state.SetPhase(MigrationPhasePrepare); err != nil {
			t.Fatal(err)
		}
		if err := state.MarkAppOperationIntent("create"); err != nil {
			t.Fatal(err)
		}
		if err := state.MarkAppOperationFailure("create", "api_rejected"); err != nil {
			t.Fatal(err)
		}
		if err := state.MarkInstanceOperationIntent("instance-a", "create"); err != nil {
			t.Fatal(err)
		}
		if err := state.MarkInstanceOperationFailure("instance-a", "create", "api_rejected"); err != nil {
			t.Fatal(err)
		}
		if !state.CanRestartSafely() {
			t.Fatal("definitively rejected create should be restartable")
		}
	})

	t.Run("successful mutation", func(t *testing.T) {
		state := mustNewMigrationState(t)
		if err := state.MarkAppOperationIntent("create"); err != nil {
			t.Fatal(err)
		}
		if err := state.MarkAppOperationSuccessWithIDs("create", 101, 0); err != nil {
			t.Fatal(err)
		}
		if state.CanRestartSafely() {
			t.Fatal("successful mutation must not be restartable")
		}
	})

	t.Run("ambiguous mutation", func(t *testing.T) {
		state := mustNewMigrationState(t)
		if err := state.MarkAppOperationIntent("create"); err != nil {
			t.Fatal(err)
		}
		if err := state.MarkAppOperationAmbiguous("create"); err != nil {
			t.Fatal(err)
		}
		if state.CanRestartSafely() {
			t.Fatal("ambiguous mutation must not be restartable")
		}
	})
}

func TestRemoveRestartableMigrationState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration-state.json")
	state := mustNewMigrationState(t)
	if err := state.SetPhase(MigrationPhasePrepare); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkAppOperationIntent("create"); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkAppOperationFailure("create", "api_rejected"); err != nil {
		t.Fatal(err)
	}
	if err := SaveMigrationState(path, state); err != nil {
		t.Fatal(err)
	}

	inspected, err := InspectMigrationState(path)
	if err != nil {
		t.Fatal(err)
	}
	if inspected.Identity() != state.Identity() || !inspected.CanRestartSafely() {
		t.Fatalf("inspected state = %#v", inspected)
	}
	if err := RemoveRestartableMigrationState(path, state.Identity()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed state stat error = %v", err)
	}
}

func TestRemoveMigrationStateAfterTargetDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration-state.json")
	state := mustNewMigrationState(t)
	if err := state.MarkAppOperationIntent("create"); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkAppOperationSuccessWithIDs("create", 101, 0); err != nil {
		t.Fatal(err)
	}
	if err := state.SetAppTarget(101, MigrationResourceReady); err != nil {
		t.Fatal(err)
	}
	if err := SaveMigrationState(path, state); err != nil {
		t.Fatal(err)
	}

	if err := RemoveMigrationStateAfterTargetDeletion(path, state.Identity(), 102); !errors.Is(err, ErrMigrationStateIdentityMismatch) {
		t.Fatalf("mismatched deleted target error = %v", err)
	}
	if err := RemoveMigrationStateAfterTargetDeletion(path, state.Identity(), 101); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed state stat error = %v", err)
	}
}

func TestMigrationStateIdentityMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration-state.json")
	state := mustNewMigrationState(t)
	if err := SaveMigrationState(path, state); err != nil {
		t.Fatal(err)
	}

	cases := map[string]func(*MigrationStateIdentity){
		"source kind":   func(identity *MigrationStateIdentity) { identity.Source.Kind = "server" },
		"source id":     func(identity *MigrationStateIdentity) { identity.Source.ID = "another-app" },
		"config digest": func(identity *MigrationStateIdentity) { identity.Source.ConfigDigest = strings.Repeat("c", 64) },
		"plan hash":     func(identity *MigrationStateIdentity) { identity.PlanHash = strings.Repeat("d", 64) },
		"org":           func(identity *MigrationStateIdentity) { identity.Target.OrgID++ },
		"project":       func(identity *MigrationStateIdentity) { identity.Target.ProjectID++ },
		"cluster":       func(identity *MigrationStateIdentity) { identity.Target.ClusterID++ },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			identity := state.Identity()
			mutate(&identity)
			if _, err := LoadMigrationState(path, identity); !errors.Is(err, ErrMigrationStateIdentityMismatch) {
				t.Fatalf("load identity mismatch error = %v", err)
			}
		})
	}

	if _, _, err := LoadOrInitializeMigrationState(
		path,
		state.Identity(),
		[]string{"instance-a"},
	); !errors.Is(err, ErrMigrationStateIdentityMismatch) {
		t.Fatalf("load instance-set mismatch error = %v", err)
	}
}

func TestMigrationStateAcceptsOrganizationOwnedTarget(t *testing.T) {
	identity := testMigrationStateIdentity()
	identity.Target.ProjectID = 0
	state, err := NewMigrationState(identity, []string{"instance-a"})
	if err != nil {
		t.Fatal(err)
	}
	if state.Target.ProjectID != 0 {
		t.Fatalf("target = %#v", state.Target)
	}

	identity.Target.ProjectID = -1
	if _, err := NewMigrationState(identity, []string{"instance-a"}); err == nil {
		t.Fatal("negative target project ID must be rejected")
	}
}

func TestMigrationStateBackupDigestCanChangeWithoutChangingIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration-state.json")
	state := mustNewMigrationState(t)
	identity := state.Identity()

	if err := state.SetBackupDigest(strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	if err := SaveMigrationState(path, state); err != nil {
		t.Fatal(err)
	}
	if err := state.SetBackupDigest(strings.Repeat("d", 64)); err != nil {
		t.Fatal(err)
	}
	if err := SaveMigrationState(path, state); err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadMigrationState(path, identity)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Source.BackupDigest; got != strings.Repeat("d", 64) {
		t.Fatalf("backup digest = %q", got)
	}
	if reloaded.Identity() != identity {
		t.Fatalf("backup refresh changed state identity: %#v", reloaded.Identity())
	}

	configChanged := identity
	configChanged.Source.ConfigDigest = strings.Repeat("e", 64)
	if _, err := LoadMigrationState(path, configChanged); !errors.Is(err, ErrMigrationStateIdentityMismatch) {
		t.Fatalf("config identity mismatch error = %v", err)
	}
	if err := reloaded.SetBackupDigest("https://storage.example/signed?token=secret"); !errors.Is(err, ErrMigrationStateInvalid) {
		t.Fatalf("signed backup digest error = %v", err)
	}
}

func TestMigrationStateAmbiguousOperationRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration-state.json")
	state := mustNewMigrationState(t)
	if err := state.SetPhase(MigrationPhasePrepare); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkInstanceOperationIntent("instance-a", "instance.create"); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkInstanceOperationAmbiguousWithIDs(
		"instance-a",
		"instance.create",
		202,
		2002,
	); err != nil {
		t.Fatal(err)
	}
	if err := SaveMigrationState(path, state); err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadMigrationState(path, state.Identity())
	if err != nil {
		t.Fatal(err)
	}
	operation := reloaded.Instances["instance-a"].Operations["instance.create"]
	if operation.Status != MigrationOperationAmbiguous ||
		operation.TargetID != 202 ||
		operation.TaskID != 2002 ||
		operation.IntentAt.IsZero() ||
		operation.UpdatedAt.Before(operation.IntentAt) {
		t.Fatalf("ambiguous operation = %#v", operation)
	}
	if reloaded.Status != MigrationStatusFailed {
		t.Fatalf("migration status = %q", reloaded.Status)
	}

	if err := reloaded.MarkInstanceOperationIntent("instance-a", "instance.create"); err != nil {
		t.Fatal(err)
	}
	retried := reloaded.Instances["instance-a"].Operations["instance.create"]
	if retried.Status != MigrationOperationIntent ||
		retried.Attempts != 2 ||
		retried.TargetID != 0 ||
		retried.TaskID != 0 {
		t.Fatalf("retried operation = %#v", retried)
	}
}

func TestMigrationStateAcceptedOperationRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration-state.json")
	state := mustNewMigrationState(t)
	if err := state.MarkInstanceOperationIntent("instance-a", "build.start"); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkInstanceOperationAcceptedWithIDs(
		"instance-a",
		"build.start",
		202,
		2002,
	); err != nil {
		t.Fatal(err)
	}
	if err := SaveMigrationState(path, state); err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadMigrationState(path, state.Identity())
	if err != nil {
		t.Fatal(err)
	}
	operation := reloaded.Instances["instance-a"].Operations["build.start"]
	if operation.Status != MigrationOperationAccepted ||
		operation.TargetID != 202 ||
		operation.TaskID != 2002 ||
		operation.Attempts != 1 {
		t.Fatalf("accepted operation = %#v", operation)
	}
	if reloaded.Status != MigrationStatusRunning {
		t.Fatalf("migration status = %q", reloaded.Status)
	}

	if err := reloaded.MarkInstanceOperationAcceptedWithIDs(
		"instance-a",
		"build.start",
		202,
		2002,
	); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Instances["instance-a"].Operations["build.start"].Attempts; got != 1 {
		t.Fatalf("accepted operation attempts = %d, want 1", got)
	}
}

func TestMigrationStateRejectsCorruptAndUnknownSchema(t *testing.T) {
	dir := t.TempDir()
	identity := testMigrationStateIdentity()

	corruptPath := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corruptPath, []byte("{"), migrationStateFileMode); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMigrationState(corruptPath, identity); !errors.Is(err, ErrMigrationStateInvalid) {
		t.Fatalf("corrupt state error = %v", err)
	}

	unknownSchemaPath := filepath.Join(dir, "unknown-schema.json")
	state := mustNewMigrationState(t)
	state.Schema = "wodby1-migration-state/v99"
	state.Revision = 1
	writeMigrationStateFixture(t, unknownSchemaPath, state)
	if _, err := LoadMigrationState(unknownSchemaPath, identity); !errors.Is(err, ErrMigrationStateInvalid) {
		t.Fatalf("unknown schema error = %v", err)
	}

	unknownFieldPath := filepath.Join(dir, "unknown-field.json")
	valid := mustNewMigrationState(t)
	valid.Revision = 1
	data, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), `"revision":1`, `"revision":1,"backupUrl":"https://storage.example/signed?token=secret"`, 1))
	if err := os.WriteFile(unknownFieldPath, data, migrationStateFileMode); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMigrationState(unknownFieldPath, identity); !errors.Is(err, ErrMigrationStateInvalid) {
		t.Fatalf("unknown payload field error = %v", err)
	}
}

func TestMigrationStateContainsNoSensitivePayloadFields(t *testing.T) {
	state := mustNewMigrationState(t)
	if err := state.MarkAppOperationIntent("route.create"); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkAppOperationFailure("route.create", "target_rejected"); err != nil {
		t.Fatal(err)
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	jsonText := string(data)
	for _, forbidden := range []string{
		"password",
		"secret",
		"token",
		"backupUrl",
		"signedUrl",
		"https://",
	} {
		if strings.Contains(jsonText, forbidden) {
			t.Fatalf("state contains forbidden payload marker %q: %s", forbidden, jsonText)
		}
	}

	if err := state.MarkInstanceOperationIntent(
		"instance-a",
		"https://storage.example/backup?token=secret",
	); !errors.Is(err, ErrMigrationStateInvalid) {
		t.Fatalf("signed URL operation error = %v", err)
	}
	if err := state.MarkInstanceOperationFailure(
		"instance-a",
		"create",
		"https://storage.example/backup?token=secret",
	); !errors.Is(err, ErrMigrationStateInvalid) {
		t.Fatalf("signed URL failure code error = %v", err)
	}
}

func TestMigrationStateOperationEncodingIsDeterministic(t *testing.T) {
	state := mustNewMigrationState(t)
	for _, operation := range []string{"route.create", "app.create", "imports.verify"} {
		if err := state.MarkAppOperationIntent(operation); err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	appIndex := strings.Index(text, `"app.create"`)
	importIndex := strings.Index(text, `"imports.verify"`)
	routeIndex := strings.Index(text, `"route.create"`)
	if appIndex == -1 || importIndex == -1 || routeIndex == -1 ||
		!(appIndex < importIndex && importIndex < routeIndex) {
		t.Fatalf("operations were not deterministically key-sorted: %s", text)
	}
}

func TestMigrationStateValidatesPhaseAndOperationMetadata(t *testing.T) {
	state := mustNewMigrationState(t)
	state.Phase = MigrationPhase("apply")
	if err := state.Validate(); !errors.Is(err, ErrMigrationStateInvalid) {
		t.Fatalf("invalid phase validation error = %v", err)
	}
	state.Phase = MigrationPhasePrepare

	if err := state.MarkAppOperationIntent("app.create"); err != nil {
		t.Fatal(err)
	}
	operation := state.App.Operations["app.create"]
	operation.TargetID = -1
	state.App.Operations["app.create"] = operation
	if err := state.Validate(); !errors.Is(err, ErrMigrationStateInvalid) {
		t.Fatalf("negative operation target validation error = %v", err)
	}

	operation.TargetID = 0
	operation.UpdatedAt = operation.IntentAt.Add(-1)
	state.App.Operations["app.create"] = operation
	if err := state.Validate(); !errors.Is(err, ErrMigrationStateInvalid) {
		t.Fatalf("operation timestamp validation error = %v", err)
	}
}

func TestMigrationStateRejectsStaleWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration-state.json")
	state := mustNewMigrationState(t)
	if err := SaveMigrationState(path, state); err != nil {
		t.Fatal(err)
	}
	stale, err := LoadMigrationState(path, state.Identity())
	if err != nil {
		t.Fatal(err)
	}

	if err := state.MarkAppOperationIntent("create"); err != nil {
		t.Fatal(err)
	}
	if err := SaveMigrationState(path, state); err != nil {
		t.Fatal(err)
	}
	if err := stale.MarkAppOperationIntent("create"); err != nil {
		t.Fatal(err)
	}
	if err := SaveMigrationState(path, stale); !errors.Is(err, ErrMigrationStateConcurrentUpdate) {
		t.Fatalf("stale save error = %v", err)
	}
}

func mustNewMigrationState(t *testing.T) *MigrationState {
	t.Helper()
	state, err := NewMigrationState(
		testMigrationStateIdentity(),
		[]string{"instance-a", "instance-b"},
	)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func testMigrationStateIdentity() MigrationStateIdentity {
	return MigrationStateIdentity{
		Source: MigrationStateSourceIdentity{
			Kind:         "app",
			ID:           "source-app-uuid",
			ConfigDigest: strings.Repeat("a", 64),
		},
		PlanHash: strings.Repeat("b", 64),
		Target: MigrationStateTarget{
			OrgID:     11,
			ProjectID: 22,
			ClusterID: 33,
		},
	}
}

func writeMigrationStateFixture(t *testing.T, path string, state *MigrationState) {
	t.Helper()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, migrationStateFileMode); err != nil {
		t.Fatal(err)
	}
}
