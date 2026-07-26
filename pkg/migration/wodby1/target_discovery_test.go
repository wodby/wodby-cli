package wodby1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/wodby/wodby-cli/pkg/types"
)

func TestTargetClientTypedDiscoveryOperations(t *testing.T) {
	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()

		if r.Method != http.MethodGet {
			http.Error(w, "read-only discovery requires GET", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("X-API-KEY") != "target-key" {
			http.Error(w, "missing target key", http.StatusUnauthorized)
			return
		}

		var response any
		switch r.URL.Path {
		case "/v1/orgs":
			response = []TargetOrg{
				{ID: 9, Name: "zulu"},
				{ID: 7, Name: "alpha"},
			}
		case "/v1/orgs/7":
			response = TargetOrg{ID: 7, Name: "alpha"}
		case "/v1/projects":
			response = []TargetProject{
				{ID: 12, Name: "zulu", OrgID: 7},
				{ID: 11, Name: "alpha", OrgID: 7},
			}
		case "/v1/projects/11":
			response = TargetProject{ID: 11, Name: "alpha", OrgID: 7}
		case "/v1/clusters":
			response = []TargetCluster{
				{ID: 14, Name: "zulu", OrgID: 7},
				{ID: 13, Name: "alpha", OrgID: 7},
			}
		case "/v1/clusters/13":
			response = TargetCluster{ID: 13, Name: "alpha", OrgID: 7}
		case "/v1/envs":
			response = []TargetEnv{
				{ID: 22, Name: "zulu", OrgID: 7},
				{ID: 21, Name: "alpha", OrgID: 7},
			}
		case "/v1/envs/21":
			response = TargetEnv{ID: 21, Name: "alpha", OrgID: 7}
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client, err := NewTargetClient(types.APIConfig{
		Endpoint: server.URL + "/v1",
		Key:      "target-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	orgs, err := client.ListOrgs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	org, err := client.GetOrg(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	projects, err := client.ListProjects(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	project, err := client.GetProject(ctx, 11)
	if err != nil {
		t.Fatal(err)
	}
	clusters, err := client.ListClusters(ctx, 7, []int{12, 11, 12})
	if err != nil {
		t.Fatal(err)
	}
	cluster, err := client.GetCluster(ctx, 13)
	if err != nil {
		t.Fatal(err)
	}
	envs, err := client.ListEnvs(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	env, err := client.GetEnv(ctx, 21)
	if err != nil {
		t.Fatal(err)
	}

	if got := []int{orgs[0].ID, orgs[1].ID}; !reflect.DeepEqual(got, []int{7, 9}) {
		t.Fatalf("org order = %v", got)
	}
	if got := []int{projects[0].ID, projects[1].ID}; !reflect.DeepEqual(got, []int{11, 12}) {
		t.Fatalf("project order = %v", got)
	}
	if got := []int{clusters[0].ID, clusters[1].ID}; !reflect.DeepEqual(got, []int{13, 14}) {
		t.Fatalf("cluster order = %v", got)
	}
	if got := []int{envs[0].ID, envs[1].ID}; !reflect.DeepEqual(got, []int{21, 22}) {
		t.Fatalf("environment order = %v", got)
	}
	if org.ID != 7 || project.ID != 11 || cluster.ID != 13 || env.ID != 21 {
		t.Fatalf("get results: org=%#v project=%#v cluster=%#v env=%#v", org, project, cluster, env)
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		"GET /v1/orgs",
		"GET /v1/orgs/7",
		"GET /v1/projects?orgId=7",
		"GET /v1/projects/11",
		"GET /v1/clusters?orgId=7&projectIds=11%2C12",
		"GET /v1/clusters/13",
		"GET /v1/envs?orgId=7",
		"GET /v1/envs/21",
	}
	if !reflect.DeepEqual(gotRequests, wantRequests) {
		t.Fatalf("requests:\n got: %#v\nwant: %#v", gotRequests, wantRequests)
	}
}

func TestTargetClientDiscoverTargetByExactNames(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	var clusterQueries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		methods = append(methods, r.Method)
		if r.URL.Path == "/v1/clusters" {
			clusterQueries = append(clusterQueries, r.URL.RawQuery)
		}
		mu.Unlock()

		if r.Method != http.MethodGet {
			http.Error(w, "unexpected mutation", http.StatusMethodNotAllowed)
			return
		}

		var response any
		switch r.URL.Path {
		case "/v1/user":
			response = TargetCurrentUser{ID: 1, Email: "admin@example.com"}
		case "/v1/orgs":
			response = []TargetOrg{{ID: 7, Name: "acme", Title: "Acme"}}
		case "/v1/org-memberships":
			if r.URL.Query().Get("orgId") != "7" {
				http.Error(w, "missing membership org scope", http.StatusBadRequest)
				return
			}
			response = targetAdminMemberships(1, 7)
		case "/v1/projects":
			if r.URL.Query().Get("orgId") != "7" {
				http.Error(w, "missing project org scope", http.StatusBadRequest)
				return
			}
			response = []TargetProject{{ID: 11, Name: "platform", Title: "Platform", OrgID: 7}}
		case "/v1/clusters":
			cluster := TargetCluster{
				ID:     13,
				Name:   "production",
				Title:  "Production",
				Status: "active",
				OrgID:  7,
				Capabilities: TargetClusterCapabilities{
					EnvoyGateway:   true,
					RedirectRoutes: true,
				},
			}
			if r.URL.Query().Get("orgId") != "7" {
				http.Error(w, "missing cluster org scope", http.StatusBadRequest)
				return
			}
			if projectIDs := r.URL.Query().Get("projectIds"); projectIDs != "" && projectIDs != "11" {
				http.Error(w, "wrong cluster project scope", http.StatusBadRequest)
				return
			}
			response = []TargetCluster{cluster}
		case "/v1/envs":
			if r.URL.Query().Get("orgId") != "7" {
				http.Error(w, "missing environment org scope", http.StatusBadRequest)
				return
			}
			response = []TargetEnv{
				{ID: 22, Name: "staging", Type: "STAGING", OrgID: 7},
				{ID: 21, Name: "prod", Type: "PROD", OrgID: 7},
			}
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client, err := NewTargetClient(types.APIConfig{Endpoint: server.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.DiscoverTarget(context.Background(), TargetDiscoveryRequest{
		TargetScopeSelectors: TargetScopeSelectors{
			Org:     " acme ",
			Project: "platform",
			Cluster: "production",
		},
		Environments: []string{" staging ", "prod", "staging"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.User.ID != 1 || result.Membership.Role != "admin" || result.Membership.OrgID != 7 ||
		result.Org.ID != 7 || result.Project.ID != 11 || result.Cluster.ID != 13 {
		t.Fatalf("scope = %#v", result.TargetScopeDiscovery)
	}
	if !result.Cluster.Capabilities.EnvoyGateway || !result.Cluster.Capabilities.RedirectRoutes {
		t.Fatalf("cluster capabilities = %#v", result.Cluster.Capabilities)
	}
	if got := []string{result.Environments[0].Selector, result.Environments[1].Selector}; !reflect.DeepEqual(got, []string{"prod", "staging"}) {
		t.Fatalf("environment selector order = %#v", got)
	}
	if got := []int{result.Environments[0].Env.ID, result.Environments[1].Env.ID}; !reflect.DeepEqual(got, []int{21, 22}) {
		t.Fatalf("environment IDs = %#v", got)
	}

	mu.Lock()
	gotMethods := append([]string(nil), methods...)
	gotClusterQueries := append([]string(nil), clusterQueries...)
	mu.Unlock()
	for _, method := range gotMethods {
		if method != http.MethodGet {
			t.Fatalf("discovery used %s, methods = %v", method, gotMethods)
		}
	}
	sort.Strings(gotClusterQueries)
	if !reflect.DeepEqual(gotClusterQueries, []string{"orgId=7", "orgId=7&projectIds=11"}) {
		t.Fatalf("cluster queries = %#v", gotClusterQueries)
	}
}

func TestTargetClientDiscoverTargetByIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var response any
		switch r.URL.RequestURI() {
		case "/v1/orgs/7":
			response = TargetOrg{ID: 7, Name: "acme"}
		case "/v1/user":
			response = TargetCurrentUser{ID: 1}
		case "/v1/org-memberships?orgId=7":
			response = targetAdminMemberships(1, 7)
		case "/v1/projects/11":
			response = TargetProject{ID: 11, Name: "platform", OrgID: 7}
		case "/v1/clusters/13":
			response = TargetCluster{ID: 13, Name: "production", OrgID: 7}
		case "/v1/clusters?orgId=7&projectIds=11":
			response = []TargetCluster{{ID: 13, Name: "production", OrgID: 7}}
		case "/v1/envs/21":
			response = TargetEnv{ID: 21, Name: "prod", OrgID: 7}
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client, err := NewTargetClient(types.APIConfig{Endpoint: server.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.DiscoverTarget(context.Background(), TargetDiscoveryRequest{
		TargetScopeSelectors: TargetScopeSelectors{Org: "7", Project: "11", Cluster: "13"},
		Environments:         []string{"21"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Org.ID != 7 || result.Project.ID != 11 || result.Cluster.ID != 13 ||
		len(result.Environments) != 1 || result.Environments[0].Env.ID != 21 {
		t.Fatalf("result = %#v", result)
	}
}

func TestTargetDiscoveryRelationshipBlockers(t *testing.T) {
	t.Run("project organization", func(t *testing.T) {
		server := newTargetDiscoveryServer(t, func(r *http.Request) any {
			switch r.URL.RequestURI() {
			case "/v1/orgs/7":
				return TargetOrg{ID: 7, Name: "acme"}
			case "/v1/user":
				return TargetCurrentUser{ID: 1}
			case "/v1/org-memberships?orgId=7":
				return targetAdminMemberships(1, 7)
			case "/v1/projects/11":
				return TargetProject{ID: 11, Name: "other", OrgID: 8}
			default:
				return nil
			}
		})
		defer server.Close()

		client := mustTargetClient(t, server.URL)
		_, err := client.DiscoverTargetScope(context.Background(), TargetScopeSelectors{
			Org: "7", Project: "11", Cluster: "13",
		})
		assertTargetBlocker(t, err, TargetBlockerWrongOrg,
			`target project selector "11" belongs to organization ID 8, expected organization ID 7`)
	})

	t.Run("cluster organization", func(t *testing.T) {
		server := newTargetDiscoveryServer(t, func(r *http.Request) any {
			switch r.URL.RequestURI() {
			case "/v1/orgs/7":
				return TargetOrg{ID: 7, Name: "acme"}
			case "/v1/user":
				return TargetCurrentUser{ID: 1}
			case "/v1/org-memberships?orgId=7":
				return targetAdminMemberships(1, 7)
			case "/v1/projects/11":
				return TargetProject{ID: 11, Name: "platform", OrgID: 7}
			case "/v1/clusters/13":
				return TargetCluster{ID: 13, Name: "other", OrgID: 8}
			default:
				return nil
			}
		})
		defer server.Close()

		client := mustTargetClient(t, server.URL)
		_, err := client.DiscoverTargetScope(context.Background(), TargetScopeSelectors{
			Org: "7", Project: "11", Cluster: "13",
		})
		assertTargetBlocker(t, err, TargetBlockerWrongOrg,
			`target cluster selector "13" belongs to organization ID 8, expected organization ID 7`)
	})

	t.Run("cluster project", func(t *testing.T) {
		server := newTargetDiscoveryServer(t, func(r *http.Request) any {
			switch r.URL.RequestURI() {
			case "/v1/orgs/7":
				return TargetOrg{ID: 7, Name: "acme"}
			case "/v1/user":
				return TargetCurrentUser{ID: 1}
			case "/v1/org-memberships?orgId=7":
				return targetAdminMemberships(1, 7)
			case "/v1/projects/11":
				return TargetProject{ID: 11, Name: "platform", OrgID: 7}
			case "/v1/clusters/13":
				return TargetCluster{ID: 13, Name: "production", OrgID: 7}
			case "/v1/clusters?orgId=7&projectIds=11":
				return []TargetCluster{}
			default:
				return nil
			}
		})
		defer server.Close()

		client := mustTargetClient(t, server.URL)
		_, err := client.DiscoverTargetScope(context.Background(), TargetScopeSelectors{
			Org: "7", Project: "11", Cluster: "13",
		})
		assertTargetBlocker(t, err, TargetBlockerWrongProject,
			`target cluster selector "13" is not associated with project ID 11`)
	})

	t.Run("environment organization", func(t *testing.T) {
		server := newTargetDiscoveryServer(t, func(r *http.Request) any {
			if r.URL.RequestURI() == "/v1/envs/21" {
				return TargetEnv{ID: 21, Name: "prod", OrgID: 8}
			}
			return nil
		})
		defer server.Close()

		client := mustTargetClient(t, server.URL)
		_, err := client.ResolveTargetEnvs(context.Background(), 7, []string{"21"})
		assertTargetBlocker(t, err, TargetBlockerWrongOrg,
			`target environment selector "21" belongs to organization ID 8, expected organization ID 7`)
	})
}

func TestTargetDiscoveryStableSelectorBlockers(t *testing.T) {
	t.Run("required", func(t *testing.T) {
		client := &TargetClient{}
		_, err := client.DiscoverTargetScope(context.Background(), TargetScopeSelectors{})
		assertTargetBlocker(t, err, TargetBlockerSelectorRequired, "target organization selector is required")
	})

	t.Run("invalid numeric", func(t *testing.T) {
		client := &TargetClient{}
		_, err := client.ResolveTargetEnvs(context.Background(), 7, []string{"-1"})
		assertTargetBlocker(t, err, TargetBlockerSelectorInvalid,
			`target environment selector "-1" must be a positive ID or exact name`)
	})

	t.Run("not found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer server.Close()

		client := mustTargetClient(t, server.URL)
		_, err := client.ResolveTargetEnvs(context.Background(), 7, []string{"99"})
		assertTargetBlocker(t, err, TargetBlockerNotFound,
			`target environment selector "99" was not found in organization ID 7`)
	})

	t.Run("ambiguous exact name", func(t *testing.T) {
		server := newTargetDiscoveryServer(t, func(r *http.Request) any {
			if r.URL.RequestURI() == "/v1/envs?orgId=7" {
				return []TargetEnv{
					{ID: 21, Name: "prod", OrgID: 7},
					{ID: 22, Name: "prod", OrgID: 7},
				}
			}
			return nil
		})
		defer server.Close()

		client := mustTargetClient(t, server.URL)
		_, err := client.ResolveTargetEnvs(context.Background(), 7, []string{"prod"})
		assertTargetBlocker(t, err, TargetBlockerAmbiguous,
			`target environment selector "prod" matched multiple resources`)
	})
}

func newTargetDiscoveryServer(t *testing.T, response func(*http.Request) any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "unexpected mutation", http.StatusMethodNotAllowed)
			return
		}
		value := response(r)
		if value == nil {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(value)
	}))
}

func mustTargetClient(t *testing.T, serverURL string) *TargetClient {
	t.Helper()
	client, err := NewTargetClient(types.APIConfig{Endpoint: serverURL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func targetAdminMemberships(userID int, orgID int) []TargetOrgMembership {
	return []TargetOrgMembership{{
		ID:     1,
		UserID: &userID,
		OrgID:  orgID,
		Role:   "admin",
		Status: "ok",
	}}
}

func assertTargetBlocker(t *testing.T, err error, code string, message string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected target discovery blocker")
	}
	var blocker *TargetDiscoveryBlocker
	if !errors.As(err, &blocker) {
		t.Fatalf("error is not TargetDiscoveryBlocker: %T %v", err, err)
	}
	if blocker.Code != code {
		t.Fatalf("blocker code = %q, want %q", blocker.Code, code)
	}
	if err.Error() != message {
		t.Fatalf("error = %q, want %q", err.Error(), message)
	}
}
