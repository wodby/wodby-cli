package wodby1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// Wodby 2 publishes compatible provider revisions and moves integrations to
// them. A migration reviewed before such a release must still resume.
func TestEnsureTargetIntegrationsAcceptsNewerProviderRevision(t *testing.T) {
	for _, tc := range []struct {
		name               string
		providerID         int
		integrationRevID   int
		revisionProviderID int
		wantErr            string
	}{
		{name: "integration on the newer revision", providerID: 5, integrationRevID: 902},
		{name: "integration on an older revision", providerID: 5, integrationRevID: 901, revisionProviderID: 5},
		{name: "integration moved to another provider", providerID: 5, integrationRevID: 950, revisionProviderID: 6, wantErr: "no longer matches"},
		{name: "provider replaced", providerID: 7, integrationRevID: 902, wantErr: "changed after the migration plan was reviewed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/providers/by-name/github":
					writeTargetExecutionJSON(t, w, TargetProvider{ID: tc.providerID, RevID: 902, Name: "github"})
				case "/v1/integrations/70":
					writeTargetExecutionJSON(t, w, TargetIntegration{ID: 70, OrgID: 1, ProviderRevID: tc.integrationRevID, Title: "Git"})
				case "/v1/provider-revisions/" + strconv.Itoa(tc.integrationRevID):
					writeTargetExecutionJSON(t, w, TargetProviderRevision{ID: tc.integrationRevID, Name: "github", ProviderID: tc.revisionProviderID})
				default:
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
				}
			}))
			defer server.Close()

			state, statePath := newExecutorTestState(t)
			operation := operationKey("integration_resolve", "git")
			if err := state.MarkAppOperationIntent(operation); err != nil {
				t.Fatal(err)
			}
			if err := state.MarkAppOperationSuccessWithIDs(operation, 70, 0); err != nil {
				t.Fatal(err)
			}
			executor := &MigrationExecutor{target: mustTargetExecutionClient(t, server.URL), state: FileStateStore(statePath)}
			plan := Plan{Target: PlanTarget{OrgID: 1, ProjectID: 2, ClusterID: 3}}
			prepared := PreparedMigration{Integrations: []PreparedIntegration{{
				Key: "git", ProviderName: "github", ProviderID: 5, ProviderRevID: 900, Kind: "git",
			}}}

			result, err := executor.ensureTargetIntegrations(context.Background(), state, plan, prepared)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if item := result.Integrations[0]; item.TargetID != 70 || item.ProviderRevID != 902 {
				t.Fatalf("integration = %+v", item)
			}
		})
	}
}

func TestSameIntegrationPlansIgnoresProviderRevision(t *testing.T) {
	reviewed := []IntegrationPlan{{Key: "git", ProviderName: "github", ProviderID: 5, ProviderRevID: 900, Kind: "git"}}
	current := []IntegrationPlan{{Key: "git", ProviderName: "github", ProviderID: 5, ProviderRevID: 902, Kind: "git"}}
	if !sameIntegrationPlans(reviewed, current) {
		t.Fatal("a newer provider revision was treated as a changed integration mapping")
	}
	current[0].ProviderID = 6
	if sameIntegrationPlans(reviewed, current) {
		t.Fatal("another provider was treated as the same integration mapping")
	}
}
