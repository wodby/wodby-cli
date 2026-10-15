package migrate

import (
	"reflect"
	"testing"

	"github.com/wodby/wodby-cli/pkg/migration/wodby1"
)

func TestWorkspaceFlagsMatchCommandScope(t *testing.T) {
	instance := newWodby1InstanceCommand()
	if instance.Flags().Lookup("workspace") == nil || instance.Flags().Lookup("workspace-instance") != nil {
		t.Fatal("instance migration must expose only --workspace")
	}
	for _, cmd := range []string{"app", "server"} {
		command := newWodby1AppCommand()
		if cmd == "server" {
			command = newWodby1ServerCommand()
		}
		if command.Flags().Lookup("workspace") != nil || command.Flags().Lookup("workspace-instance") == nil {
			t.Fatalf("%s migration must expose only --workspace-instance", cmd)
		}
	}
}

func TestResolveWorkspaceInstances(t *testing.T) {
	export := wodby1.Export{
		Schema: wodby1.ExportSchemaV2,
		Source: &wodby1.ExportSource{Kind: "app", UUID: "app-1"},
		Apps: []wodby1.AppExport{{
			App:       wodby1.App{UUID: "app-1", Name: "shop"},
			Instances: []wodby1.Instance{{UUID: "inst-1", Name: "prod"}, {UUID: "inst-2", Name: "dev"}},
		}},
	}
	for _, tc := range []struct {
		name    string
		kind    string
		opts    options
		want    map[string]bool
		wantErr bool
	}{
		{name: "none selected", kind: "app", want: map[string]bool{}},
		{name: "by name", kind: "app", opts: options{workspaceInstances: []string{"dev"}}, want: map[string]bool{"inst-2": true}},
		{name: "by UUID and scoped name", kind: "server", opts: options{workspaceInstances: []string{"inst-1", "shop/dev"}}, want: map[string]bool{"inst-1": true, "inst-2": true}},
		{name: "server needs the app", kind: "server", opts: options{workspaceInstances: []string{"dev"}}, wantErr: true},
		{name: "unknown instance", kind: "app", opts: options{workspaceInstances: []string{"stage"}}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveWorkspaceInstances(export, tc.kind, &tc.opts)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v", err)
			}
			if !tc.wantErr && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("selected = %#v, want %#v", got, tc.want)
			}
		})
	}

	// An instance migration exports exactly its one instance.
	export.Apps[0].Instances = export.Apps[0].Instances[1:]
	got, err := resolveWorkspaceInstances(export, "instance", &options{workspace: true})
	if err != nil || !reflect.DeepEqual(got, map[string]bool{"inst-2": true}) {
		t.Fatalf("selected = %#v, error = %v", got, err)
	}
}
