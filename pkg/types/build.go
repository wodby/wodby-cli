package types

const (
	GitRefTypeBranch GitRefType = "branch"
)

type (
	GitRefType string
	AppBuild   struct {
		ID         int             `json:"id"`
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
		Name         string                `json:"name"`
		Title        string                `json:"title"`
		Image        string                `json:"image"`
		Managed      bool                  `json:"managed"`
		Main         bool                  `json:"main"`
		Dockerfile   *string               `json:"dockerfile"`
		Dockerignore *string               `json:"dockerignore"`
		Args         []*AppServiceBuildArg `json:"args"`
	}
	AppServiceBuildArg struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	NewBuildFromCIInput struct {
		GitRepoID            int     `json:"gitRepoID"`
		GitCommitSHA         string  `json:"gitCommitSHA"`
		GitRef               string  `json:"gitRef"`
		GitRefType           string  `json:"gitRefType"`
		Workflow             string  `json:"workflow"`
		BuildNum             int     `json:"buildNum"`
		BuildID              string  `json:"buildID"`
		GitCommitAuthorName  *string `json:"gitCommitAuthorName"`
		GitCommitAuthorEmail *string `json:"gitCommitAuthorEmail"`
		GitCommitMessage     *string `json:"gitCommitMessage"`
		Provider             string  `json:"provider"`
		SkipPostDeployment   *bool   `json:"skipPostDeployment"`
	}
)
