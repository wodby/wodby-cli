package ci

import (
	"net/mail"
	"os"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/wodby/wodby-cli/pkg/exec"
	"github.com/wodby/wodby-cli/pkg/types"
)

func CollectBuildInfo() (types.NewBuildFromCIInput, error) {
	buildInput, detected, err := collectBuildInfoFromCIEnv()
	if err != nil {
		return types.NewBuildFromCIInput{}, err
	}

	if !detected {
		buildInput, err = collectBuildInfoFromGit()
		if err != nil {
			return types.NewBuildFromCIInput{}, err
		}
	}

	if buildInput.GitCommitMessage == nil && buildInput.GitCommitSHA != "" {
		out, err := exec.Command("git", "log", "--format=%B", "-n", "1", buildInput.GitCommitSHA).CombinedOutput()
		if err != nil {
			return types.NewBuildFromCIInput{}, errors.Wrap(err, "Failed to acquire commit message")
		}

		commitMessage := strings.TrimSuffix(string(out), "\n")
		buildInput.GitCommitMessage = &commitMessage
	}

	if buildInput.GitCommitAuthorName == nil && buildInput.GitCommitSHA != "" {
		out, err := exec.Command("git", "log", "-1", buildInput.GitCommitSHA, "--pretty=%aN").CombinedOutput()
		if err != nil {
			return types.NewBuildFromCIInput{}, errors.Wrap(err, "Failed to acquire commit author username")
		}

		username := strings.TrimSpace(string(out))
		buildInput.GitCommitAuthorName = &username
	}

	if buildInput.GitCommitAuthorEmail == nil && buildInput.GitCommitSHA != "" {
		out, err := exec.Command("git", "log", "-1", buildInput.GitCommitSHA, "--pretty=%cE").CombinedOutput()
		if err != nil {
			return types.NewBuildFromCIInput{}, errors.Wrap(err, "Failed to acquire commit author email")
		}

		email := strings.TrimSpace(string(out))
		buildInput.GitCommitAuthorEmail = &email
	}

	return buildInput, nil
}

func collectBuildInfoFromCIEnv() (types.NewBuildFromCIInput, bool, error) {
	switch {
	case os.Getenv("CIRCLECI") != "":
		buildInput, err := collectCircleCIBuildInfo()
		return buildInput, true, err
	case os.Getenv("GITLAB_CI") != "":
		buildInput, err := collectGitLabBuildInfo()
		return buildInput, true, err
	case os.Getenv("GITHUB_ACTIONS") != "":
		buildInput, err := collectGitHubActionsBuildInfo()
		return buildInput, true, err
	default:
		return types.NewBuildFromCIInput{}, false, nil
	}
}

func collectCircleCIBuildInfo() (types.NewBuildFromCIInput, error) {
	buildNum, err := parseBuildNum("CIRCLE_BUILD_NUM")
	if err != nil {
		return types.NewBuildFromCIInput{}, err
	}

	buildInput := types.NewBuildFromCIInput{
		Provider:     "circleci",
		Workflow:     os.Getenv("CIRCLE_WORKFLOW_ID"),
		BuildID:      os.Getenv("CIRCLE_WORKFLOW_JOB_ID"),
		BuildNum:     buildNum,
		GitCommitSHA: os.Getenv("CIRCLE_SHA1"),
	}

	if os.Getenv("CIRCLE_TAG") != "" {
		buildInput.GitRefType = string(types.GitRefTypeTag)
		buildInput.GitRef = os.Getenv("CIRCLE_TAG")
	} else {
		buildInput.GitRefType = string(types.GitRefTypeBranch)
		buildInput.GitRef = os.Getenv("CIRCLE_BRANCH")
	}

	return buildInput, nil
}

func collectGitLabBuildInfo() (types.NewBuildFromCIInput, error) {
	buildNum, err := parseBuildNum("CI_PIPELINE_IID")
	if err != nil {
		return types.NewBuildFromCIInput{}, err
	}

	buildInput := types.NewBuildFromCIInput{
		Provider:     "gitlab",
		Workflow:     os.Getenv("CI_PIPELINE_ID"),
		BuildID:      os.Getenv("CI_JOB_ID"),
		BuildNum:     buildNum,
		GitCommitSHA: os.Getenv("CI_COMMIT_SHA"),
	}

	if tag := os.Getenv("CI_COMMIT_TAG"); tag != "" {
		buildInput.GitRefType = string(types.GitRefTypeTag)
		buildInput.GitRef = tag
	} else {
		buildInput.GitRefType = string(types.GitRefTypeBranch)
		buildInput.GitRef = firstNonEmpty(os.Getenv("CI_COMMIT_BRANCH"), os.Getenv("CI_COMMIT_REF_NAME"))
	}

	if commitMessage := os.Getenv("CI_COMMIT_MESSAGE"); commitMessage != "" {
		buildInput.GitCommitMessage = &commitMessage
	}

	if author := parseGitLabCommitAuthor(os.Getenv("CI_COMMIT_AUTHOR")); author != nil {
		buildInput.GitCommitAuthorName = author.name
		buildInput.GitCommitAuthorEmail = author.email
	}

	return buildInput, nil
}

func collectGitHubActionsBuildInfo() (types.NewBuildFromCIInput, error) {
	buildNum, err := parseBuildNum("GITHUB_RUN_NUMBER")
	if err != nil {
		return types.NewBuildFromCIInput{}, err
	}

	buildInput := types.NewBuildFromCIInput{
		Provider:     "githubactions",
		Workflow:     os.Getenv("GITHUB_RUN_ID"),
		BuildID:      githubActionsBuildID(),
		BuildNum:     buildNum,
		GitCommitSHA: os.Getenv("GITHUB_SHA"),
	}

	if os.Getenv("GITHUB_REF_TYPE") == "tag" {
		buildInput.GitRefType = string(types.GitRefTypeTag)
		buildInput.GitRef = os.Getenv("GITHUB_REF_NAME")
	} else {
		buildInput.GitRefType = string(types.GitRefTypeBranch)
		buildInput.GitRef = firstNonEmpty(os.Getenv("GITHUB_HEAD_REF"), os.Getenv("GITHUB_REF_NAME"))
	}

	return buildInput, nil
}

func collectBuildInfoFromGit() (types.NewBuildFromCIInput, error) {
	var buildInput types.NewBuildFromCIInput

	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	if err != nil {
		return types.NewBuildFromCIInput{}, errors.Wrap(err, "Failed to acquire branch info")
	}

	branch := strings.TrimSuffix(string(out), "\n")
	if branch == "HEAD" {
		out, err = exec.Command("git", "describe", "--tags").CombinedOutput()
		if err != nil {
			return types.NewBuildFromCIInput{}, errors.Wrap(err, "Failed to acquire tag info")
		}
		buildInput.GitRef = strings.TrimSuffix(string(out), "\n")
		buildInput.GitRefType = string(types.GitRefTypeTag)
	} else {
		buildInput.GitRef = branch
		buildInput.GitRefType = string(types.GitRefTypeBranch)
	}

	out, err = exec.Command("git", "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		return types.NewBuildFromCIInput{}, errors.Wrap(err, "Failed to acquire commit info")
	}

	buildInput.GitCommitSHA = strings.TrimSuffix(string(out), "\n")

	return buildInput, nil
}

func parseBuildNum(envVar string) (int, error) {
	buildNum, err := strconv.Atoi(os.Getenv(envVar))
	if err != nil {
		return 0, errors.Wrapf(err, "Failed to parse %s", envVar)
	}

	return buildNum, nil
}

func githubActionsBuildID() string {
	runID := os.Getenv("GITHUB_RUN_ID")
	jobID := os.Getenv("GITHUB_JOB")
	runAttempt := os.Getenv("GITHUB_RUN_ATTEMPT")

	switch {
	case runID != "" && jobID != "" && runAttempt != "":
		return strings.Join([]string{runID, jobID, runAttempt}, ":")
	case runID != "" && jobID != "":
		return strings.Join([]string{runID, jobID}, ":")
	case runID != "":
		return runID
	default:
		return jobID
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}

type gitLabCommitAuthor struct {
	name  *string
	email *string
}

func parseGitLabCommitAuthor(author string) *gitLabCommitAuthor {
	if author == "" {
		return nil
	}

	address, err := mail.ParseAddress(author)
	if err != nil {
		return nil
	}

	name := address.Name
	email := address.Address

	return &gitLabCommitAuthor{
		name:  &name,
		email: &email,
	}
}
