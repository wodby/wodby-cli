package ci

import (
	"os"
	"testing"

	"github.com/wodby/wodby-cli/pkg/types"
)

func TestCollectBuildInfoFromGitLabCI(t *testing.T) {
	t.Setenv("GITLAB_CI", "true")
	t.Setenv("CI_PIPELINE_ID", "9001")
	t.Setenv("CI_PIPELINE_IID", "42")
	t.Setenv("CI_JOB_ID", "314")
	t.Setenv("CI_COMMIT_SHA", "abc123")
	t.Setenv("CI_COMMIT_BRANCH", "main")
	t.Setenv("CI_COMMIT_MESSAGE", "release: ship it")
	t.Setenv("CI_COMMIT_AUTHOR", "Jane Doe <jane@example.com>")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current directory: %v", err)
	}

	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}

	t.Cleanup(func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatalf("failed to restore directory: %v", err)
		}
	})

	buildInput, err := CollectBuildInfo()
	if err != nil {
		t.Fatalf("CollectBuildInfo returned error: %v", err)
	}

	if buildInput.Provider != "gitlab" {
		t.Fatalf("expected provider gitlab, got %q", buildInput.Provider)
	}
	if buildInput.Workflow != "9001" {
		t.Fatalf("expected workflow 9001, got %q", buildInput.Workflow)
	}
	if buildInput.BuildID != "314" {
		t.Fatalf("expected build ID 314, got %q", buildInput.BuildID)
	}
	if buildInput.BuildNum != 42 {
		t.Fatalf("expected build number 42, got %d", buildInput.BuildNum)
	}
	if buildInput.GitCommitSHA != "abc123" {
		t.Fatalf("expected commit SHA abc123, got %q", buildInput.GitCommitSHA)
	}
	if buildInput.GitRefType != string(types.GitRefTypeBranch) {
		t.Fatalf("expected branch ref type, got %q", buildInput.GitRefType)
	}
	if buildInput.GitRef != "main" {
		t.Fatalf("expected branch main, got %q", buildInput.GitRef)
	}
	if buildInput.GitCommitMessage == nil || *buildInput.GitCommitMessage != "release: ship it" {
		t.Fatalf("expected commit message from GitLab CI, got %#v", buildInput.GitCommitMessage)
	}
	if buildInput.GitCommitAuthorName == nil || *buildInput.GitCommitAuthorName != "Jane Doe" {
		t.Fatalf("expected commit author name Jane Doe, got %#v", buildInput.GitCommitAuthorName)
	}
	if buildInput.GitCommitAuthorEmail == nil || *buildInput.GitCommitAuthorEmail != "jane@example.com" {
		t.Fatalf("expected commit author email jane@example.com, got %#v", buildInput.GitCommitAuthorEmail)
	}
}

func TestCollectGitHubActionsBuildInfoUsesHeadRefForPullRequests(t *testing.T) {
	t.Setenv("GITHUB_RUN_ID", "1001")
	t.Setenv("GITHUB_RUN_NUMBER", "17")
	t.Setenv("GITHUB_RUN_ATTEMPT", "2")
	t.Setenv("GITHUB_JOB", "build")
	t.Setenv("GITHUB_SHA", "deadbeef")
	t.Setenv("GITHUB_REF_TYPE", "branch")
	t.Setenv("GITHUB_REF_NAME", "123/merge")
	t.Setenv("GITHUB_HEAD_REF", "feature/login")

	buildInput, err := collectGitHubActionsBuildInfo()
	if err != nil {
		t.Fatalf("collectGitHubActionsBuildInfo returned error: %v", err)
	}

	if buildInput.Provider != "githubactions" {
		t.Fatalf("expected provider githubactions, got %q", buildInput.Provider)
	}
	if buildInput.Workflow != "1001" {
		t.Fatalf("expected workflow 1001, got %q", buildInput.Workflow)
	}
	if buildInput.BuildID != "1001:build:2" {
		t.Fatalf("expected build ID 1001:build:2, got %q", buildInput.BuildID)
	}
	if buildInput.BuildNum != 17 {
		t.Fatalf("expected build number 17, got %d", buildInput.BuildNum)
	}
	if buildInput.GitCommitSHA != "deadbeef" {
		t.Fatalf("expected commit SHA deadbeef, got %q", buildInput.GitCommitSHA)
	}
	if buildInput.GitRefType != string(types.GitRefTypeBranch) {
		t.Fatalf("expected branch ref type, got %q", buildInput.GitRefType)
	}
	if buildInput.GitRef != "feature/login" {
		t.Fatalf("expected head ref feature/login, got %q", buildInput.GitRef)
	}
}

func TestCollectGitHubActionsBuildInfoUsesTagRef(t *testing.T) {
	t.Setenv("GITHUB_RUN_ID", "2002")
	t.Setenv("GITHUB_RUN_NUMBER", "33")
	t.Setenv("GITHUB_JOB", "release")
	t.Setenv("GITHUB_SHA", "feedface")
	t.Setenv("GITHUB_REF_TYPE", "tag")
	t.Setenv("GITHUB_REF_NAME", "v1.2.3")

	buildInput, err := collectGitHubActionsBuildInfo()
	if err != nil {
		t.Fatalf("collectGitHubActionsBuildInfo returned error: %v", err)
	}

	if buildInput.BuildID != "2002:release" {
		t.Fatalf("expected build ID 2002:release, got %q", buildInput.BuildID)
	}
	if buildInput.GitRefType != string(types.GitRefTypeTag) {
		t.Fatalf("expected tag ref type, got %q", buildInput.GitRefType)
	}
	if buildInput.GitRef != "v1.2.3" {
		t.Fatalf("expected tag v1.2.3, got %q", buildInput.GitRef)
	}
}
