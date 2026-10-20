package migrate

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
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
