package types

type (
	DeploymentInput struct {
		AppBuildID     int                       `json:"appBuildID"`
		Services       []*ServiceDeploymentInput `json:"services"`
		PostDeployment bool                      `json:"postDeployment"`
	}
	ServiceDeploymentInput struct {
		Name  string `json:"name"`
		Image string `json:"image"`
	}
)
