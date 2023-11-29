package ci

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/wodby/wodby-cli/pkg/exec"
	"github.com/wodby/wodby-cli/pkg/types"
)

func CollectBuildInfo() (types.NewCIBuildInput, error) {
	var buildInput types.NewCIBuildInput

	if os.Getenv("CIRCLECI") != "" {
		buildInput = types.NewCIBuildInput{
			Provider:     "circleci",
			BuildID:      os.Getenv("CIRCLE_WORKFLOW_ID"),
			GitCommitSHA: os.Getenv("CIRCLE_SHA1"),
		}

		if os.Getenv("CIRCLE_TAG") != "" {
			buildInput.GitRefType = "tag"
			buildInput.GitRef = os.Getenv("CIRCLE_TAG")
		} else {
			buildInput.GitRefType = "branch"
			buildInput.GitRef = os.Getenv("CIRCLE_BRANCH")
		}

		var err error
		buildInput.BuildNum, err = strconv.Atoi(os.Getenv("CIRCLE_BUILD_NUM"))
		if err != nil {
			return types.NewCIBuildInput{}, errors.WithStack(err)
		}
	} else {
		out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()

		if err != nil {
			fmt.Println("Failed to acquire branch info")
		} else {
			branch := strings.TrimSuffix(string(out), "\n")

			if branch == "HEAD" {
				branch = ""
				out, err = exec.Command("git", "describe", "--tags").CombinedOutput()

				if err != nil {
					fmt.Println("Failed to acquire tag info")
				} else {
					buildInput.GitRef = strings.TrimSuffix(string(out), "\n")
					buildInput.GitRefType = "tag"
				}
			} else {
				buildInput.GitRef = branch
				buildInput.GitRefType = "branch"
			}
		}

		out, err = exec.Command("git", "rev-parse", "HEAD").CombinedOutput()

		if err != nil {
			fmt.Println("Failed to acquire commit info")
		} else {
			buildInput.GitCommitSHA = strings.TrimSuffix(string(out), "\n")
		}
	}

	if buildInput.GitCommitMessage == nil && buildInput.GitCommitSHA != "" {
		out, err := exec.Command("git", "log", "--format=%B", "-n", "1", buildInput.GitCommitSHA).CombinedOutput()

		if err != nil {
			fmt.Println("Failed to acquire commit message")
		} else {
			commitMessage := strings.TrimSuffix(string(out), "\n")
			buildInput.GitCommitMessage = &commitMessage
		}
	}

	out, err := exec.Command("git", "log", "-1", buildInput.GitCommitSHA, "--pretty=%aN").CombinedOutput()

	if err != nil {
		fmt.Println("Failed to acquire commit author username")
	} else {
		username := strings.TrimSpace(string(out))
		buildInput.GitCommitAuthorName = &username
	}

	out, err = exec.Command("git", "log", "-1", buildInput.GitCommitSHA, "--pretty=%cE").CombinedOutput()

	if err != nil {
		fmt.Println("Failed to acquire commit author email")
	} else {
		email := strings.TrimSpace(string(out))
		buildInput.GitCommitAuthorEmail = &email
	}

	return buildInput, nil
}
