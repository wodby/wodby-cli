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

func TestTargetClientRequireAdmin(t *testing.T) {
	var gotAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("X-API-KEY")
		if r.URL.Path != "/v1/user" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(TargetCurrentUser{
			ID:      42,
			Email:   "admin@example.com",
			Name:    "Admin",
			IsAdmin: true,
		})
	}))
	defer server.Close()

	client, err := NewTargetClient(types.APIConfig{
		Endpoint: server.URL + "/v1",
		Key:      "target-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	user, err := client.RequireAdmin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotAPIKey != "target-key" {
		t.Fatalf("X-API-KEY = %q", gotAPIKey)
	}
	if user.ID != 42 || !user.IsAdmin {
		t.Fatalf("user = %#v", user)
	}
}

func TestTargetClientRequireAdminRejectsNonAdmin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(TargetCurrentUser{
			ID:      7,
			Email:   "member@example.com",
			IsAdmin: false,
		})
	}))
	defer server.Close()

	client, err := NewTargetClient(types.APIConfig{Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.RequireAdmin(context.Background())
	if err == nil || !strings.Contains(err.Error(), "administrator credentials are required") {
		t.Fatalf("err = %v", err)
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
