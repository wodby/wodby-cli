package ci

import (
	"os"
	"os/exec"
	"strings"
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

func TestCollectBuildInfoFromGitFallsBackToUnknownProvider(t *testing.T) {
	t.Setenv("CIRCLECI", "")
	t.Setenv("GITLAB_CI", "")
	t.Setenv("GITHUB_ACTIONS", "")

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

	runGit(t, "init")
	runGit(t, "checkout", "-b", "main")
	runGit(t, "config", "user.name", "Jane Doe")
	runGit(t, "config", "user.email", "jane@example.com")
	if err := os.WriteFile("README.md", []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("failed to write README.md: %v", err)
	}
	runGit(t, "add", "README.md")
	runGit(t, "commit", "-m", "initial commit")

	buildInput, err := CollectBuildInfo()
	if err != nil {
		t.Fatalf("CollectBuildInfo returned error: %v", err)
	}

	if buildInput.Provider != ProviderUnknown {
		t.Fatalf("expected provider %q, got %q", ProviderUnknown, buildInput.Provider)
	}
	if buildInput.GitRefType != string(types.GitRefTypeBranch) {
		t.Fatalf("expected branch ref type, got %q", buildInput.GitRefType)
	}
	if buildInput.GitRef != "main" {
		t.Fatalf("expected branch main, got %q", buildInput.GitRef)
	}
	if buildInput.GitCommitSHA == "" {
		t.Fatal("expected commit SHA")
	}
	if buildInput.GitCommitMessage == nil || strings.TrimSpace(*buildInput.GitCommitMessage) != "initial commit" {
		t.Fatalf("expected commit message from git, got %#v", buildInput.GitCommitMessage)
	}
	if buildInput.GitCommitAuthorName == nil || *buildInput.GitCommitAuthorName != "Jane Doe" {
		t.Fatalf("expected commit author name Jane Doe, got %#v", buildInput.GitCommitAuthorName)
	}
	if buildInput.GitCommitAuthorEmail == nil || *buildInput.GitCommitAuthorEmail != "jane@example.com" {
		t.Fatalf("expected commit author email jane@example.com, got %#v", buildInput.GitCommitAuthorEmail)
	}
}

func runGit(t *testing.T, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func runGitOutput(t *testing.T, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func initDetachedCheckoutTestRepo(t *testing.T) string {
	t.Helper()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current directory: %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatalf("failed to restore directory: %v", err)
		}
	})

	runGit(t, "init")
	runGit(t, "checkout", "-b", "main")
	runGit(t, "config", "user.name", "Jane Doe")
	runGit(t, "config", "user.email", "jane@example.com")
	if err := os.WriteFile("README.md", []byte("first\n"), 0o644); err != nil {
		t.Fatalf("failed to write README.md: %v", err)
	}
	runGit(t, "add", "README.md")
	runGit(t, "commit", "-m", "first commit")
	return runGitOutput(t, "rev-parse", "HEAD")
}

func TestCollectBuildInfoFromDetachedGitCheckout(t *testing.T) {
	tests := []struct {
		name     string
		prepare  func(*testing.T, string) string
		wantType types.GitRefType
		wantRef  func(string) string
	}{
		{
			name: "untagged commit uses commit ref",
			prepare: func(t *testing.T, sha string) string {
				runGit(t, "checkout", "--detach", sha)
				return sha
			},
			wantType: types.GitRefTypeCommit,
			wantRef:  func(sha string) string { return sha },
		},
		{
			name: "lightweight exact tag uses tag ref",
			prepare: func(t *testing.T, sha string) string {
				runGit(t, "tag", "v1.2.3")
				runGit(t, "checkout", "--detach", sha)
				return sha
			},
			wantType: types.GitRefTypeTag,
			wantRef:  func(string) string { return "v1.2.3" },
		},
		{
			name: "annotated exact tag uses tag ref",
			prepare: func(t *testing.T, sha string) string {
				runGit(t, "tag", "-a", "v2.0.0", "-m", "release")
				runGit(t, "checkout", "--detach", sha)
				return sha
			},
			wantType: types.GitRefTypeTag,
			wantRef:  func(string) string { return "v2.0.0" },
		},
		{
			name: "commit after reachable tag stays a commit ref",
			prepare: func(t *testing.T, _ string) string {
				runGit(t, "tag", "v1.0.0")
				if err := os.WriteFile("README.md", []byte("second\n"), 0o644); err != nil {
					t.Fatalf("failed to update README.md: %v", err)
				}
				runGit(t, "add", "README.md")
				runGit(t, "commit", "-m", "second commit")
				sha := runGitOutput(t, "rev-parse", "HEAD")
				runGit(t, "checkout", "--detach", sha)
				return sha
			},
			wantType: types.GitRefTypeCommit,
			wantRef:  func(sha string) string { return sha },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sha := initDetachedCheckoutTestRepo(t)
			sha = tt.prepare(t, sha)

			buildInput, err := collectBuildInfoFromGit()
			if err != nil {
				t.Fatalf("collectBuildInfoFromGit() error = %v", err)
			}
			if buildInput.GitCommitSHA != sha {
				t.Fatalf("GitCommitSHA = %q, want %q", buildInput.GitCommitSHA, sha)
			}
			if buildInput.GitRefType != string(tt.wantType) {
				t.Fatalf("GitRefType = %q, want %q", buildInput.GitRefType, tt.wantType)
			}
			if want := tt.wantRef(sha); buildInput.GitRef != want {
				t.Fatalf("GitRef = %q, want %q", buildInput.GitRef, want)
			}
		})
	}
}

func TestCollectGitHubActionsBuildInfoUsesHeadRefForPullRequests(t *testing.T) {
	t.Setenv("GITHUB_RUN_ID", "1001")
	t.Setenv("GITHUB_RUN_NUMBER", "17")
	t.Setenv("GITHUB_RUN_ATTEMPT", "2")
	t.Setenv("GITHUB_JOB", "build")
	t.Setenv("GITHUB_WORKFLOW_REF", "wodby/wodby-cli/.github/workflows/build.yml@refs/heads/main")
	t.Setenv("GITHUB_SHA", "deadbeef")
	t.Setenv("GITHUB_REF_TYPE", "branch")
	t.Setenv("GITHUB_REF_NAME", "123/merge")
	t.Setenv("GITHUB_HEAD_REF", "feature/login")

	buildInput, err := collectGitHubActionsBuildInfo()
	if err != nil {
		t.Fatalf("collectGitHubActionsBuildInfo returned error: %v", err)
	}

	if buildInput.Provider != "github" {
		t.Fatalf("expected provider github, got %q", buildInput.Provider)
	}
	if buildInput.Workflow != ".github/workflows/build.yml" {
		t.Fatalf("expected workflow path .github/workflows/build.yml, got %q", buildInput.Workflow)
	}
	if buildInput.BuildID != "1001" {
		t.Fatalf("expected build ID 1001, got %q", buildInput.BuildID)
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
	t.Setenv("GITHUB_WORKFLOW_REF", "wodby/wodby-cli/.github/workflows/release.yml@refs/tags/v1.2.3")
	t.Setenv("GITHUB_SHA", "feedface")
	t.Setenv("GITHUB_REF_TYPE", "tag")
	t.Setenv("GITHUB_REF_NAME", "v1.2.3")

	buildInput, err := collectGitHubActionsBuildInfo()
	if err != nil {
		t.Fatalf("collectGitHubActionsBuildInfo returned error: %v", err)
	}

	if buildInput.BuildID != "2002" {
		t.Fatalf("expected build ID 2002, got %q", buildInput.BuildID)
	}
	if buildInput.Workflow != ".github/workflows/release.yml" {
		t.Fatalf("expected workflow path .github/workflows/release.yml, got %q", buildInput.Workflow)
	}
	if buildInput.GitRefType != string(types.GitRefTypeTag) {
		t.Fatalf("expected tag ref type, got %q", buildInput.GitRefType)
	}
	if buildInput.GitRef != "v1.2.3" {
		t.Fatalf("expected tag v1.2.3, got %q", buildInput.GitRef)
	}
}

func TestParseGitHubWorkflowRef(t *testing.T) {
	testCases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "extracts workflow path from ref",
			in:   "wodby/wodby-cli/.github/workflows/build.yml@refs/heads/main",
			want: ".github/workflows/build.yml",
		},
		{
			name: "returns original value when path prefix is missing",
			in:   ".github/workflows/build.yml@refs/heads/main",
			want: ".github/workflows/build.yml",
		},
		{
			name: "returns empty value",
			in:   "",
			want: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseGitHubWorkflowRef(tc.in); got != tc.want {
				t.Fatalf("parseGitHubWorkflowRef(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
