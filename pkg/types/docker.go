package types

type (
	DockerRegistryCredentials struct {
		Host     string `json:"host"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
)
