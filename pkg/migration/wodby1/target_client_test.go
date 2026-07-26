package wodby1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wodby/wodby-cli/pkg/types"
)

func TestTargetClientRequireOrgOwnerOrAdmin(t *testing.T) {
	var gotAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("X-API-KEY")
		switch r.URL.Path {
		case "/v1/user":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      42,
				"email":   "admin@example.com",
				"name":    "Admin",
				"isAdmin": false,
			})
		case "/v1/org-memberships":
			if r.URL.Query().Get("orgId") != "8" {
				http.Error(w, "missing organization scope", http.StatusBadRequest)
				return
			}
			userID := 42
			_ = json.NewEncoder(w).Encode([]TargetOrgMembership{{
				ID:     9,
				UserID: &userID,
				OrgID:  8,
				Role:   " ADMIN ",
				Status: " OK ",
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewTargetClient(types.APIConfig{
		Endpoint: server.URL + "/v1",
		Key:      "target-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	user, membership, err := client.RequireOrgOwnerOrAdmin(context.Background(), 8)
	if err != nil {
		t.Fatal(err)
	}
	if gotAPIKey != "target-key" {
		t.Fatalf("X-API-KEY = %q", gotAPIKey)
	}
	if user.ID != 42 {
		t.Fatalf("user = %#v", user)
	}
	if membership.ID != 9 || membership.UserID == nil || *membership.UserID != 42 ||
		membership.OrgID != 8 || membership.Role != "admin" || membership.Status != "ok" {
		t.Fatalf("membership = %#v", membership)
	}
}

func TestTargetClientRequireOrgOwnerOrAdminUsesSelectedOrgRole(t *testing.T) {
	tests := []struct {
		name          string
		role          string
		status        string
		platformAdmin bool
		wantGranted   bool
	}{
		{name: "owner", role: "owner", status: "ok", wantGranted: true},
		{name: "admin", role: "admin", status: "ok", wantGranted: true},
		{name: "platform admin with member role", role: "member", status: "ok", platformAdmin: true},
		{name: "support", role: "support", status: "ok", platformAdmin: true},
		{name: "robot", role: "robot", status: "ok", platformAdmin: true},
		{name: "inactive admin", role: "admin", status: "invited", platformAdmin: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := 7
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/user":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"id":      userID,
						"isAdmin": tt.platformAdmin,
					})
				case "/org-memberships":
					_ = json.NewEncoder(w).Encode([]TargetOrgMembership{{
						ID:     11,
						UserID: &userID,
						OrgID:  5,
						Role:   tt.role,
						Status: tt.status,
					}})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			client, err := NewTargetClient(types.APIConfig{Endpoint: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			_, membership, err := client.RequireOrgOwnerOrAdmin(context.Background(), 5)
			if tt.wantGranted {
				if err != nil {
					t.Fatal(err)
				}
				if membership.Role != tt.role {
					t.Fatalf("membership = %#v", membership)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "organization OWNER or ADMIN credentials are required") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestTargetClientRequireOrgOwnerOrAdminRejectsInvalidMembershipResponses(t *testing.T) {
	tests := []struct {
		name        string
		memberships func(int) []TargetOrgMembership
		errorText   string
	}{
		{
			name: "missing current user",
			memberships: func(_ int) []TargetOrgMembership {
				otherUserID := 8
				return []TargetOrgMembership{{ID: 11, UserID: &otherUserID, OrgID: 5, Role: "admin", Status: "ok"}}
			},
			errorText: "organization OWNER or ADMIN credentials are required",
		},
		{
			name: "wrong organization",
			memberships: func(userID int) []TargetOrgMembership {
				return []TargetOrgMembership{{ID: 11, UserID: &userID, OrgID: 6, Role: "admin", Status: "ok"}}
			},
			errorText: "organization ID 6, expected 5",
		},
		{
			name: "duplicate current user",
			memberships: func(userID int) []TargetOrgMembership {
				return []TargetOrgMembership{
					{ID: 11, UserID: &userID, OrgID: 5, Role: "admin", Status: "ok"},
					{ID: 12, UserID: &userID, OrgID: 5, Role: "owner", Status: "ok"},
				}
			},
			errorText: "multiple memberships",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := 7
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/user":
					_ = json.NewEncoder(w).Encode(TargetCurrentUser{ID: userID})
				case "/org-memberships":
					_ = json.NewEncoder(w).Encode(tt.memberships(userID))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			client, err := NewTargetClient(types.APIConfig{Endpoint: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = client.RequireOrgOwnerOrAdmin(context.Background(), 5)
			if err == nil || !strings.Contains(err.Error(), tt.errorText) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestTargetClientRequireOrgOwnerOrAdminRejectsInvalidOrganizationBeforeNetwork(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := NewTargetClient(types.APIConfig{Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = client.RequireOrgOwnerOrAdmin(context.Background(), 0)
	if err == nil || !strings.Contains(err.Error(), "organization ID must be positive") {
		t.Fatalf("err = %v", err)
	}
	if called {
		t.Fatal("invalid organization ID must be rejected before a network request")
	}
}

func TestNewTargetClientRequiresTLSOutsideLoopback(t *testing.T) {
	if _, err := NewTargetClient(types.APIConfig{Endpoint: "http://api.example.com/v1"}); err == nil ||
		!strings.Contains(err.Error(), "must use HTTPS") {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewTargetClient(types.APIConfig{Endpoint: "https://user@example.com/v1"}); err == nil ||
		!strings.Contains(err.Error(), "must not contain user credentials") {
		t.Fatalf("err = %v", err)
	}
}
