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
	req, err := c.getAuthorizedRequest(APP_BUILD)
	if err != nil {
		return types.AppBuild{}, errors.WithStack(err)
	}
	req.Var("id", id)

	var respData struct {
		AppBuild types.AppBuild `json:"appBuild"`
	}
	if err := c.client.Run(ctx, req, &respData); err != nil {
		return types.AppBuild{}, errors.WithStack(err)
	}

	return respData.AppBuild, nil
}

func (c *client) GetDockerRegistryCredentials(ctx context.Context, appBuildID int) (types.DockerRegistryCredentials, error) {
	req, err := c.getAuthorizedRequest(DOCKER_REGISTRY_CREDENTIALS)
	if err != nil {
		return types.DockerRegistryCredentials{}, errors.WithStack(err)
	}
	req.Var("appBuildID", appBuildID)

	var respData struct {
		DockerRegistryCredentials types.DockerRegistryCredentials `json:"dockerRegistryCredentials"`
	}

	if err := c.client.Run(ctx, req, &respData); err != nil {
		return types.DockerRegistryCredentials{}, errors.WithStack(err)
	}

	return respData.DockerRegistryCredentials, nil
}

func (c *client) Deploy(ctx context.Context, input types.DeploymentInput) (bool, error) {
	req, err := c.getAuthorizedRequest(DEPLOY)
	if err != nil {
		return false, errors.WithStack(err)
	}
	req.Var("input", input)

	var respData struct {
		AppDeployment types.AppDeployment `json:"appDeployment"`
	}

	if err := c.client.Run(ctx, req, &respData); err != nil {
		return false, errors.WithStack(err)
	}

	return true, nil
}

func (c *client) getAuthorizedRequest(query string) (*graphql.Request, error) {
	req := graphql.NewRequest(query)

	if c.config.Key != "" {
		req.Header.Set("X-API-KEY", c.config.Key)
	} else {
		req.Header.Set("X-ACCESS-TOKEN", c.config.AccessToken)
	}

	return req, nil
}
