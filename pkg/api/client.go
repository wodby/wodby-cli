package api

import (
	"context"

	"github.com/machinebox/graphql"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/wodby/wodby-cli/pkg/types"
)

type client struct {
	client *graphql.Client
	config types.APIConfig
	logger *logrus.Entry
}

func NewClient(config types.APIConfig) *client {
	return &client{
		client: graphql.NewClient(config.Endpoint),
		config: config,
		logger: logrus.WithField("logger", "client"),
	}
}

func (c *client) GetAppBuild(ctx context.Context, id int) (types.AppBuild, error) {
	req := graphql.NewRequest(APP_BUILD)
	req.Var("id", id)

	var respData map[string]interface{}
	req.Header.Set("X-API-KEY", c.config.Key)

	if err := c.client.Run(ctx, req, &respData); err != nil {
		return types.AppBuild{}, errors.WithStack(err)
	}

	return respData["appBuild"].(types.AppBuild), nil
}

func (c *client) GetDockerRegistryCredentials(ctx context.Context, appBuildID int) (types.DockerRegistryCredentials, error) {
	req := graphql.NewRequest(DOCKER_REGISTRY_CREDENTIALS)
	req.Var("appBuildID", appBuildID)

	var respData map[string]interface{}
	req.Header.Set("X-API-KEY", c.config.Key)

	if err := c.client.Run(ctx, req, &respData); err != nil {
		return types.DockerRegistryCredentials{}, errors.WithStack(err)
	}

	return respData["dockerRegistryCredentials"].(types.DockerRegistryCredentials), nil
}

func (c *client) Deploy(ctx context.Context, input types.DeploymentInput) (bool, error) {
	req := graphql.NewRequest(DEPLOY)
	req.Var("input", input)

	var respData map[string]interface{}
	req.Header.Set("X-API-KEY", c.config.Key)

	if err := c.client.Run(ctx, req, &respData); err != nil {
		return false, errors.WithStack(err)
	}

	return respData["deploy"].(bool), nil
}