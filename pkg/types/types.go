package types

import (
	"os"
	"time"
	"strconv"
	"os/exec"
	"fmt"
	"github.com/pkg/errors"
	"strings"
)

// Tasks' statuses.
const (
	TaskStatusDone     = "Done"
	TaskStatusWaiting  = "Waiting"
	TaskStatusProgress = "In progress"
	TaskStatusCanceled = "Canceled"
	TaskStatusFailed   = "Failed"
)

// CI providers
const (
	TravisCIName        = "Travis CI"
	CircleCIName        = "CircleCI"
	BitbucketCIName     = "Bitbucket Pipelines"
	CodeshipBasicCIName = "Codeship Basic"
	CodeshipProCIName   = "Codeship Pro"
	JenkinsName   		= "Jenkins"
	GitLabCIName   		= "GitLab CI"
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
}

type BuildMetadata struct {
	Known    bool   `json,mapstructure:"known"`
	Provider string `json,mapstructure:"provider"`
	Username string `json,mapstructure:"username"`
	Number   string `json,mapstructure:"build_number"`
	URL      string `json,mapstructure:"build_url"`
	Comment  string `json,mapstructure:"comment"`
	Branch   string `json,mapstructure:"branch"`
}

func NewBuildMetadata(buildNumber string) (*BuildMetadata, error) {
	var metadata *BuildMetadata

	if os.Getenv("TRAVIS") != "" {
		var url = fmt.Sprintf(
			"https://travis-ci.org/%s/builds/%s",
			os.Getenv("TRAVIS_REPO_SLUG"),
			os.Getenv("TRAVIS_BUILD_ID"))

		metadata = &BuildMetadata{
			Known:    true,
			Provider: TravisCIName,
			URL:	  url,
			Number:   os.Getenv("TRAVIS_BUILD_NUMBER"),
			Branch:   os.Getenv("TRAVIS_BRANCH"),
		}
	} else if os.Getenv("CIRCLECI") != "" {
		metadata = &BuildMetadata{
			Known:    true,
			Provider: CircleCIName,
			URL:      os.Getenv("CIRCLE_BUILD_URL"),
			Number:   os.Getenv("CIRCLE_BUILD_NUM"),
			Branch:   os.Getenv("CIRCLE_BRANCH"),
		}
	} else if os.Getenv("BITBUCKET_BUILD_NUMBER") != "" {
		var url = fmt.Sprintf(
			"https://bitbucket.org/%s/addon/pipelines/home#!/results/%s",
			os.Getenv("BITBUCKET_REPO_SLUG"),
			os.Getenv("BITBUCKET_BUILD_NUMBER"))

		metadata = &BuildMetadata{
			Known:    true,
			Provider: BitbucketCIName,
			URL:      url,
			Number:   os.Getenv("BITBUCKET_BUILD_NUMBER"),
			Branch:   os.Getenv("BITBUCKET_BRANCH"),
		}
	} else if os.Getenv("JENKINS_HOME") != "" {
		metadata = &BuildMetadata{
			Known:    true,
			Provider: JenkinsName,
			URL:   	  os.Getenv("JOB_URL"),
			Number:   os.Getenv("BUILD_NUMBER"),
			Branch:   os.Getenv("GIT_BRANCH"),
		}
	} else {
		metadata = &BuildMetadata{
			Known: false,
		}

		out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()

		if err != nil {
			return nil, errors.New(string(out))
		}

		branch := strings.TrimSuffix(string(out), "\n")

		if branch == "HEAD" {
			branch = ""
		}

		metadata.Branch = branch

		if buildNumber != "" {
			metadata.Number = buildNumber
		} else {
			metadata.Number =  strconv.FormatInt(time.Now().Unix(), 10)
		}
	}

	return metadata, nil
}
