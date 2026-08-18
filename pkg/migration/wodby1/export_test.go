package wodby1

import "testing"

func TestInstanceExportAcceptsSiblingContext(t *testing.T) {
	export := Export{
		Schema:          ExportSchemaV2,
		GeneratedAt:     100,
		SecretsIncluded: true,
		Source:          &ExportSource{Kind: "instance", UUID: "inst-1"},
		Apps: []AppExport{{
			App:              App{UUID: "app-1", Name: "demo"},
			Instances:        []Instance{exportContextTestInstance("inst-1", "prod", "prod")},
			ContextInstances: []Instance{exportContextTestInstance("inst-2", "dev", "dev")},
		}},
	}
	if err := export.Validate(); err != nil {
		t.Fatalf("an instance export may carry the app's other instances: %v", err)
	}

	// The requested instance must not also appear as context, or the split
	// would compare it against itself and count it twice.
	repeated := export
	repeated.Apps = []AppExport{{
		App:              export.Apps[0].App,
		Instances:        export.Apps[0].Instances,
		ContextInstances: []Instance{exportContextTestInstance("inst-1", "prod", "prod")},
	}}
	if err := repeated.Validate(); err == nil {
		t.Fatal("the source instance must not be repeated as context")
	}

	// An app export already contains every instance.
	appScoped := export
	appScoped.Source = &ExportSource{Kind: "app", UUID: "app-1"}
	if err := appScoped.Validate(); err == nil {
		t.Fatal("an app export must not carry context instances")
	}
}

func exportContextTestInstance(uuid, name, instanceType string) Instance {
	return Instance{
		UUID: uuid, Name: name, Type: instanceType, Status: "ok",
		Stack: Stack{UUID: "stack-1", Name: "drupal11", Version: "11"},
	}
}
