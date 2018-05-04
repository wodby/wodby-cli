package types

import (
	"os"
	"time"
	"strconv"
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
}

func NewBuildMetadata(buildNumber string) *BuildMetadata {
	if os.Getenv("TRAVIS") != "" {
		var url = "https://travis-ci.org/" + os.Getenv("TRAVIS_REPO_SLUG") + "/builds/" + os.Getenv("TRAVIS_BUILD_ID")

		return &BuildMetadata{
			Known:    true,
			Provider: TravisCIName,
			URL:	  url,
			Number:   os.Getenv("TRAVIS_BUILD_NUMBER"),
		}
	} else if os.Getenv("CIRCLECI") != "" {
		return &BuildMetadata{
			Known:    true,
			Provider: CircleCIName,
			Number:   os.Getenv("CIRCLE_BUILD_NUM"),
			URL:      os.Getenv("CIRCLE_BUILD_URL"),
		}
	} else if os.Getenv("BITBUCKET_BUILD_NUMBER") != "" {
		var url = "https://bitbucket.org/" + os.Getenv("BITBUCKET_REPO_SLUG") + "/addon/pipelines/home#!/results/" + os.Getenv("BITBUCKET_BUILD_NUMBER")

		return &BuildMetadata{
			Known:    true,
			Provider: BitbucketCIName,
			Number:   os.Getenv("BITBUCKET_BUILD_NUMBER"),
			URL:      url,
		}
	} else if os.Getenv("JENKINS_HOME") != "" {
		return &BuildMetadata{
			Known:    true,
			Provider: JenkinsName,
			Number:   os.Getenv("BUILD_NUMBER"),
			URL:   	  os.Getenv("JOB_URL"),
		}
	}

	//else if os.Getenv("CI_NAME") == "codeship" {
		//if os.Getenv("CI_BUILD_ID") != "" {
		//	return &BuildMetadata{
		//		Known:    true,
		//		Provider: CodeshipProCIName,
		//		Number:   os.Getenv("CI_BUILD_ID"),
		//	}
		//} else {
		//	return &BuildMetadata{
		//		Known:    true,
		//		Provider: CodeshipBasicCIName,
		//		Number:   os.Getenv("CI_BUILD_NUMBER"),
		//		URL:   	  os.Getenv("CI_BUILD_URL"),
		//	}
		//}
	//}

	metadata := &BuildMetadata{
		Known: false,
	}

	if metadata.Number == "" {
		if buildNumber != "" {
			metadata.Number = buildNumber
		} else {
			metadata.Number =  strconv.FormatInt(time.Now().Unix(), 10)
		}
	}

	return metadata
}
