package wodby1

import "testing"

func TestResolveSourceBackups(t *testing.T) {
	export := Export{Apps: []AppExport{
		{App: App{UUID: "app-1", Name: "alpha"}, Instances: []Instance{{UUID: "instance-1", Name: "prod"}}},
		{App: App{UUID: "app-2", Name: "beta"}, Instances: []Instance{{UUID: "instance-2", Name: "dev"}}},
	}}

	selected, err := ResolveSourceBackups(export, "server", []string{
		"alpha/prod=backup-1",
		"instance-2=backup-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if selected["instance-1"] != "backup-1" || selected["instance-2"] != "backup-2" {
		t.Fatalf("selection = %#v", selected)
	}
}

func TestResolveSourceBackupsAcceptsBareUUIDForInstance(t *testing.T) {
	export := Export{Apps: []AppExport{{
		App:       App{UUID: "app-1", Name: "alpha"},
		Instances: []Instance{{UUID: "instance-1", Name: "prod"}},
	}}}
	selected, err := ResolveSourceBackups(export, "instance", []string{"backup-1"})
	if err != nil {
		t.Fatal(err)
	}
	if selected["instance-1"] != "backup-1" {
		t.Fatalf("selection = %#v", selected)
	}
}

func TestPlanSourceBackupsRejectsMixedSnapshots(t *testing.T) {
	_, err := PlanSourceBackups(Plan{Apps: []AppPlan{{Instances: []InstancePlan{{
		SourceUUID: "instance-1",
		Imports: []ImportPlan{
			{Action: "import", BackupUUID: "backup-1"},
			{Action: "import", BackupUUID: "backup-2"},
		},
	}}}}})
	if err == nil {
		t.Fatal("expected mixed backup snapshots to be rejected")
	}
}
