package wodby1

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

type TargetProvider struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Public bool   `json:"public"`
	RevID  int    `json:"revId"`
	OrgID  int    `json:"orgId"`
}

type TargetVariableProviderFieldInput struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Variable string `json:"variable"`
	Required bool   `json:"required"`
	Secret   bool   `json:"secret"`
}

type TargetCreateVariableProviderInput struct {
	OrgID     int                                `json:"orgId"`
	ProjectID *int                               `json:"projectId,omitempty"`
	Name      string                             `json:"name"`
	Title     string                             `json:"title"`
	Fields    []TargetVariableProviderFieldInput `json:"fields"`
}

type TargetIntegrationFieldInput struct {
	Name    string  `json:"name"`
	Value   string  `json:"value"`
	EnvType *string `json:"envType,omitempty"`
}

type TargetResolveIntegrationInput struct {
	OrgID       int                           `json:"orgId"`
	ProviderID  int                           `json:"providerId"`
	Name        string                        `json:"name"`
	Title       string                        `json:"title"`
	Kinds       []string                      `json:"kinds"`
	ProjectID   *int                          `json:"projectId,omitempty"`
	FieldsInput []TargetIntegrationFieldInput `json:"fieldsInput,omitempty"`
	Scope       *string                       `json:"scope,omitempty"`
}

type TargetIntegration struct {
	ID            int     `json:"id"`
	Name          string  `json:"name"`
	Title         string  `json:"title"`
	Status        string  `json:"status"`
	Scope         *string `json:"scope,omitempty"`
	ProviderRevID int     `json:"providerRevId"`
	OrgID         int     `json:"orgId"`
}

type TargetResolveIntegrationResult struct {
	Integration *TargetIntegration `json:"integration"`
	Created     bool               `json:"created"`
}

type TargetStackServiceIntegration struct {
	ID             int    `json:"id"`
	StackServiceID int    `json:"stackServiceId"`
	Name           string `json:"name"`
	IntegrationID  int    `json:"integrationId"`
}

type TargetCreateStackServiceIntegrationInput struct {
	Name          string `json:"name"`
	IntegrationID int    `json:"integrationId"`
}

type TargetAutomationTimeWindowInput struct {
	Enabled  bool     `json:"enabled"`
	Start    string   `json:"start,omitempty"`
	End      string   `json:"end,omitempty"`
	TimeZone string   `json:"timeZone,omitempty"`
	Days     []string `json:"days,omitempty"`
}

type TargetCreateBackupPresetInput struct {
	AppServiceID  *int                             `json:"appServiceId,omitempty"`
	AppInstanceID *int                             `json:"appInstanceId,omitempty"`
	BackupName    *string                          `json:"backupName,omitempty"`
	IntegrationID int                              `json:"integrationId"`
	Bucket        string                           `json:"bucket"`
	Disabled      bool                             `json:"disabled"`
	Override      bool                             `json:"override"`
	Auto          *bool                            `json:"auto,omitempty"`
	TimeWindow    *TargetAutomationTimeWindowInput `json:"timeWindow,omitempty"`
	Duration      *int                             `json:"duration,omitempty"`
}

type TargetBackupPreset struct {
	ID            int                         `json:"id"`
	AppServiceID  *int                        `json:"appServiceId,omitempty"`
	AppInstanceID *int                        `json:"appInstanceId,omitempty"`
	BackupName    *string                     `json:"backupName,omitempty"`
	IntegrationID int                         `json:"integrationId"`
	Bucket        string                      `json:"bucket"`
	Override      bool                        `json:"override"`
	Auto          bool                        `json:"auto"`
	Disabled      bool                        `json:"disabled"`
	TimeWindow    *TargetAutomationTimeWindow `json:"timeWindow,omitempty"`
	Duration      *int                        `json:"duration,omitempty"`
}

type TargetAutomationTimeWindow struct {
	Start    string   `json:"start"`
	End      string   `json:"end"`
	TimeZone string   `json:"timeZone"`
	Days     []string `json:"days"`
}

func (c *TargetClient) GetProviderByName(ctx context.Context, name string) (TargetProvider, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return TargetProvider{}, errors.New("target provider name is required")
	}
	var item TargetProvider
	if err := c.client.Get(ctx, "/providers/by-name/"+url.PathEscape(name), nil, &item); err != nil {
		return TargetProvider{}, errors.Wrapf(err, "resolve target Wodby 2 provider %q", name)
	}
	if item.ID <= 0 || item.RevID <= 0 || item.Name != name {
		return TargetProvider{}, errors.Errorf("target Wodby 2 provider %q returned an invalid immutable identity", name)
	}
	return item, nil
}

func (c *TargetClient) CreateVariableProvider(ctx context.Context, input TargetCreateVariableProviderInput) (TargetProvider, error) {
	if input.OrgID <= 0 || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Title) == "" || len(input.Fields) == 0 {
		return TargetProvider{}, errors.New("target variable provider requires an organization, name, title, and fields")
	}
	var item TargetProvider
	if err := c.client.Post(ctx, "/providers/actions/create-variable", nil, input, &item); err != nil {
		return TargetProvider{}, errors.Wrap(err, "create target Wodby 2 variable provider")
	}
	if item.ID <= 0 || item.RevID <= 0 || item.Name != input.Name || item.OrgID != input.OrgID || item.Public {
		return TargetProvider{}, errors.New("target Wodby 2 created an unexpected variable provider")
	}
	return item, nil
}

func (c *TargetClient) ResolveIntegration(ctx context.Context, input TargetResolveIntegrationInput) (TargetResolveIntegrationResult, error) {
	if input.OrgID <= 0 || input.ProviderID <= 0 || strings.TrimSpace(input.Name) == "" || len(input.Kinds) == 0 {
		return TargetResolveIntegrationResult{}, errors.New("target integration requires an organization, provider, name, and kind")
	}
	var result TargetResolveIntegrationResult
	if err := c.client.Post(ctx, "/integrations/actions/resolve", nil, input, &result); err != nil {
		return TargetResolveIntegrationResult{}, errors.Wrap(err, "resolve target Wodby 2 integration")
	}
	if result.Integration == nil || result.Integration.ID <= 0 || result.Integration.OrgID != input.OrgID {
		return TargetResolveIntegrationResult{}, errors.New("target Wodby 2 integration resolution returned an invalid result")
	}
	return result, nil
}

func (c *TargetClient) GetIntegration(ctx context.Context, id int) (TargetIntegration, error) {
	if id <= 0 {
		return TargetIntegration{}, errors.New("target integration ID must be positive")
	}
	var item TargetIntegration
	if err := c.client.Get(ctx, "/integrations/"+strconv.Itoa(id), nil, &item); err != nil {
		return TargetIntegration{}, errors.Wrap(err, "get target Wodby 2 integration")
	}
	if item.ID != id || item.OrgID <= 0 || item.ProviderRevID <= 0 {
		return TargetIntegration{}, errors.New("target Wodby 2 returned an invalid integration")
	}
	return item, nil
}

func (c *TargetClient) ListStackServiceIntegrations(ctx context.Context, stackServiceID int) ([]TargetStackServiceIntegration, error) {
	if stackServiceID <= 0 {
		return nil, errors.New("target stack service ID must be positive")
	}
	items := []TargetStackServiceIntegration{}
	if err := c.client.Get(ctx, "/stack-services/"+strconv.Itoa(stackServiceID)+"/integrations", nil, &items); err != nil {
		return nil, errors.Wrap(err, "list target stack service integrations")
	}
	for _, item := range items {
		if item.ID <= 0 || item.StackServiceID != stackServiceID || strings.TrimSpace(item.Name) == "" || item.IntegrationID <= 0 {
			return nil, errors.New("target Wodby 2 stack service returned an invalid integration link")
		}
	}
	return items, nil
}

func (c *TargetClient) CreateStackServiceIntegration(ctx context.Context, stackServiceID int, input TargetCreateStackServiceIntegrationInput) (TargetStackServiceIntegration, error) {
	if stackServiceID <= 0 || strings.TrimSpace(input.Name) == "" || input.IntegrationID <= 0 {
		return TargetStackServiceIntegration{}, errors.New("target stack service integration requires a service, name, and integration")
	}
	var item TargetStackServiceIntegration
	if err := c.client.Post(ctx, "/stack-services/"+strconv.Itoa(stackServiceID)+"/integrations", nil, input, &item); err != nil {
		return TargetStackServiceIntegration{}, errors.Wrap(err, "create target stack service integration")
	}
	if item.ID <= 0 || item.StackServiceID != stackServiceID || item.Name != input.Name || item.IntegrationID != input.IntegrationID {
		return TargetStackServiceIntegration{}, errors.New("target Wodby 2 created an unexpected stack service integration")
	}
	return item, nil
}

func (c *TargetClient) ListBackupPresets(ctx context.Context, appServiceID int, backupName string) ([]TargetBackupPreset, error) {
	if appServiceID <= 0 || strings.TrimSpace(backupName) == "" {
		return nil, errors.New("target backup preset lookup requires an app service and backup name")
	}
	query := url.Values{"appServiceId": []string{strconv.Itoa(appServiceID)}, "backupName": []string{backupName}}
	items := []TargetBackupPreset{}
	if err := c.client.Get(ctx, "/backup-presets", query, &items); err != nil {
		return nil, errors.Wrap(err, "list target Wodby 2 backup presets")
	}
	return items, nil
}

func (c *TargetClient) CreateBackupPreset(ctx context.Context, input TargetCreateBackupPresetInput) (TargetBackupPreset, error) {
	if input.AppServiceID == nil || *input.AppServiceID <= 0 || input.BackupName == nil || strings.TrimSpace(*input.BackupName) == "" || input.IntegrationID < 0 {
		return TargetBackupPreset{}, errors.New("target backup preset requires an app service, backup name, and valid integration")
	}
	var item TargetBackupPreset
	if err := c.client.Post(ctx, "/backup-presets", nil, input, &item); err != nil {
		return TargetBackupPreset{}, errors.Wrap(err, "create target Wodby 2 backup preset")
	}
	if item.ID <= 0 || item.AppServiceID == nil || *item.AppServiceID != *input.AppServiceID || item.BackupName == nil || *item.BackupName != *input.BackupName {
		return TargetBackupPreset{}, errors.New("target Wodby 2 created an unexpected backup preset")
	}
	return item, nil
}

func (c *TargetClient) GetBackupPreset(ctx context.Context, id int) (TargetBackupPreset, error) {
	if id <= 0 {
		return TargetBackupPreset{}, errors.New("target backup preset ID must be positive")
	}
	var item TargetBackupPreset
	if err := c.client.Get(ctx, "/backup-presets/"+strconv.Itoa(id), nil, &item); err != nil {
		return TargetBackupPreset{}, errors.Wrap(err, "get target Wodby 2 backup preset")
	}
	if item.ID != id || item.AppServiceID == nil || *item.AppServiceID <= 0 || item.BackupName == nil || strings.TrimSpace(*item.BackupName) == "" {
		return TargetBackupPreset{}, errors.New("target Wodby 2 returned an invalid backup preset")
	}
	return item, nil
}
