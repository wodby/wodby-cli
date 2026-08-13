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
	if selected["instance-1"][allBackupComponents] != "backup-1" || selected["instance-2"][allBackupComponents] != "backup-2" {
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
	if selected["instance-1"][allBackupComponents] != "backup-1" {
		t.Fatalf("selection = %#v", selected)
	}
}

func TestPlanSourceBackupsPinsMixedSnapshotsPerComponent(t *testing.T) {
	selected, err := PlanSourceBackups(Plan{Apps: []AppPlan{{Instances: []InstancePlan{{
		SourceUUID: "instance-1",
		Imports: []ImportPlan{
			{Action: "import", Component: "db", BackupUUID: "backup-1"},
			{Action: "import", Component: "files", BackupUUID: "backup-2"},
		},
	}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if selected["instance-1"]["db"] != "backup-1" || selected["instance-1"]["files"] != "backup-2" {
		t.Fatalf("selection = %#v", selected)
	}
}
