package api

import (
	"context"
	"net/url"
	"strconv"

	"github.com/pkg/errors"
	"github.com/wodby/wodby-cli/pkg/api/rest"
	"github.com/wodby/wodby-cli/pkg/types"
)

type Client struct {
	client *rest.Client
}

type newBuildFromCIRequest struct {
	AppServiceID         int     `json:"appServiceId"`
	GitCommitSHA         string  `json:"gitCommitSHA"`
	GitRef               string  `json:"gitRef"`
	GitRefType           string  `json:"gitRefType"`
	Workflow             string  `json:"workflow"`
	BuildNum             int     `json:"buildNum"`
	BuildID              string  `json:"buildId"`
	GitCommitAuthorName  *string `json:"gitCommitAuthorName"`
	GitCommitAuthorEmail *string `json:"gitCommitAuthorEmail"`
	GitCommitMessage     *string `json:"gitCommitMessage"`
	Provider             string  `json:"provider"`
	PostDeployment       *string `json:"postDeployment"`
}

type deploymentFromCIRequest struct {
	AppBuildID         int                             `json:"appBuildId"`
	Services           []*types.ServiceDeploymentInput `json:"services"`
	SkipPostDeployment bool                            `json:"skipPostDeployment"`
}

func NewClient(config types.APIConfig) (*Client, error) {
	client, err := rest.NewClient(config)
	if err != nil {
		return nil, err
	}
	return &Client{
		client: client,
	}, nil
}

func (c *Client) GetAppBuild(ctx context.Context, id types.ID) (types.AppBuild, error) {
	var appBuild types.AppBuild
	if err := c.client.Get(ctx, "/app-builds/"+url.PathEscape(id.String()), nil, &appBuild); err != nil {
		return types.AppBuild{}, errors.WithStack(err)
	}
	return appBuild, nil
}

func (c *Client) GetAppBuildConfig(ctx context.Context, appBuildID types.ID) (types.AppBuildConfig, error) {
	var config types.AppBuildConfig
	if err := c.client.Get(ctx, "/app-builds/"+url.PathEscape(appBuildID.String())+"/config", nil, &config); err != nil {
		return types.AppBuildConfig{}, errors.WithStack(err)
	}
	return config, nil
}

func (c *Client) GetDockerRegistryCredentials(ctx context.Context, appBuildID types.ID) (types.DockerRegistryCredentials, error) {
	var credentials types.DockerRegistryCredentials
	if err := c.client.Get(ctx, "/app-builds/"+url.PathEscape(appBuildID.String())+"/docker-registry-credentials", nil, &credentials); err != nil {
		return types.DockerRegistryCredentials{}, errors.WithStack(err)
	}
	return credentials, nil
}

func (c *Client) Deploy(ctx context.Context, input types.DeploymentFromCIInput) (types.AppDeployment, error) {
	appBuildID, err := idAsInt(input.AppBuildID, "app build id")
	if err != nil {
		return types.AppDeployment{}, err
	}

	request := deploymentFromCIRequest{
		AppBuildID:         appBuildID,
		Services:           input.Services,
		SkipPostDeployment: input.SkipPostDeployment,
	}

	var appDeployment types.AppDeployment
	if err := c.client.Post(ctx, "/app-deployments/from-ci", nil, request, &appDeployment); err != nil {
		return types.AppDeployment{}, errors.WithStack(err)
	}

	return appDeployment, nil
}

func (c *Client) NewCIBuild(ctx context.Context, input types.NewBuildFromCIInput) (types.AppBuild, error) {
	appServiceID, err := idAsInt(input.AppServiceID, "app service id")
	if err != nil {
		return types.AppBuild{}, err
	}

	request := newBuildFromCIRequest{
		AppServiceID:         appServiceID,
		GitCommitSHA:         input.GitCommitSHA,
		GitRef:               input.GitRef,
		GitRefType:           input.GitRefType,
		Workflow:             input.Workflow,
		BuildNum:             input.BuildNum,
		BuildID:              input.BuildID,
		GitCommitAuthorName:  input.GitCommitAuthorName,
		GitCommitAuthorEmail: input.GitCommitAuthorEmail,
		GitCommitMessage:     input.GitCommitMessage,
		Provider:             input.Provider,
		PostDeployment:       input.PostDeployment,
	}

	var appBuild types.AppBuild
	if err := c.client.Post(ctx, "/app-builds/from-ci", nil, request, &appBuild); err != nil {
		return types.AppBuild{}, errors.WithStack(err)
	}

	return appBuild, nil
}

func idAsInt(id types.ID, name string) (int, error) {
	value, err := strconv.Atoi(id.String())
	if err != nil {
		return 0, errors.Wrapf(err, "invalid %s %q", name, id.String())
	}
	return value, nil
}
