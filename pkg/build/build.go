package build

import (
	"github.com/wodby/wodby-cli/pkg/docker"
	"github.com/wodby/wodby-cli/pkg/types"
)

// Builder is builder representation.
type Builder struct {
	Client *docker.Client
}

// Build builds docker images for wodby services by config.
func (b *Builder) Build(config *types.BuildConfig, context string) error {
	return nil
}

func NewBuilder() *Builder {
	return &Builder{Client: docker.NewClient()}
}
