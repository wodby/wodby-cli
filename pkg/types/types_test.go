package types

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestNewBuildMetadataUsesGitHubRefNames(t *testing.T) {
	clearCIEnvironment(t)
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_RUN_ID", "1001")
	t.Setenv("GITHUB_RUN_NUMBER", "17")
	t.Setenv("GITHUB_SHA", gitOutput(t, "rev-parse", "HEAD"))
	t.Setenv("GITHUB_REF_TYPE", "branch")
	t.Setenv("GITHUB_REF", "refs/pull/42/merge")
	t.Setenv("GITHUB_REF_NAME", "42/merge")
	t.Setenv("GITHUB_HEAD_REF", "feature/login")
	t.Setenv("GITHUB_REPOSITORY", "wodby/wodby-cli")

	metadata, err := NewBuildMetadata("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Provider != GitHubActions {
		t.Fatalf("provider = %q, want %q", metadata.Provider, GitHubActions)
	}
	if metadata.Branch != "feature/login" {
		t.Fatalf("branch = %q, want feature/login", metadata.Branch)
	}
	if metadata.Number != "17" {
		t.Fatalf("number = %q, want 17", metadata.Number)
	}
}

func TestNewBuildMetadataUsesGitHubTagName(t *testing.T) {
	clearCIEnvironment(t)
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REF_TYPE", "tag")
	t.Setenv("GITHUB_REF", "refs/tags/v1.2.3")
	t.Setenv("GITHUB_REF_NAME", "v1.2.3")
	t.Setenv("GITHUB_SHA", gitOutput(t, "rev-parse", "HEAD"))

	metadata, err := NewBuildMetadata("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Tag != "v1.2.3" || metadata.Branch != "" {
		t.Fatalf("tag/branch = %q/%q, want v1.2.3/empty", metadata.Tag, metadata.Branch)
	}
}

func TestNewBuildMetadataExplicitValuesOverrideDetectedValues(t *testing.T) {
	clearCIEnvironment(t)
	t.Setenv("GITLAB_CI", "true")
	t.Setenv("CI_PIPELINE_IID", "7")
	t.Setenv("CI_PIPELINE_URL", "https://gitlab.example/detected")
	t.Setenv("CI_COMMIT_SHA", gitOutput(t, "rev-parse", "HEAD"))

	metadata, err := NewBuildMetadata("custom", "42", "https://ci.example/manual")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Provider != "custom" {
		t.Fatalf("provider = %q, want custom", metadata.Provider)
	}
	if metadata.Number != "42" {
		t.Fatalf("number = %q, want 42", metadata.Number)
	}
	if metadata.URL != "https://ci.example/manual" {
		t.Fatalf("URL = %q, want manual URL", metadata.URL)
	}
}

func TestNewBuildMetadataHandlesGitLabTags(t *testing.T) {
	clearCIEnvironment(t)
	t.Setenv("GITLAB_CI", "true")
	t.Setenv("CI_COMMIT_BRANCH", "main")
	t.Setenv("CI_COMMIT_TAG", "v1.2.3")
	t.Setenv("CI_COMMIT_SHA", gitOutput(t, "rev-parse", "HEAD"))
	t.Setenv("CI_COMMIT_MESSAGE", "release")
	t.Setenv("CI_PROJECT_PATH", "wodby/wodby-cli")

	metadata, err := NewBuildMetadata("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Tag != "v1.2.3" || metadata.Branch != "" {
		t.Fatalf("tag/branch = %q/%q, want v1.2.3/empty", metadata.Tag, metadata.Branch)
	}
	if metadata.Message != "release" || metadata.Slug != "wodby/wodby-cli" {
		t.Fatalf("message/slug = %q/%q", metadata.Message, metadata.Slug)
	}
}

func TestDetachedCheckoutUsesOnlyExactTags(t *testing.T) {
	clearCIEnvironment(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	runGit(t, "init")
	runGit(t, "config", "user.name", "Jane Doe")
	runGit(t, "config", "user.email", "jane@example.com")
	runGit(t, "config", "commit.gpgsign", "false")
	runGit(t, "config", "tag.gpgsign", "false")
	if err := os.WriteFile("README.md", []byte("first\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit(t, "add", "README.md")
	runGit(t, "commit", "-m", "first")
	runGit(t, "tag", "v1.0.0")
	if err := os.WriteFile("README.md", []byte("second\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit(t, "commit", "-am", "second")
	sha := gitOutput(t, "rev-parse", "HEAD")
	runGit(t, "checkout", "--detach", sha)

	metadata, err := NewBuildMetadata("", "7", "")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Tag != "" {
		t.Fatalf("tag = %q, want empty for commit after tag", metadata.Tag)
	}
	if metadata.Commit != sha {
		t.Fatalf("commit = %q, want %q", metadata.Commit, sha)
	}
}

func clearCIEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"GITHUB_ACTION", "GITHUB_ACTIONS", "TRAVIS", "CIRCLECI",
		"BITBUCKET_BUILD_NUMBER", "JENKINS_HOME", "GITLAB_CI",
	} {
		t.Setenv(name, "")
	}
}

func runGit(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func gitOutput(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}
