package wodby1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTargetAppStackIDsAreUniqueAndSorted(t *testing.T) {
	got := targetAppStackIDs([]TargetAppInstance{
		{StackID: 9}, {StackID: 3}, {StackID: 9}, {StackID: 0},
	})
	if len(got) != 2 || got[0] != 3 || got[1] != 9 {
		t.Fatalf("stack IDs = %#v", got)
	}
}

func TestListInventoryGroupsInstancesByApp(t *testing.T) {
	token := strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != token {
			t.Fatalf("token header = %q", r.Header.Get("X-API-Key"))
		}
		org := r.URL.Query().Get("org_id")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v3/orgs":
			_, _ = w.Write([]byte(`[{"id":"org-1","name":"acme","title":"Acme"}]`))
		case "/api/v3/apps":
			if org != "org-1" {
				t.Fatalf("apps org_id = %q", org)
			}
			_, _ = w.Write([]byte(`[{"id":"app-2","name":"shop","title":"Shop","status":"ok","org_id":"org-1"},{"id":"app-1","name":"blog","title":"Blog","status":1,"org_id":"org-1"}]`))
		case "/api/v3/instances":
			_, _ = w.Write([]byte(`[{"id":"i-2","name":"prod","title":"Prod","type":"prod","status":"ok","app_id":"app-1","server_id":"s-1"},{"id":"i-1","name":"dev","title":"Dev","type":"dev","status":"ok","app_id":"app-1","server_id":"s-1"}]`))
		case "/api/v3/servers":
			_, _ = w.Write([]byte(`[{"id":"s-1","title":"Main","status":"ok","ip_address":"192.0.2.1"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewSourceClient(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := client.ListInventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Orgs) != 1 || len(inventory.Orgs[0].Apps) != 2 || len(inventory.Orgs[0].Servers) != 1 {
		t.Fatalf("inventory = %+v", inventory)
	}
	blog, shop := inventory.Orgs[0].Apps[0], inventory.Orgs[0].Apps[1]
	if blog.Name != "blog" || blog.Status != "1" || len(blog.Instances) != 2 || blog.Instances[0].Name != "dev" ||
		blog.Instances[1].ServerID != "s-1" || shop.Name != "shop" || len(shop.Instances) != 0 {
		t.Fatalf("apps = %+v", inventory.Orgs[0].Apps)
	}
}

func TestListInventoryReportsWodby1Errors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Access denied"}`))
	}))
	defer server.Close()
	client, err := NewSourceClient(server.URL, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListInventory(context.Background()); err == nil {
		t.Fatal("a refused list was not reported")
	}
}

// A token that sees many organizations can list them alone, with one request,
// or one organization with its apps, without reading all the others.
func TestListInventoryFilteredSelectsOrganizations(t *testing.T) {
	token := strings.Repeat("a", 64)
	requests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests[r.URL.Path+"?"+r.URL.Query().Get("org_id")]++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v3/orgs":
			_, _ = w.Write([]byte(`[{"id":"org-1","name":"nsf","title":"NSF"},{"id":"org-2","name":"nsfw-tools","title":"Tools"},{"id":"org-3","name":"acme","title":"Acme"}]`))
		case "/api/v3/apps", "/api/v3/instances", "/api/v3/servers":
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewSourceClient(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}

	orgs, err := client.ListInventoryFiltered(context.Background(), SourceInventoryFilter{OrgsOnly: true})
	if err != nil || len(orgs.Orgs) != 3 || len(requests) != 1 {
		t.Fatalf("orgs = %+v, requests = %v, err = %v", orgs, requests, err)
	}

	// The exact name wins over a name that contains it.
	inventory, err := client.ListInventoryFiltered(context.Background(), SourceInventoryFilter{Org: "NSF"})
	if err != nil || len(inventory.Orgs) != 1 || inventory.Orgs[0].ID != "org-1" {
		t.Fatalf("inventory = %+v, err = %v", inventory, err)
	}
	if requests["/api/v3/apps?org-1"] != 1 || requests["/api/v3/apps?org-2"] != 0 || requests["/api/v3/apps?org-3"] != 0 {
		t.Fatalf("requests = %v", requests)
	}
	partial, err := client.ListInventoryFiltered(context.Background(), SourceInventoryFilter{Org: "tool", OrgsOnly: true})
	if err != nil || len(partial.Orgs) != 1 || partial.Orgs[0].ID != "org-2" {
		t.Fatalf("partial = %+v, err = %v", partial, err)
	}
	none, err := client.ListInventoryFiltered(context.Background(), SourceInventoryFilter{Org: "missing"})
	if err != nil || len(none.Orgs) != 0 {
		t.Fatalf("none = %+v, err = %v", none, err)
	}
}
