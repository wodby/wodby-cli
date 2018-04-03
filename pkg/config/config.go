package config

import (
	"github.com/wodby/wodby-cli/pkg/api"
	"github.com/wodby/wodby-cli/pkg/types"
)

type Config struct {
	UUID          string               `json,mapstructure:"instance"`
	DataContainer string               `json,mapstructure:"data,omitempty"`
	Context       string               `json,mapstructure:"context"`
	API           *api.Config          `json,mapstructure:"api"`
	Stack         *types.BuildConfig   `json,mapstructure:"stack"`
	Metadata      *types.BuildMetadata `json,mapstructure:"metadata"`
}
