package types

type (
	APIConfig struct {
		Key         string
		AccessToken string
		Endpoint    string
	}
	Config struct {
		ID            int
		WorkingDir    string
		Context       string
		BuiltServices []BuiltService
		API           APIConfig
		AppBuild      AppBuild
	}
	BuiltService struct {
		Name     string
		Image    string
		Released bool
	}
)
