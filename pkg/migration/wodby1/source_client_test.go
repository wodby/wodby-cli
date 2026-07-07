package wodby1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSourceClientExportsApp(t *testing.T) {
	var gotPath string
	var gotQuery string
	var gotAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(Export{
			Schema: ExportSchema,
			App:    &App{UUID: "app-1", Name: "demo"},
		})
	}))
	defer server.Close()

	client, err := NewSourceClient(server.URL, "secret-token")
	if err != nil {
		t.Fatal(err)
	}

	export, err := client.ExportApp(context.Background(), "app-1", true)
	if err != nil {
		t.Fatal(err)
	}

	if gotPath != "/api/v4/migrations/apps/app-1/export" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotQuery != "include_secrets=true" {
		t.Fatalf("query = %q", gotQuery)
	}
	if gotAuth != "token secret-token" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if export.App == nil || export.App.Name != "demo" {
		t.Fatalf("export = %#v", export)
	}
}

func TestDecodeExportRejectsUnsupportedSchema(t *testing.T) {
	_, err := DecodeExport([]byte(`{"schema":"old","app":{"uuid":"app-1","name":"demo"}}`))
	if err == nil {
		t.Fatal("expected unsupported schema error")
	}
}
