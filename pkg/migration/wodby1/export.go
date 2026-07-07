package wodby1

import (
	"encoding/json"
	"fmt"
)

const ExportSchema = "wodby1-migration/v1"

type Export struct {
	Schema    string      `json:"schema"`
	App       *App        `json:"app,omitempty"`
	Apps      []AppExport `json:"apps,omitempty"`
	Instances []Instance  `json:"instances,omitempty"`
}

type AppExport struct {
	App       App        `json:"app"`
	Instances []Instance `json:"instances"`
}

type App struct {
	UUID  string `json:"uuid"`
	Name  string `json:"name"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

type Instance struct {
	UUID       string                 `json:"uuid"`
	Name       string                 `json:"name"`
	Title      string                 `json:"title"`
	Type       string                 `json:"type"`
	Server     *Server                `json:"server,omitempty"`
	Stack      Stack                  `json:"stack"`
	Properties map[string]interface{} `json:"properties,omitempty"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
	BasicAuth  *BasicAuth             `json:"basic_auth,omitempty"`
	Domains    []Domain               `json:"domains,omitempty"`
	Services   []Service              `json:"services,omitempty"`
	Backups    []Backup               `json:"backups,omitempty"`
}

type Server struct {
	UUID    string `json:"uuid"`
	Title   string `json:"title"`
	Version string `json:"version,omitempty"`
}

type Stack struct {
	UUID     string `json:"uuid"`
	Name     string `json:"name"`
	Version  string `json:"version,omitempty"`
	Revision int    `json:"revision,omitempty"`
	Custom   bool   `json:"custom"`
}

type BasicAuth struct {
	Enabled  bool   `json:"enabled"`
	Login    string `json:"login,omitempty"`
	Password string `json:"password,omitempty"`
	Secret   bool   `json:"secret,omitempty"`
}

type Domain struct {
	Name           string `json:"name"`
	Primary        bool   `json:"primary,omitempty"`
	Indexed        *bool  `json:"indexed,omitempty"`
	SSL            bool   `json:"ssl,omitempty"`
	SSLRequired    *bool  `json:"ssl_required,omitempty"`
	Protected      bool   `json:"protected,omitempty"`
	Service        string `json:"service,omitempty"`
	PortNumber     *int   `json:"port_number,omitempty"`
	RedirectToWWW  bool   `json:"redirect_to_www,omitempty"`
	RedirectNonWWW bool   `json:"redirect_non_www,omitempty"`
	RedirectTarget string `json:"redirect_target,omitempty"`
}

type Service struct {
	Name            string                 `json:"name"`
	Enabled         bool                   `json:"enabled"`
	Configuration   map[string]interface{} `json:"configuration,omitempty"`
	EnvVars         []EnvVar               `json:"env_vars,omitempty"`
	CronJobs        []CronJob              `json:"cron_jobs,omitempty"`
	SecretsRedacted []string               `json:"secrets_redacted,omitempty"`
}

type EnvVar struct {
	Name      string `json:"name"`
	Value     string `json:"value,omitempty"`
	Secret    bool   `json:"secret,omitempty"`
	Enabled   bool   `json:"enabled"`
	Protected bool   `json:"protected,omitempty"`
	Origin    string `json:"origin,omitempty"`
}

type CronJob struct {
	Title   string `json:"title,omitempty"`
	Crontab string `json:"crontab"`
	Command string `json:"command"`
	Enabled bool   `json:"enabled"`
	Source  string `json:"source,omitempty"`
}

type Backup struct {
	UUID      string `json:"uuid,omitempty"`
	Component string `json:"component,omitempty"`
	URL       string `json:"url,omitempty"`
	Status    string `json:"status,omitempty"`
}

func DecodeExport(data []byte) (Export, error) {
	var export Export
	if err := json.Unmarshal(data, &export); err != nil {
		return Export{}, err
	}
	if err := export.Validate(); err != nil {
		return Export{}, err
	}
	return export, nil
}

func (e Export) Validate() error {
	if e.Schema != ExportSchema {
		return fmt.Errorf("unsupported Wodby 1 migration export schema %q", e.Schema)
	}
	if e.App == nil && len(e.Apps) == 0 {
		return fmt.Errorf("migration export must contain app or apps")
	}
	return nil
}

func (e Export) AppExports() []AppExport {
	if len(e.Apps) != 0 {
		return e.Apps
	}
	if e.App == nil {
		return nil
	}
	return []AppExport{{
		App:       *e.App,
		Instances: e.Instances,
	}}
}
