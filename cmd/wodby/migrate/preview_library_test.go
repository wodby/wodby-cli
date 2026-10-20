package migrate

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/wodby/wodby-cli/pkg/migration/wodby1"
	"github.com/wodby/wodby-cli/pkg/types"
)

type countingTransport struct{ requests atomic.Int64 }

func (t *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.requests.Add(1)
	return http.DefaultTransport.RoundTrip(req)
}

// The library preview, which the Wodby 2 backend runs for the Wodby Agent,
// builds the same plan as the command with the same choices, and sends its
// Wodby 2 requests through a transport the caller supplies.
func TestLibraryPreviewMatchesTheCommand(t *testing.T) {
	fixture := newMigrationAPIFixture(t, "owner", "ok", false)
	defer fixture.Close()
	fixture.setSourceKind("instance")
	setMigrationTargetConfig(t, fixture.target.URL+"/v1", "target-key", "")

	var output bytes.Buffer
	cmd := newWodby1InstanceCommand()
	cmd.SilenceUsage = true
	cmd.SetOut(&output)
	cmd.SetArgs(fixture.instancePlanArgs("", "json"))
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var fromCommand wodby1.Plan
	if err := json.Unmarshal(output.Bytes(), &fromCommand); err != nil {
		t.Fatal(err)
	}

	source, err := wodby1.NewSourceClient(fixture.source.URL, testSourceToken)
	if err != nil {
		t.Fatal(err)
	}
	transport := &countingTransport{}
	target, err := wodby1.NewTargetClient(types.APIConfig{Endpoint: fixture.target.URL + "/v1", Key: "target-key", Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	result, err := wodby1.Preview(context.Background(), source, target, wodby1.PreviewRequest{
		SourceKind: "instance", SourceID: "instance-1",
		TargetProject: "22", TargetCluster: "33", TargetStackID: 7,
		TargetEnvMap: map[string]string{"prod": "prod"},
		SkipCode:     true, SkipData: true, AddMissingServices: true,
		SourceToken: testSourceToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Prepared == nil {
		t.Fatal("the target was not inspected")
	}
	if transport.requests.Load() == 0 {
		t.Fatal("the caller's transport was not used")
	}
	want, _ := json.Marshal(fromCommand)
	got, _ := json.Marshal(result.Plan)
	if !bytes.Equal(want, got) {
		t.Fatalf("library plan differs from the command plan\ncommand: %s\nlibrary: %s", want, got)
	}
}

// libraryRunEnv runs a migration through the library with its plan and state in
// records, the way the Wodby 2 backend runs one for the Wodby Agent.
func libraryRunEnv(t *testing.T, fixture *migrationAPIFixture, records wodby1.MigrationRecords, output *bytes.Buffer) *wodby1.RunEnv {
	t.Helper()
	target, err := wodby1.NewTargetClient(types.APIConfig{Endpoint: fixture.target.URL + "/v1", Key: "target-key"})
	if err != nil {
		t.Fatal(err)
	}
	return &wodby1.RunEnv{
		Ctx: context.Background(), Out: output, Target: target,
		Artifacts: func(string, string, string) (wodby1.MigrationArtifacts, error) {
			return wodby1.NewRecordArtifacts(records), nil
		},
	}
}

func libraryApplyOptions(fixture *migrationAPIFixture) *wodby1.RunOptions {
	opts := wodby1.DefaultRunOptions()
	opts.SourceBaseURL, opts.SourceToken = fixture.source.URL, testSourceToken
	opts.TargetProject, opts.TargetCluster, opts.TargetStackID = "22", "33", 7
	opts.TargetEnvMap = []string{"prod=prod"}
	opts.SkipCode, opts.SkipData = true, true
	opts.Apply, opts.Yes = true, true
	return opts
}

// The library apply keeps the reviewed plan and the resume state in the
// caller's records instead of files, and refuses a plan other than the one
// approved elsewhere before it changes anything.
func TestLibraryApplyKeepsPlanAndStateInRecords(t *testing.T) {
	fixture := newMigrationAPIFixture(t, "admin", "ok", false)
	defer fixture.Close()
	t.Setenv("TMPDIR", t.TempDir())

	refused := wodby1.NewMemoryMigrationRecords()
	var output bytes.Buffer
	env := libraryRunEnv(t, fixture, refused, &output)
	env.ApprovedPlanHash = strings.Repeat("0", 64)
	err := wodby1.RunSingle(env, "app", "app-1", libraryApplyOptions(fixture))
	if err == nil || !strings.Contains(err.Error(), "changed after it was approved") {
		t.Fatalf("apply with another approved plan: %v", err)
	}
	if keys, _ := refused.Keys(""); len(keys) != 0 {
		t.Fatalf("a refused apply stored %v", keys)
	}

	records := wodby1.NewMemoryMigrationRecords()
	err = wodby1.RunSingle(libraryRunEnv(t, fixture, records, &output), "app", "app-1", libraryApplyOptions(fixture))
	// The fixture leaves app creation unconfirmed, as in the command test.
	if err == nil || !strings.Contains(err.Error(), "app creation is ambiguous") {
		t.Fatalf("apply error = %v", err)
	}
	artifacts := wodby1.NewRecordArtifacts(records)
	plan, err := artifacts.LoadPlan()
	if err != nil {
		t.Fatal(err)
	}
	state, err := artifacts.InspectState(artifacts.StateLocation())
	if err != nil || state.PlanHash != plan.PlanHash {
		t.Fatalf("state = %+v, %v", state, err)
	}
	if entries, _ := os.ReadDir(filepath.Join(os.TempDir(), "wodby-migrations")); len(entries) != 0 {
		t.Fatalf("the library apply wrote files: %v", entries)
	}
}
