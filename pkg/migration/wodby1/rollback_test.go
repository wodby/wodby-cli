package wodby1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func rollbackTestState(t *testing.T, existingApp bool) *MigrationState {
	t.Helper()
	identity := MigrationStateIdentity{
		Source:   MigrationStateSourceIdentity{Kind: "app", ID: "app-1", ConfigDigest: strings.Repeat("a", 64)},
		PlanHash: strings.Repeat("b", 64),
		Target:   MigrationStateTarget{OrgID: 1, ProjectID: 2, ClusterID: 3},
	}
	if existingApp {
		identity.Target.AppID = 101
		identity.Target.ExistingApp = true
	}
	state, err := NewMigrationState(identity, []string{"instance-1"})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestPlanRollbackDeletesOnlyWhatTheMigrationCreated(t *testing.T) {
	state := rollbackTestState(t, false)
	if err := state.SetAppTarget(900, MigrationResourceReady); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkAppOperationIntent(generatedStackOperation); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkAppOperationCreated(generatedStackOperation, 700, 701); err != nil {
		t.Fatal(err)
	}
	// One integration this migration created, one it reused.
	if err := state.MarkAppOperationIntent("integration_resolve.aaaa"); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkAppOperationCreated("integration_resolve.aaaa", 610, 0); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkAppOperationIntent("integration_resolve.bbbb"); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkAppOperationSuccessWithIDs("integration_resolve.bbbb", 620, 0); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanRollback(state)
	if err != nil {
		t.Fatal(err)
	}
	if plan.AppID != 900 || plan.StackID != 700 {
		t.Fatalf("plan = %#v", plan)
	}
	if len(plan.IntegrationIDs) != 1 || plan.IntegrationIDs[0] != 610 {
		t.Fatalf("only created integrations may be deleted: %#v", plan.IntegrationIDs)
	}
	if len(plan.ReusedIntegrations) != 1 || plan.ReusedIntegrations[0] != 620 {
		t.Fatalf("reused integrations must be reported and kept: %#v", plan.ReusedIntegrations)
	}
	// Deleting the app cascades, so instances are not listed separately.
	if len(plan.InstanceIDs) != 0 {
		t.Fatalf("instances = %#v", plan.InstanceIDs)
	}
}

func TestPlanRollbackKeepsAnAppItDidNotCreate(t *testing.T) {
	state := rollbackTestState(t, true)
	if err := state.SetAppTarget(101, MigrationResourceReady); err != nil {
		t.Fatal(err)
	}
	if err := state.SetInstanceTarget("instance-1", 555, MigrationResourceReady); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanRollback(state)
	if err != nil {
		t.Fatal(err)
	}
	// The app predates the migration; deleting it would take unrelated
	// instances with it.
	if plan.AppID != 0 {
		t.Fatalf("pre-existing app must never be deleted: %#v", plan)
	}
	if len(plan.InstanceIDs) != 1 || plan.InstanceIDs[0].ID != 555 {
		t.Fatalf("instances = %#v", plan.InstanceIDs)
	}
}

func TestPlanRollbackWillNotTouchIntegrationsOfUnknownOrigin(t *testing.T) {
	state := rollbackTestState(t, false)
	// A state written before provenance was recorded.
	state.TracksProvenance = false
	if err := state.SetAppTarget(900, MigrationResourceReady); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkAppOperationIntent("integration_resolve.aaaa"); err != nil {
		t.Fatal(err)
	}
	if err := state.MarkAppOperationSuccessWithIDs("integration_resolve.aaaa", 610, 0); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanRollback(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.IntegrationIDs) != 0 {
		t.Fatalf("unknown-origin integrations must not be deleted: %#v", plan.IntegrationIDs)
	}
	if len(plan.SkippedIntegrations) != 1 || plan.SkippedIntegrations[0] != 610 {
		t.Fatalf("unknown-origin integrations must be reported: %#v", plan.SkippedIntegrations)
	}
	if !strings.Contains(plan.Describe("demo"), "does not record") {
		t.Fatalf("description must surface the skip:\n%s", plan.Describe("demo"))
	}
}

func TestPlanRollbackRefusesAfterCutover(t *testing.T) {
	for _, test := range []struct {
		name  string
		apply func(*MigrationState)
	}{
		{"verified", func(s *MigrationState) { _ = s.SetPhase(MigrationPhaseVerify) }},
		{"complete", func(s *MigrationState) { _ = s.SetStatus(MigrationStatusComplete) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := rollbackTestState(t, false)
			if err := state.SetAppTarget(900, MigrationResourceReady); err != nil {
				t.Fatal(err)
			}
			if err := state.SetInstanceTarget("instance-1", 555, MigrationResourceReady); err != nil {
				t.Fatal(err)
			}
			test.apply(state)

			if _, err := PlanRollback(state); err == nil {
				t.Fatal("a migration serving live traffic must not be rolled back")
			}
		})
	}
}

func TestPlanRollbackOnAnUntouchedTargetIsANoOp(t *testing.T) {
	plan, err := PlanRollback(rollbackTestState(t, false))
	if err != nil {
		t.Fatal(err)
	}
	if !plan.empty() {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestRollbackDeletesInDependencyOrder(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/v1/apps/900":
			writeTargetExecutionJSON(t, w, TargetOperationResult{Success: true})
		case "/v1/stacks/700":
			writeTargetExecutionJSON(t, w, TargetOperationResult{Success: true})
		case "/v1/integrations/610":
			writeTargetExecutionJSON(t, w, TargetOperationResult{Success: true})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	state := rollbackTestState(t, false)
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := SaveMigrationState(statePath, state); err != nil {
		t.Fatal(err)
	}
	executor, err := NewMigrationExecutor(
		mustTargetExecutionClient(t, server.URL),
		MigrationExecutorOptions{StatePath: statePath},
	)
	if err != nil {
		t.Fatal(err)
	}

	plan := RollbackPlan{AppID: 900, StackID: 700, IntegrationIDs: []int{610}}
	if err := executor.Rollback(context.Background(), state, plan, "demo"); err != nil {
		t.Fatal(err)
	}
	// The stack cannot go before the app that references it, and integrations
	// cannot go before the services linking to them.
	want := []string{"DELETE /v1/apps/900", "DELETE /v1/stacks/700", "DELETE /v1/integrations/610"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
	// A rollback that finished must not leave state a later --apply could
	// resume onto deleted resources.
	if _, err := LoadMigrationState(statePath, state.Identity()); err == nil {
		t.Fatal("completed rollback must remove the resume state")
	}
}

func TestRollbackTreatsAlreadyDeletedResourcesAsDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	state := rollbackTestState(t, false)
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := SaveMigrationState(statePath, state); err != nil {
		t.Fatal(err)
	}
	executor, err := NewMigrationExecutor(
		mustTargetExecutionClient(t, server.URL),
		MigrationExecutorOptions{StatePath: statePath},
	)
	if err != nil {
		t.Fatal(err)
	}

	// Rerunning an interrupted rollback must not trip over its own progress.
	if err := executor.Rollback(context.Background(), state, RollbackPlan{AppID: 900}, "demo"); err != nil {
		t.Fatalf("already-deleted resources must not fail a rollback: %v", err)
	}
}
