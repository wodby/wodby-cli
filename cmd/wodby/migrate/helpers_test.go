package migrate

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wodby/wodby-cli/pkg/migration/wodby1"
)

// These helpers lay out migration files the way the library keeps them, so
// command tests can seed and inspect them.

func artifactPaths(sourceKind, sourceID, stateFile string) (string, string, error) {
	artifacts, err := wodby1.NewFileArtifacts(sourceKind, sourceID, stateFile)
	if err != nil {
		return "", "", err
	}
	return artifacts.PlanLocation(), artifacts.StateLocation(), nil
}

func ensureArtifactDirectories(planPath, statePath string) error {
	for _, dir := range []string{filepath.Join(os.TempDir(), "wodby-migrations"), filepath.Dir(planPath), filepath.Dir(statePath)} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		if err := os.Chmod(dir, 0700); err != nil {
			return err
		}
	}
	return nil
}

func explicitStatePlanPath(statePath string) string {
	ext := filepath.Ext(statePath)
	if ext == "" {
		return statePath + ".plan.json"
	}
	return strings.TrimSuffix(statePath, ext) + ".plan" + ext
}

func serverAppStatePath(basePath string, sourceAppUUID string) string {
	digest := sha256.Sum256([]byte(sourceAppUUID))
	suffix := fmt.Sprintf(".app-%x", digest[:8])
	ext := filepath.Ext(basePath)
	if ext == "" {
		return basePath + suffix
	}
	return strings.TrimSuffix(basePath, ext) + suffix + ext
}

func writePlanFile(path string, plan wodby1.Plan) error {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}

func migrationStateIdentity(plan wodby1.Plan) wodby1.MigrationStateIdentity {
	return wodby1.MigrationStateIdentity{
		Source: wodby1.MigrationStateSourceIdentity{
			Kind:         plan.Source.Kind,
			ID:           plan.Source.ID,
			ConfigDigest: plan.Source.ConfigDigest,
		},
		PlanHash: plan.PlanHash,
		Target: wodby1.MigrationStateTarget{
			OrgID:       plan.Target.OrgID,
			ProjectID:   plan.Target.ProjectID,
			ClusterID:   plan.Target.ClusterID,
			AppID:       plan.Target.AppID,
			ExistingApp: plan.Target.AppID > 0,
		},
	}
}

func pausedTestError() *wodby1.MigrationPausedError {
	return &wodby1.MigrationPausedError{Actions: []*wodby1.ExternalActionRequiredError{{
		Instance:         "prod",
		TargetInstanceID: 4100,
		ServiceName:      "php",
		TargetServiceID:  4200,
		ProviderLabel:    "GitHub Actions",
		ExampleURL:       "https://github.com/wodby/wodby-ci/blob/2.0/drupal/github-actions/wodby.yml",
	}}}
}

func TestWodby1RepositorySelectionUsesNamesInsteadOfProviderIDs(t *testing.T) {
	cmd := newWodby1InstanceCommand()
	if cmd.Flags().Lookup("target-git-integration-id") == nil {
		t.Fatal("--target-git-integration-id flag is missing")
	}
	if cmd.Flags().Lookup("target-repository-name") == nil {
		t.Fatal("--target-repository-name flag is missing")
	}
	if cmd.Flags().Lookup("target-remote-git-repo-id") != nil {
		t.Fatal("provider repository ID must not be a customer-facing migration flag")
	}
}
