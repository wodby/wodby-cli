package api

import (
	"context"
	"fmt"
	"os"

	"github.com/machinebox/graphql"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/wodby/wodby-cli/pkg/types"
)

type Client struct {
	client *graphql.Client
	config types.APIConfig
	logger *logrus.Entry
}

func NewClient(config types.APIConfig) *Client {
	if os.Getenv("DEBUG") != "" {
		logrus.SetLevel(logrus.DebugLevel)
	}
	return &Client{
		client: graphql.NewClient(config.Endpoint),
		config: config,
		logger: logrus.WithField("logger", "client"),
	}
}

func (c *Client) GetAppBuild(ctx context.Context, id int) (types.AppBuild, error) {
	req, err := c.getAuthorizedRequest(APP_BUILD)
	if err != nil {
		return types.AppBuild{}, errors.WithStack(err)
	}
	req.Var("id", id)

	var respData struct {
		AppBuild types.AppBuild `json:"appBuild"`
	}
	c.logger.Debugf("Exec get app build request [id: %d]", id)
	if err := c.client.Run(ctx, req, &respData); err != nil {
		c.logger.Debugf("%v", respData)
		return types.AppBuild{}, errors.WithStack(err)
	}

	return respData.AppBuild, nil
}

func (c *Client) GetDockerRegistryCredentials(ctx context.Context, appBuildID int) (types.DockerRegistryCredentials, error) {
	req, err := c.getAuthorizedRequest(DOCKER_REGISTRY_CREDENTIALS)
	if err != nil {
		return types.DockerRegistryCredentials{}, errors.WithStack(err)
	}
	req.Var("appBuildID", appBuildID)

	var respData struct {
		DockerRegistryCredentials types.DockerRegistryCredentials `json:"dockerRegistryCredentials"`
	}

	c.logger.Debugf("Exec get docker credentials request [app build id: %d]", appBuildID)
	if err := c.client.Run(ctx, req, &respData); err != nil {
		return types.DockerRegistryCredentials{}, errors.WithStack(err)
	}

	return respData.DockerRegistryCredentials, nil
}

func (c *Client) Deploy(ctx context.Context, input types.DeploymentInput) (bool, error) {
	req, err := c.getAuthorizedRequest(DEPLOY)
	if err != nil {
		return false, errors.WithStack(err)
	}
	req.Var("input", input)

	var respData struct {
		AppDeployment types.AppDeployment `json:"appDeployment"`
	}

	c.logger.Debugf("Exec deploy request [input: %v]", input)
	if err := c.client.Run(ctx, req, &respData); err != nil {
		return false, errors.WithStack(err)
	}

	return true, nil
}

func (c *Client) NewCIBuild(ctx context.Context, input types.NewBuildFromCIInput) (types.AppBuild, error) {
	req, err := c.getAuthorizedRequest(NEW_CI_BUILD)
	if err != nil {
		return types.AppBuild{}, errors.WithStack(err)
	}
	req.Var("input", input)

	var respData struct {
		AppBuild types.AppBuild `json:"appBuild"`
	}

	c.logger.Debugf("Exec new CI build request [input: %v]", input)
	if err := c.client.Run(ctx, req, &respData); err != nil {
		return types.AppBuild{}, errors.WithStack(err)
	}
	fmt.Printf("%+v\n", respData)

	return respData.AppBuild, nil
}

func (c *Client) getAuthorizedRequest(query string) (*graphql.Request, error) {
	req := graphql.NewRequest(query)

	if c.config.Key != "" {
		req.Header.Set("X-API-KEY", c.config.Key)
	} else {
		req.Header.Set("X-ACCESS-TOKEN", c.config.AccessToken)
	}

	return req, nil
}
