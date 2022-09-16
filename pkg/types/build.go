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
		RegistryHost           string                   `json:"registryHost"`
		AppServiceBuildConfigs []*AppServiceBuildConfig `json:"appServiceBuildConfigs"`
	}
	AppServiceBuildConfig struct {
		Name         string  `json:"name"`
		Title        string  `json:"title"`
		Image        string  `json:"image"`
		Slug         string  `json:"slug"`
		Managed      bool    `json:"managed"`
		Main         bool    `json:"main"`
		Dockerfile   *string `json:"dockerfile"`
		Dockerignore *string `json:"dockerignore"`
	}
)
