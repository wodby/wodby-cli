package wodby1

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReviewedPlanRequiresSecureExactJSONAndValidHash(t *testing.T) {
	valid := reviewedPlanFixture(t)
	tests := []struct {
		name string
		edit func(*testing.T, string)
		want error
	}{
		{
			name: "insecure permissions",
			edit: func(t *testing.T, path string) {
				t.Helper()
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			},
			want: ErrMigrationPlanInsecure,
		},
		{
			name: "unknown field",
			edit: func(t *testing.T, path string) {
				t.Helper()
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				data = []byte(strings.TrimSuffix(strings.TrimSpace(string(data)), "}") + `,"unknown":true}`)
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			},
			want: ErrMigrationPlanInvalid,
		},
		{
			name: "trailing JSON",
			edit: func(t *testing.T, path string) {
				t.Helper()
				file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := file.WriteString("\n{}\n"); err != nil {
					_ = file.Close()
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			},
			want: ErrMigrationPlanInvalid,
		},
		{
			name: "tampered contents",
			edit: func(t *testing.T, path string) {
				t.Helper()
				plan, err := cloneMigrationPlan(valid)
				if err != nil {
					t.Fatal(err)
				}
				plan.Apps[0].Title = "Tampered"
				writeReviewedPlanFixture(t, path, plan)
			},
			want: ErrMigrationPlanInvalid,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "reviewed.json")
			writeReviewedPlanFixture(t, path, valid)
			test.edit(t, path)
			if _, err := LoadReviewedPlan(path); !errors.Is(err, test.want) {
				t.Fatalf("LoadReviewedPlan() error = %v, want %v", err, test.want)
			}
		})
	}

	t.Run("symlink", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "target.json")
		link := filepath.Join(dir, "reviewed.json")
		writeReviewedPlanFixture(t, target, valid)
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadReviewedPlan(link); !errors.Is(err, ErrMigrationPlanInvalid) {
			t.Fatalf("LoadReviewedPlan() error = %v", err)
		}
	})

	t.Run("valid", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "reviewed.json")
		writeReviewedPlanFixture(t, path, valid)
		loaded, err := LoadReviewedPlan(path)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.PlanHash != valid.PlanHash {
			t.Fatalf("loaded hash = %q, want %q", loaded.PlanHash, valid.PlanHash)
		}
	})
}

func TestPinReviewedTargetsCopiesOnlyResolvedImmutableIdentity(t *testing.T) {
	reviewed := reviewedPlanFixture(t)
	current, err := cloneMigrationPlan(reviewed)
	if err != nil {
		t.Fatal(err)
	}
	current.PlanHash = ""
	current.Apps[0].Repository.TargetService = ""
	current.Apps[0].Repository.RemoteGitRepoID = ""
	instance := &current.Apps[0].Instances[0]
	instance.Stack.Target = "drupal11"
	instance.Stack.TargetID = 0
	instance.Stack.TargetRevID = 0
	instance.Stack.TargetVersion = ""
	instance.BuildServiceID = 0
	instance.BuildServiceRevID = 0
	instance.Services[0].TargetID = 0
	instance.Services[0].TargetServiceRevID = 0
	instance.Imports[0].SourceUUID = "fresh-backup-file"
	instance.Imports[0].BackupUUID = "fresh-backup"
	instance.Imports[0].BackupCreated = 999
	instance.Imports[0].Size = 12345
	instance.Imports[0].TargetService = ""
	instance.Imports[0].TargetImport = ""
	instance.Imports[0].TargetServiceID = 0
	instance.Imports[0].TargetServiceRevID = 0

	if err := PinReviewedTargets(&current, reviewed); err != nil {
		t.Fatal(err)
	}
	pinned := current.Apps[0].Instances[0]
	if pinned.Stack.TargetID != 7 || pinned.Stack.TargetRevID != 71 ||
		pinned.Services[0].TargetID != 11 ||
		pinned.Services[0].TargetServiceRevID != 101 ||
		pinned.Imports[0].TargetServiceID != 12 ||
		pinned.Imports[0].TargetServiceRevID != 102 ||
		pinned.BuildServiceID != 11 ||
		pinned.BuildServiceRevID != 101 {
		t.Fatalf("pinned plan = %#v", current)
	}
	if current.Apps[0].Repository.RemoteGitRepoID != "remote-1" {
		t.Fatalf("pinned repository = %#v", current.Apps[0].Repository)
	}
	if pinned.Imports[0].SourceUUID != "fresh-backup-file" ||
		pinned.Imports[0].BackupCreated != 999 ||
		pinned.Imports[0].Size != 12345 {
		t.Fatalf("fresh source backup metadata was overwritten: %#v", pinned.Imports[0])
	}
}

func TestPinReviewedTargetsRejectsChangedOptionsWithoutMutatingCurrent(t *testing.T) {
	reviewed := reviewedPlanFixture(t)
	current, err := cloneMigrationPlan(reviewed)
	if err != nil {
		t.Fatal(err)
	}
	current.PlanHash = ""
	current.Apps[0].Instances[0].Services[0].TargetName = "nginx"
	before, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}

	err = PinReviewedTargets(&current, reviewed)
	if err == nil || !strings.Contains(err.Error(), "options no longer match") {
		t.Fatalf("PinReviewedTargets() error = %v", err)
	}
	after, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("failed pinning partially mutated the current plan")
	}
}

func TestValidateReviewedAcceptsExactlyOneInstanceSource(t *testing.T) {
	plan := reviewedPlanFixture(t)
	plan.Source.Kind = "instance"
	plan.Source.ID = "instance-1"
	digest, err := plan.contentDigest()
	if err != nil {
		t.Fatal(err)
	}
	plan.PlanHash = digest
	if err := plan.ValidateReviewed(); err != nil {
		t.Fatal(err)
	}

	plan.Apps[0].Instances[0].SourceUUID = "instance-2"
	digest, err = plan.contentDigest()
	if err != nil {
		t.Fatal(err)
	}
	plan.PlanHash = digest
	if err := plan.ValidateReviewed(); err == nil || !strings.Contains(err.Error(), "approved source instance") {
		t.Fatalf("ValidateReviewed() error = %v", err)
	}
}

func reviewedPlanFixture(t *testing.T) Plan {
	t.Helper()
	plan := Plan{
		Schema: MigrationPlanSchema,
		Source: PlanSource{
			Kind:         "app",
			ID:           "app-1",
			Schema:       ExportSchemaV2,
			ExportDigest: strings.Repeat("a", 64),
			ConfigDigest: strings.Repeat("b", 64),
			BackupDigest: strings.Repeat("c", 64),
		},
		Target: PlanTarget{
			OrgID: 8, ProjectID: 9, ClusterID: 10,
			OrgOwnerOrAdminVerified: true, DiscoveryVerified: true,
		},
		Apps: []AppPlan{{
			SourceUUID: "app-1",
			Name:       "example",
			Title:      "Example",
			Type:       "app",
			Repository: &RepositoryPlan{
				SourceUUID: "repo-1", Action: "connect",
				TargetService: "php", RepositoryName: "acme/example",
				GitIntegrationID: 44, RemoteGitRepoID: "remote-1",
			},
			Instances: []InstancePlan{{
				SourceUUID: "instance-1", Name: "prod", Title: "Production",
				SourceType: "prod", TargetEnv: "prod", TargetEnvID: 21,
				BuildServiceID: 11, BuildServiceRevID: 101,
				Stack: StackPlan{
					Name: "drupal11", Target: "acme/drupal11",
					TargetID: 7, TargetRevID: 71, TargetVersion: "revision-4",
				},
				Services: []ServicePlan{{
					SourceName: "php", TargetName: "php", Enabled: true, Action: "migrate",
					TargetID: 11, TargetServiceRevID: 101,
				}},
				Imports: []ImportPlan{{
					SourceUUID: "backup-file", BackupUUID: "backup", Component: "db",
					Action: "import", TargetService: "mariadb", TargetImport: "database",
					TargetServiceID: 12, TargetServiceRevID: 102,
				}},
			}},
		}},
		Review: []ReviewItem{},
		Status: "ready",
	}
	digest, err := plan.contentDigest()
	if err != nil {
		t.Fatal(err)
	}
	plan.PlanHash = digest
	return plan
}

func writeReviewedPlanFixture(t *testing.T, path string, plan Plan) {
	t.Helper()
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
}
