package config

import (
	"github.com/wodby/wodby-cli/pkg/api"
	"github.com/wodby/wodby-cli/pkg/types"
	"github.com/pkg/errors"
)

type Config struct {
	UUID          string               `json,mapstructure:"instance"`
	DataContainer string               `json,mapstructure:"data,omitempty"`
	Context       string               `json,mapstructure:"context"`
	API           *api.Config          `json,mapstructure:"api"`
	Stack         *types.BuildConfig   `json,mapstructure:"stack"`
	Metadata      *types.BuildMetadata `json,mapstructure:"metadata"`
}

func (config *Config) FindService(serviceName string) (types.Service, error) {
	for _, service := range config.Stack.Services {
		if service.Name == serviceName {
			return service, nil
		}
	}

	return types.Service{}, errors.New("Service not found")
}
