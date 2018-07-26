package types

import (
	"os"
	"time"
	"strconv"
	"os/exec"
	"fmt"
	"strings"
)

// Tasks" statuses.
const (
	TaskStatusDone     = "Done"
	TaskStatusCanceled = "Canceled"
	TaskStatusFailed   = "Failed"
)

// CI providers
const (
	TravisCI           = "travisci"
	CircleCI           = "circleci"
	BitbucketPipelines = "bitbucket-pipelines"
	Jenkins            = "jenkins"
)

type CircleCIConfig struct {
	Jobs struct {
		Build struct {
			Docker  interface{}
			Machine interface{}
		}
	}
}

// Task is the task entity type.
type Task struct {
	ID     string
	Title  string
	Status string
}

type Service struct {
	Name  string `json,mapstructure:"name"`
	Image string `json,mapstructure:"image"`
	Slug  string `json,mapstructure:"slug"`
}

type ErrorResponse struct {
	Error struct {
		Message string `json,mapstructure:"message"`
	} `json,mapstructure:"error"`
}

// BuildConfig is the build config response type.
type BuildConfig struct {
	Services map[string]Service `json,mapstructure:"services"`
	Init *struct {
		Service     string                 `json,mapstructure:"service"`
		Command     string                 `json,mapstructure:"command"`
		Environment map[string]interface{} `json,mapstructure:"environment"`
	} `json,mapstructure:"init,omitempty"`
	Title 	string `json,mapstructure:"title"`
	Default string `json,mapstructure:"default"`
	Registry struct {
		Host string `json,mapstructure:"host"`
		Username string `json,mapstructure:"username"`
		Password string `json,mapstructure:"password"`
	} `json,mapstructure:"registry"`
	Custom bool `json,mapstructure:"custom"`
	Token string `json,mapstructure:"token"`
}

type BuildMetadata struct {
	Provider string `json,mapstructure:"provider"`
	Username string `json,mapstructure:"username"`
	Email	 string `json,mapstructure:"email"`
	Number   string `json,mapstructure:"number"`
	URL      string `json,mapstructure:"url"`
	Branch   string `json,mapstructure:"branch"`
	Commit   string `json,mapstructure:"commit"`
	Message  string `json,mapstructure:"message"`
	Tag	 	 string `json,mapstructure:"tag"`
	Slug	 string `json,mapstructure:"slug"`
	RepoURL	 string `json,mapstructure:"repo_url"`
	Id	 	 string `json,mapstructure:"id"`
}

func NewBuildMetadata(provider string, buildNumber string, url string) (*BuildMetadata, error) {
	var metadata *BuildMetadata

	if os.Getenv("TRAVIS") != "" {
		metadata = &BuildMetadata{
			Provider: TravisCI,
			URL:      url,
			Id:		  os.Getenv("TRAVIS_BUILD_ID"),
			Number:   os.Getenv("TRAVIS_BUILD_NUMBER"),
			Branch:   os.Getenv("TRAVIS_BRANCH"),
			Commit:   os.Getenv("TRAVIS_COMMIT"),
			Message:  os.Getenv("TRAVIS_COMMIT_MESSAGE"),
			Tag:      os.Getenv("TRAVIS_TAG"),
			Slug:     os.Getenv("TRAVIS_REPO_SLUG"),
		}
	} else if os.Getenv("CIRCLECI") != "" {
		metadata = &BuildMetadata{
			Provider: CircleCI,
			URL:      os.Getenv("CIRCLE_BUILD_URL"),
			Number:   os.Getenv("CIRCLE_BUILD_NUM"),
			Branch:   os.Getenv("CIRCLE_BRANCH"),
			Commit:   os.Getenv("CIRCLE_SHA1"),
			Tag:      os.Getenv("CIRCLE_TAG"),
			Slug:     os.Getenv("CIRCLE_PROJECT_REPONAME"),
			RepoURL:  os.Getenv("CIRCLE_REPOSITORY_URL"),
		}

	} else if os.Getenv("BITBUCKET_BUILD_NUMBER") != "" {
		metadata = &BuildMetadata{
			Provider: BitbucketPipelines,
			Number:   os.Getenv("BITBUCKET_BUILD_NUMBER"),
			Branch:   os.Getenv("BITBUCKET_BRANCH"),
			Commit:   os.Getenv("BITBUCKET_COMMIT"),
			Tag:      os.Getenv("BITBUCKET_TAG"),
			Slug:     os.Getenv("BITBUCKET_REPO_SLUG"),
		}
	// @todo acquire repo slug and git tag from Jenkins.
	} else if os.Getenv("JENKINS_HOME") != "" {
		metadata = &BuildMetadata{
			Provider: Jenkins,
			URL:      os.Getenv("JOB_URL"),
			Number:   os.Getenv("BUILD_NUMBER"),
			Branch:   os.Getenv("GIT_BRANCH"),
			Commit:   os.Getenv("GIT_COMMIT"),
			RepoURL:  os.Getenv("GIT_URL"),
		}
	} else {
		metadata = &BuildMetadata{}

		if provider != "" {
			metadata.Provider = provider
		} else {
			metadata.Provider = "Unknown"
		}

		if url != "" {
			metadata.URL = url
		}

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
					metadata.Tag = strings.TrimSuffix(string(out), "\n")
				}
			} else {
				metadata.Branch = branch
			}
		}

		out, err = exec.Command("git", "rev-parse", "HEAD").CombinedOutput()

		if err != nil {
			fmt.Println("Failed to acquire commit info")
		} else {
			metadata.Commit = strings.TrimSuffix(string(out), "\n")
		}

		if buildNumber != "" {
			metadata.Number = buildNumber
		} else {
			metadata.Number = strconv.FormatInt(time.Now().Unix(), 10)
		}
	}

	if metadata.Message == "" && metadata.Commit != "" {
		out, err := exec.Command("git", "log", "--format=%B", "-n", "1", metadata.Commit).CombinedOutput()

		if err != nil {
			fmt.Println("Failed to acquire commit message")
		} else {
			metadata.Message = strings.TrimSuffix(string(out), "\n")
		}
	}

	out, err := exec.Command("git", "log", "-1", metadata.Commit, "--pretty=%aN").CombinedOutput()

	if err != nil {
		fmt.Println("Failed to acquire commit author username")
	} else {
		metadata.Username = strings.TrimSpace(string(out))
	}

	out, err = exec.Command("git", "log", "-1", metadata.Commit, "--pretty=%cE").CombinedOutput()

	if err != nil {
		fmt.Println("Failed to acquire commit author email")
	} else {
		metadata.Email = strings.TrimSpace(string(out))
	}

	return metadata, nil
}
