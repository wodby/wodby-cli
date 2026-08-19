package types

type (
	APIConfig struct {
		Key         string
		AccessToken string
		Endpoint    string
	}
	Config struct {
		ID            string
		DataContainer string
		WorkingDir    string
		Context       string
		BuiltServices []BuiltService
		API           APIConfig
		AppBuild      AppBuild
	}
	BuiltService struct {
		Name  string
		Image string
		// Unmanaged marks an image that was not built FROM the service image,
		// so it no longer tracks service image updates.
		Unmanaged bool
		// DockerfilePath is set only for an author-provided Dockerfile.
		DockerfilePath string
		// DockerfileHash is the SHA-256 of the Dockerfile that produced the image.
		DockerfileHash string
		Released       bool
	}
)
