package types

import (
	"strings"
)

const (
	GitRefTypeBranch GitRefType = "BRANCH"
	GitRefTypeTag    GitRefType = "TAG"
	GitRefTypeCommit GitRefType = "COMMIT"
)

type (
	GitRefType string
	AppBuild   struct {
		ID         ID              `json:"id"`
		Number     int             `json:"number"`
		GitRefType GitRefType      `json:"gitRefType"`
		GitRef     string          `json:"gitRef"`
		Config     *AppBuildConfig `json:"config"`
	}
	AppBuildConfig struct {
		RegistryHost       string                   `json:"registryHost"`
		RegistryRepository string                   `json:"registryRepository"`
		Services           []*AppServiceBuildConfig `json:"services"`
	}
	AppServiceBuildConfig struct {
		Name         string  `json:"name"`
		Title        string  `json:"title"`
		Image        string  `json:"image"`
		Managed      bool    `json:"managed"`
		Main         bool    `json:"main"`
		Dockerfile   *string `json:"dockerfile"`
		Dockerignore *string `json:"dockerignore"`
		// CopyFrom and CopyTo narrow the build to a subdirectory, relative to the
		// --from and --to paths. Empty means copy the whole context.
		CopyFrom string                `json:"copyFrom"`
		CopyTo   string                `json:"copyTo"`
		Args     []*AppServiceBuildArg `json:"args"`
	}
	AppServiceBuildArg struct {
		Name   string `json:"name"`
		Value  string `json:"value"`
		Secret bool   `json:"secret"`
	}
	NewBuildFromCIInput struct {
		AppServiceID         ID      `json:"appServiceId"`
		GitCommitSHA         string  `json:"gitCommitSHA"`
		GitRef               string  `json:"gitRef"`
		GitRefType           string  `json:"gitRefType"`
		Workflow             string  `json:"workflow"`
		BuildNum             int     `json:"buildNum"`
		BuildID              string  `json:"buildId"`
		GitCommitAuthorName  *string `json:"gitCommitAuthorName"`
		GitCommitAuthorEmail *string `json:"gitCommitAuthorEmail"`
		GitCommitMessage     *string `json:"gitCommitMessage"`
		Provider             string  `json:"provider"`
		PostDeployment       *string `json:"postDeployment"`
	}
)

func (t GitRefType) Normalize() GitRefType {
	switch strings.ToUpper(string(t)) {
	case string(GitRefTypeBranch):
		return GitRefTypeBranch
	case string(GitRefTypeTag):
		return GitRefTypeTag
	case string(GitRefTypeCommit):
		return GitRefTypeCommit
	default:
		return t
	}
}
