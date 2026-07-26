package wodby1

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

const (
	ExportSchemaV1 = "wodby1-migration/v1"
	ExportSchemaV2 = "wodby1-migration/v2"

	// ExportSchema is retained as the legacy schema alias for callers that build
	// the original app-shaped export in memory.
	ExportSchema = ExportSchemaV1
)

type Export struct {
	Schema          string        `json:"schema"`
	GeneratedAt     int64         `json:"generated_at,omitempty"`
	Source          *ExportSource `json:"source,omitempty"`
	SecretsIncluded bool          `json:"secrets_included,omitempty"`
	Apps            []AppExport   `json:"apps"`
	Issues          []ExportIssue `json:"issues,omitempty"`
	Digest          string        `json:"-"`
	ResponseDigest  string        `json:"-"`

	// App and Instances are the Wodby 1 migration/v1 app export shape.
	App       *App       `json:"app,omitempty"`
	Instances []Instance `json:"instances,omitempty"`
}

func (e Export) ContentDigest() (string, error) {
	// generated_at is transport metadata rather than source state. Excluding it
	// makes identical exports comparable across repeated read-only requests.
	e.GeneratedAt = 0
	e.Digest = ""
	e.ResponseDigest = ""
	data, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	var canonical Export
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&canonical); err != nil {
		return "", err
	}
	canonicalizeExport(&canonical)
	data, err = json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

type ExportSource struct {
	Kind string `json:"kind"`
	UUID string `json:"uuid"`
}

type ExportIssue struct {
	Code     string                 `json:"code,omitempty"`
	Severity string                 `json:"severity,omitempty"`
	Path     string                 `json:"path,omitempty"`
	Message  string                 `json:"message,omitempty"`
	Details  map[string]interface{} `json:"details,omitempty"`
}

type AppExport struct {
	App       App        `json:"app"`
	Instances []Instance `json:"instances"`
}

type App struct {
	UUID       string      `json:"uuid"`
	Name       string      `json:"name"`
	Title      string      `json:"title"`
	Type       string      `json:"type"`
	Status     string      `json:"status,omitempty"`
	Created    int64       `json:"created,omitempty"`
	Updated    int64       `json:"updated,omitempty"`
	Repository *Repository `json:"repository,omitempty"`
}

type Repository struct {
	UUID    string `json:"uuid"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Status  string `json:"status,omitempty"`
	Created int64  `json:"created,omitempty"`
	Updated int64  `json:"updated,omitempty"`
}

type Instance struct {
	UUID       string                 `json:"uuid"`
	Name       string                 `json:"name"`
	Title      string                 `json:"title"`
	Type       string                 `json:"type"`
	Status     string                 `json:"status,omitempty"`
	Updated    int64                  `json:"updated,omitempty"`
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
	UUID         string `json:"uuid"`
	Name         string `json:"name"`
	Version      string `json:"version,omitempty"`
	Revision     int    `json:"revision,omitempty"`
	Custom       bool   `json:"custom"`
	AncestorUUID string `json:"ancestor_uuid,omitempty"`
	AncestorName string `json:"ancestor_name,omitempty"`
}

type BasicAuth struct {
	Enabled          bool   `json:"enabled"`
	Login            string `json:"login,omitempty"`
	Password         string `json:"password,omitempty"`
	Secret           bool   `json:"secret,omitempty"`
	PasswordRedacted *bool  `json:"password_redacted,omitempty"`
}

type Domain struct {
	UUID            string `json:"uuid,omitempty"`
	Name            string `json:"name"`
	Type            string `json:"type,omitempty"`
	Status          string `json:"status,omitempty"`
	Enabled         *bool  `json:"enabled,omitempty"`
	Primary         bool   `json:"primary,omitempty"`
	Indexed         *bool  `json:"indexed,omitempty"`
	SSL             bool   `json:"ssl,omitempty"`
	SSLRequired     *bool  `json:"ssl_required,omitempty"`
	SSLCustom       bool   `json:"ssl_custom,omitempty"`
	HSTS            bool   `json:"hsts,omitempty"`
	HSTSSubdomains  bool   `json:"hsts_subdomains,omitempty"`
	Protected       bool   `json:"protected,omitempty"`
	Service         string `json:"service,omitempty"`
	ServiceProtocol string `json:"service_protocol,omitempty"`
	PortNumber      *int   `json:"port_number,omitempty"`
	RedirectToWWW   bool   `json:"redirect_to_www,omitempty"`
	RedirectNonWWW  bool   `json:"redirect_non_www,omitempty"`
	RedirectTarget  string `json:"redirect_target,omitempty"`
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
	Redacted  *bool  `json:"redacted,omitempty"`
}

type CronJob struct {
	Title          string `json:"title,omitempty"`
	Crontab        string `json:"crontab"`
	Command        string `json:"command"`
	Enabled        bool   `json:"enabled"`
	Source         string `json:"source,omitempty"`
	SourceLine     int    `json:"source_line,omitempty"`
	Classification string `json:"classification,omitempty"`
}

type Backup struct {
	UUID          string `json:"uuid,omitempty"`
	Component     string `json:"component,omitempty"`
	URL           string `json:"url,omitempty"`
	Status        string `json:"status,omitempty"`
	Size          int64  `json:"size,omitempty"`
	Created       int64  `json:"created,omitempty"`
	Updated       int64  `json:"updated,omitempty"`
	BackupUUID    string `json:"backup_uuid,omitempty"`
	BackupCreated int64  `json:"backup_created,omitempty"`
}

func DecodeExport(data []byte) (Export, error) {
	var export Export
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&export); err != nil {
		return Export{}, err
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return Export{}, err
	}
	if err := export.Validate(); err != nil {
		return Export{}, err
	}
	return export, nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("migration export contains trailing JSON data")
		}
		return err
	}
	return nil
}

func (e Export) Validate() error {
	if e.Schema != ExportSchemaV1 && e.Schema != ExportSchemaV2 {
		return fmt.Errorf("unsupported Wodby 1 migration export schema %q", e.Schema)
	}
	if e.Schema == ExportSchemaV2 && e.Apps == nil {
		return fmt.Errorf("Wodby 1 migration/v2 export must contain apps")
	}
	if e.Schema == ExportSchemaV2 {
		if e.Source == nil {
			return fmt.Errorf("Wodby 1 migration/v2 export must identify its source")
		}
		if e.Source.Kind != "app" && e.Source.Kind != "server" {
			return fmt.Errorf("Wodby 1 migration/v2 export has unsupported source kind %q", e.Source.Kind)
		}
		if e.Source.UUID == "" {
			return fmt.Errorf("Wodby 1 migration/v2 export source UUID is required")
		}
		if e.Source.Kind == "app" {
			if len(e.Apps) != 1 || e.Apps[0].App.UUID != e.Source.UUID {
				return fmt.Errorf("Wodby 1 migration/v2 app export must contain exactly its requested source app")
			}
		}
		if err := validateV2Identities(e.Apps); err != nil {
			return err
		}
	}
	if e.Schema == ExportSchemaV1 && e.App == nil && e.Apps == nil {
		return fmt.Errorf("migration export must contain app or apps")
	}
	return nil
}

func (e Export) ValidateSource(kind string, uuid string) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if e.Schema != ExportSchemaV2 {
		return fmt.Errorf(
			"Wodby 1 migration source validation requires schema %q, got %q",
			ExportSchemaV2,
			e.Schema,
		)
	}
	if e.Source.Kind != kind || e.Source.UUID != uuid {
		return fmt.Errorf(
			"Wodby 1 migration export source %s %q does not match requested source %s %q",
			e.Source.Kind,
			e.Source.UUID,
			kind,
			uuid,
		)
	}
	return nil
}

func (e Export) AppExports() []AppExport {
	if e.Apps != nil {
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

func (b BasicAuth) IsPasswordRedacted() bool {
	if b.PasswordRedacted != nil {
		return *b.PasswordRedacted
	}
	return b.Secret && b.Password == ""
}

func (v EnvVar) IsRedacted() bool {
	if v.Redacted != nil {
		return *v.Redacted
	}
	return (v.Secret || v.Protected) && v.Value == ""
}

func validateV2Identities(apps []AppExport) error {
	appUUIDs := map[string]bool{}
	instanceUUIDs := map[string]bool{}
	domainUUIDs := map[string]bool{}
	for appIndex, appExport := range apps {
		appPath := fmt.Sprintf("apps[%d]", appIndex)
		if strings.TrimSpace(appExport.App.UUID) == "" {
			return fmt.Errorf("%s app UUID is required", appPath)
		}
		if strings.TrimSpace(appExport.App.Name) == "" {
			return fmt.Errorf("%s app name is required", appPath)
		}
		if appUUIDs[appExport.App.UUID] {
			return fmt.Errorf("%s duplicates app UUID %q", appPath, appExport.App.UUID)
		}
		appUUIDs[appExport.App.UUID] = true

		instanceNames := map[string]bool{}
		for instanceIndex, instance := range appExport.Instances {
			instancePath := fmt.Sprintf("%s.instances[%d]", appPath, instanceIndex)
			if strings.TrimSpace(instance.UUID) == "" {
				return fmt.Errorf("%s instance UUID is required", instancePath)
			}
			if strings.TrimSpace(instance.Name) == "" {
				return fmt.Errorf("%s instance name is required", instancePath)
			}
			if strings.TrimSpace(instance.Type) == "" {
				return fmt.Errorf("%s instance type is required", instancePath)
			}
			if strings.TrimSpace(instance.Stack.Name) == "" {
				return fmt.Errorf("%s stack name is required", instancePath)
			}
			if instanceUUIDs[instance.UUID] {
				return fmt.Errorf("%s duplicates instance UUID %q", instancePath, instance.UUID)
			}
			instanceUUIDs[instance.UUID] = true
			if instanceNames[instance.Name] {
				return fmt.Errorf("%s duplicates instance name %q within app %q", instancePath, instance.Name, appExport.App.UUID)
			}
			instanceNames[instance.Name] = true

			serviceNames := map[string]bool{}
			for serviceIndex, service := range instance.Services {
				servicePath := fmt.Sprintf("%s.services[%d]", instancePath, serviceIndex)
				if strings.TrimSpace(service.Name) == "" {
					return fmt.Errorf("%s service name is required", servicePath)
				}
				if serviceNames[service.Name] {
					return fmt.Errorf("%s duplicates service name %q", servicePath, service.Name)
				}
				serviceNames[service.Name] = true
			}

			for domainIndex, domain := range instance.Domains {
				domainPath := fmt.Sprintf("%s.domains[%d]", instancePath, domainIndex)
				if strings.TrimSpace(domain.UUID) == "" {
					return fmt.Errorf("%s domain UUID is required", domainPath)
				}
				if domainUUIDs[domain.UUID] {
					return fmt.Errorf("%s duplicates domain UUID %q", domainPath, domain.UUID)
				}
				domainUUIDs[domain.UUID] = true
			}
		}
	}
	return nil
}

func canonicalizeExport(export *Export) {
	for i := range export.Apps {
		canonicalizeAppExport(&export.Apps[i])
	}
	sort.SliceStable(export.Apps, func(i, j int) bool {
		return canonicalJSON(export.Apps[i]) < canonicalJSON(export.Apps[j])
	})
	for i := range export.Instances {
		canonicalizeInstance(&export.Instances[i])
	}
	sort.SliceStable(export.Instances, func(i, j int) bool {
		return canonicalJSON(export.Instances[i]) < canonicalJSON(export.Instances[j])
	})
	sort.SliceStable(export.Issues, func(i, j int) bool {
		return canonicalJSON(export.Issues[i]) < canonicalJSON(export.Issues[j])
	})
}

func canonicalizeAppExport(appExport *AppExport) {
	for i := range appExport.Instances {
		canonicalizeInstance(&appExport.Instances[i])
	}
	sort.SliceStable(appExport.Instances, func(i, j int) bool {
		return canonicalJSON(appExport.Instances[i]) < canonicalJSON(appExport.Instances[j])
	})
}

func canonicalizeInstance(instance *Instance) {
	for i := range instance.Services {
		service := &instance.Services[i]
		sort.SliceStable(service.EnvVars, func(i, j int) bool {
			return canonicalJSON(service.EnvVars[i]) < canonicalJSON(service.EnvVars[j])
		})
		sort.SliceStable(service.CronJobs, func(i, j int) bool {
			return canonicalJSON(service.CronJobs[i]) < canonicalJSON(service.CronJobs[j])
		})
		sort.Strings(service.SecretsRedacted)
	}
	sort.SliceStable(instance.Services, func(i, j int) bool {
		return canonicalJSON(instance.Services[i]) < canonicalJSON(instance.Services[j])
	})
	sort.SliceStable(instance.Domains, func(i, j int) bool {
		return canonicalJSON(instance.Domains[i]) < canonicalJSON(instance.Domains[j])
	})
	sort.SliceStable(instance.Backups, func(i, j int) bool {
		return canonicalJSON(instance.Backups[i]) < canonicalJSON(instance.Backups[j])
	})
}
