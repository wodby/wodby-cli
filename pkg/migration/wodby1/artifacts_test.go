package wodby1

import (
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"testing"
)

func recordTestIdentity() MigrationStateIdentity {
	return MigrationStateIdentity{
		Source:   MigrationStateSourceIdentity{Kind: "app", ID: "app-1", ConfigDigest: strings.Repeat("a", 64)},
		PlanHash: strings.Repeat("b", 64),
		Target:   MigrationStateTarget{OrgID: 1, ProjectID: 2, ClusterID: 3},
	}
}

// Records keep the same guarantees as state files: one identity per state, a
// revision that refuses stale writers, and removal only when it is safe.
func TestRecordArtifactsKeepStateGuarantees(t *testing.T) {
	records := NewMemoryMigrationRecords()
	artifacts := NewRecordArtifacts(records)
	location := artifacts.StateLocation()
	identity := recordTestIdentity()

	if _, err := artifacts.LoadState(location, identity); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing state error = %v, want fs.ErrNotExist", err)
	}
	store := artifacts.StateStore(location)
	state, created, err := store.LoadOrInitialize(identity, []string{"instance-1"})
	if err != nil || !created || state.Revision != 1 {
		t.Fatalf("initialize = %+v, %v, %v", state, created, err)
	}
	stale := *state
	if err := state.MarkAppOperationIntent("create"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(state); err != nil || state.Revision != 2 {
		t.Fatalf("save = %v, revision %d", err, state.Revision)
	}
	if err := store.Save(&stale); !errors.Is(err, ErrMigrationStateConcurrentUpdate) {
		t.Fatalf("stale save error = %v", err)
	}

	resumed, created, err := store.LoadOrInitialize(identity, []string{"instance-1"})
	if err != nil || created || resumed.Revision != 2 || resumed.App.Operations["create"].Status != MigrationOperationIntent {
		t.Fatalf("resume = %+v, %v, %v", resumed, created, err)
	}
	other := identity
	other.PlanHash = strings.Repeat("c", 64)
	if _, err := artifacts.LoadState(location, other); !errors.Is(err, ErrMigrationStateIdentityMismatch) {
		t.Fatalf("other identity error = %v", err)
	}
	// An intended app creation may have happened, so a restart must not drop
	// the state without cleanup, but a rollback may.
	if err := artifacts.RemoveRestartableState(location, identity); !errors.Is(err, ErrMigrationStateUnsafeRestart) {
		t.Fatalf("restart removal error = %v", err)
	}
	if err := store.RemoveAfterRollback(identity); err != nil {
		t.Fatal(err)
	}
	if exists, err := artifacts.Exists(location); err != nil || exists {
		t.Fatalf("state after rollback exists = %v, %v", exists, err)
	}
}

func TestRecordArtifactsKeepServerStatesAndPlan(t *testing.T) {
	records := NewMemoryMigrationRecords()
	artifacts := NewRecordArtifacts(records)
	for _, app := range []string{"app-2", "app-1"} {
		identity := recordTestIdentity()
		identity.Source.ID = app
		if _, _, err := artifacts.StateStore(artifacts.ServerAppState(app)).LoadOrInitialize(identity, nil); err != nil {
			t.Fatal(err)
		}
	}
	locations, err := artifacts.ServerStates()
	if err != nil || len(locations) != 2 || locations[0] != artifacts.ServerAppState("app-1") {
		t.Fatalf("server states = %v, %v", locations, err)
	}

	plan := reviewedPlanFixture(t)
	if err := artifacts.SavePlan(plan); err != nil {
		t.Fatal(err)
	}
	if err := artifacts.SavePlan(plan); err != nil {
		t.Fatalf("replacing the plan: %v", err)
	}
	loaded, err := artifacts.LoadPlan()
	if err != nil || loaded.PlanHash != plan.PlanHash {
		t.Fatalf("plan = %s, %v", loaded.PlanHash, err)
	}
	tampered := plan
	tampered.PlanHash = strings.Repeat("0", 64)
	if err := records.Put(recordPlanKey, mustJSON(t, tampered), 2, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := artifacts.LoadPlan(); !errors.Is(err, ErrMigrationPlanInvalid) {
		t.Fatalf("tampered plan error = %v", err)
	}

	lock, err := artifacts.Lock(artifacts.StateLocation())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := artifacts.Lock(artifacts.StateLocation()); !errors.Is(err, ErrMigrationStateLocked) {
		t.Fatalf("second lock error = %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}

// The executor saves through the store it is given.
func TestExecutorSavesThroughAStateStore(t *testing.T) {
	artifacts := NewRecordArtifacts(NewMemoryMigrationRecords())
	store := artifacts.StateStore(artifacts.StateLocation())
	state, _, err := store.LoadOrInitialize(recordTestIdentity(), []string{"instance-1"})
	if err != nil {
		t.Fatal(err)
	}
	executor, err := NewMigrationExecutor(&TargetClient{}, MigrationExecutorOptions{State: store})
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.startPhase(state, MigrationPhasePrepare); err != nil {
		t.Fatal(err)
	}
	saved, err := artifacts.InspectState(artifacts.StateLocation())
	if err != nil || saved.Phase != MigrationPhasePrepare || saved.Revision != state.Revision {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
}

// A plan approved elsewhere is applied only as approved.
func TestRunEnvRefusesAPlanOtherThanTheApprovedOne(t *testing.T) {
	plan := reviewedPlanFixture(t)
	env := &RunEnv{ApprovedPlanHash: plan.PlanHash}
	if err := env.checkApprovedPlan(plan); err != nil {
		t.Fatal(err)
	}
	env.ApprovedPlanHash = strings.Repeat("0", 64)
	if err := env.checkApprovedPlan(plan); err == nil || !strings.Contains(err.Error(), "changed after it was approved") {
		t.Fatalf("error = %v", err)
	}
}

func mustJSON(t *testing.T, value interface{}) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
