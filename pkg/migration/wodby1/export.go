package wodby1

import (
	"bytes"
	"crypto/hmac"
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
	ConfigMAC       string        `json:"-"`

	// App and Instances are the Wodby 1 migration/v1 app export shape.
	App       *App       `json:"app,omitempty"`
	Instances []Instance `json:"instances,omitempty"`
}

func (e Export) ContentDigest() (string, error) {
	// generated_at is transport metadata rather than source state. Excluding it
	// makes identical exports comparable across repeated requests. Backup URLs
	// are also transport credentials: the source may refresh them even when the
	// selected backup has not changed, and they must never influence a persisted
	// plan or state identity.
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
	scrubBackupURLs(&canonical)
	scrubPublicDigestValues(&canonical)
	canonicalizeExport(&canonical)
	data, err = json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

// ConfigDigest identifies the source application configuration independently
// from backup selection and the write-freeze flag. A migration can therefore
// prepare infrastructure, let the customer enable maintenance mode and create
// a fresh backup, then safely resume data synchronization without accepting
// unrelated source drift.
func (e Export) ConfigDigest() (string, error) {
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
	normalizeConfigDigestExport(&canonical)
	scrubPublicDigestValues(&canonical)
	canonicalizeExport(&canonical)
	data, err = json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

// AuthenticatedConfigDigest binds the full source configuration, including
// protected values, without persisting an offline password oracle. The Wodby 1
// API token is already required for every phase and is never written to the
// plan or state file.
func (e Export) AuthenticatedConfigDigest(key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("authenticated configuration digest key is required")
	}
	e.GeneratedAt = 0
	e.Digest = ""
	e.ResponseDigest = ""
	e.ConfigMAC = ""
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
	normalizeConfigDigestExport(&canonical)
	canonicalizeExport(&canonical)
	data, err = json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte("wodby1-migration-config/v1\x00"))
	_, _ = mac.Write(data)
	return fmt.Sprintf("%x", mac.Sum(nil)), nil
}

// MigrationConfigDigest returns the token-authenticated fingerprint populated
// by SourceClient. In-memory/unit-test exports fall back to the public digest,
// which deliberately excludes protected and free-form values.
func (e Export) MigrationConfigDigest() (string, error) {
	if e.ConfigMAC != "" {
		if len(e.ConfigMAC) != sha256.Size*2 {
			return "", fmt.Errorf("authenticated configuration digest is invalid")
		}
		for _, char := range e.ConfigMAC {
			if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
				return "", fmt.Errorf("authenticated configuration digest is invalid")
			}
		}
		return e.ConfigMAC, nil
	}
	return e.ConfigDigest()
}

// BackupDigest identifies the selected backup files without hashing their
// backup download URLs. It is suitable for recording which immutable data
// snapshot was imported while keeping URL credentials out of migration state.
func (e Export) BackupDigest() (string, error) {
	type digestInstance struct {
		AppUUID      string   `json:"appUuid"`
		InstanceUUID string   `json:"instanceUuid"`
		Backups      []Backup `json:"backups"`
	}
	payload := struct {
		Schema string           `json:"schema"`
		Source *ExportSource    `json:"source,omitempty"`
		Items  []digestInstance `json:"items"`
	}{
		Schema: e.Schema,
		Source: e.Source,
		Items:  []digestInstance{},
	}
	for _, appExport := range e.AppExports() {
		for _, instance := range appExport.Instances {
			backups := append([]Backup(nil), instance.Backups...)
			for i := range backups {
				backups[i].URL = ""
				backups[i].MirroredURL = ""
			}
			sort.SliceStable(backups, func(i, j int) bool {
				return canonicalJSON(backups[i]) < canonicalJSON(backups[j])
			})
			payload.Items = append(payload.Items, digestInstance{
				AppUUID:      appExport.App.UUID,
				InstanceUUID: instance.UUID,
				Backups:      backups,
			})
		}
	}
	sort.SliceStable(payload.Items, func(i, j int) bool {
		return canonicalJSON(payload.Items[i]) < canonicalJSON(payload.Items[j])
	})
	data, err := json.Marshal(payload)
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
	Type         string `json:"type,omitempty"`
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
	Version         string                 `json:"version,omitempty"`
	Enabled         bool                   `json:"enabled"`
	Configuration   map[string]interface{} `json:"configuration,omitempty"`
	EnvVars         []EnvVar               `json:"env_vars,omitempty"`
	CronJobs        []CronJob              `json:"cron_jobs,omitempty"`
	SecretsRedacted []string               `json:"secrets_redacted,omitempty"`
}

func (s *Service) UnmarshalJSON(data []byte) error {
	type serviceAlias Service
	alias := serviceAlias{}
	wire := struct {
		*serviceAlias
		Configuration json.RawMessage `json:"configuration"`
	}{
		serviceAlias: &alias,
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&wire); err != nil {
		return err
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return err
	}

	configuration, err := decodeServiceConfiguration(wire.Configuration)
	if err != nil {
		return err
	}

	*s = Service(alias)
	s.Configuration = configuration
	return nil
}

func decodeServiceConfiguration(data json.RawMessage) (map[string]interface{}, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil, nil
	}

	if data[0] == '[' {
		var legacy []json.RawMessage
		if err := json.Unmarshal(data, &legacy); err != nil {
			return nil, err
		}
		if len(legacy) != 0 {
			return nil, fmt.Errorf("service configuration must be a JSON object; the legacy array form is allowed only when empty")
		}
		return nil, nil
	}
	if data[0] != '{' {
		return nil, fmt.Errorf("service configuration must be a JSON object")
	}

	var configuration map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&configuration); err != nil {
		return nil, err
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return nil, err
	}
	return configuration, nil
}

type EnvVar struct {
	Name           string   `json:"name"`
	Value          string   `json:"value,omitempty"`
	Secret         bool     `json:"secret,omitempty"`
	Enabled        bool     `json:"enabled"`
	Protected      bool     `json:"protected,omitempty"`
	Origin         string   `json:"origin,omitempty"`
	OverrideFields []string `json:"override_fields,omitempty"`
	Redacted       *bool    `json:"redacted,omitempty"`
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
	MirroredURL   string `json:"mirrored_url,omitempty"`
	Status        string `json:"status,omitempty"`
	Size          int64  `json:"size,omitempty"`
	Created       int64  `json:"created,omitempty"`
	Updated       int64  `json:"updated,omitempty"`
	BackupUUID    string `json:"backup_uuid,omitempty"`
	BackupCreated int64  `json:"backup_created,omitempty"`
	BackupUpdated int64  `json:"backup_updated,omitempty"`
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
		if e.Source.Kind != "app" && e.Source.Kind != "instance" && e.Source.Kind != "server" {
			return fmt.Errorf("Wodby 1 migration/v2 export has unsupported source kind %q", e.Source.Kind)
		}
		if e.Source.UUID == "" {
			return fmt.Errorf("Wodby 1 migration/v2 export source UUID is required")
		}
		if e.Source.Kind == "app" {
			if len(e.Apps) != 1 || e.Apps[0].App.UUID != e.Source.UUID {
				return fmt.Errorf("Wodby 1 migration/v2 app export must contain exactly its requested source app")
			}
			if len(e.Apps[0].Instances) == 0 {
				return fmt.Errorf("Wodby 1 migration/v2 app export must contain at least one source instance")
			}
		}
		if e.Source.Kind == "instance" {
			if len(e.Apps) != 1 || len(e.Apps[0].Instances) != 1 ||
				e.Apps[0].Instances[0].UUID != e.Source.UUID {
				return fmt.Errorf("Wodby 1 migration/v2 instance export must contain exactly its requested source instance")
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
		if len(appExport.Instances) == 0 {
			return fmt.Errorf("%s must contain at least one source instance", appPath)
		}

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

func scrubBackupURLs(export *Export) {
	for appIndex := range export.Apps {
		for instanceIndex := range export.Apps[appIndex].Instances {
			for backupIndex := range export.Apps[appIndex].Instances[instanceIndex].Backups {
				export.Apps[appIndex].Instances[instanceIndex].Backups[backupIndex].URL = ""
				export.Apps[appIndex].Instances[instanceIndex].Backups[backupIndex].MirroredURL = ""
			}
		}
	}
	for instanceIndex := range export.Instances {
		for backupIndex := range export.Instances[instanceIndex].Backups {
			export.Instances[instanceIndex].Backups[backupIndex].URL = ""
			export.Instances[instanceIndex].Backups[backupIndex].MirroredURL = ""
		}
	}
}

func scrubPublicDigestValues(export *Export) {
	for issueIndex := range export.Issues {
		export.Issues[issueIndex].Details = nil
	}
	if export.App != nil {
		scrubPublicAppValues(export.App)
	}
	for appIndex := range export.Apps {
		app := &export.Apps[appIndex]
		scrubPublicAppValues(&app.App)
		for instanceIndex := range app.Instances {
			scrubPublicInstanceValues(&app.Instances[instanceIndex])
		}
	}
	for instanceIndex := range export.Instances {
		scrubPublicInstanceValues(&export.Instances[instanceIndex])
	}
}

func scrubPublicAppValues(app *App) {
	if app != nil && app.Repository != nil {
		app.Repository.URL = ""
	}
}

func scrubPublicInstanceValues(instance *Instance) {
	if instance.BasicAuth != nil {
		instance.BasicAuth.Password = ""
	}
	for serviceIndex := range instance.Services {
		service := &instance.Services[serviceIndex]
		for name := range service.Configuration {
			service.Configuration[name] = nil
		}
		for envIndex := range service.EnvVars {
			service.EnvVars[envIndex].Value = ""
		}
		for cronIndex := range service.CronJobs {
			cron := &service.CronJobs[cronIndex]
			cron.Title = ""
			cron.Command = ""
			cron.Source = ""
		}
	}
}

func normalizeConfigDigestExport(export *Export) {
	export.GeneratedAt = 0
	export.Digest = ""
	export.ResponseDigest = ""
	export.Issues = filterConfigDigestIssues(export.Issues)
	if export.App != nil {
		export.App.Updated = 0
		if export.App.Repository != nil {
			export.App.Repository.Updated = 0
		}
	}
	for appIndex := range export.Apps {
		app := &export.Apps[appIndex]
		app.App.Updated = 0
		if app.App.Repository != nil {
			app.App.Repository.Updated = 0
		}
		for instanceIndex := range app.Instances {
			normalizeConfigDigestInstance(&app.Instances[instanceIndex])
		}
	}
	for instanceIndex := range export.Instances {
		normalizeConfigDigestInstance(&export.Instances[instanceIndex])
	}
}

func normalizeConfigDigestInstance(instance *Instance) {
	instance.Updated = 0
	instance.Backups = nil
	if instance.Properties != nil {
		delete(instance.Properties, "maintenance_mode")
	}
}

func filterConfigDigestIssues(issues []ExportIssue) []ExportIssue {
	result := make([]ExportIssue, 0, len(issues))
	for _, issue := range issues {
		// Backup freshness and availability are validated by sync-data against
		// BackupDigest. They must not invalidate already prepared target
		// infrastructure.
		if strings.HasPrefix(issue.Code, "backup.") {
			continue
		}
		result = append(result, issue)
	}
	return result
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
		for envIndex := range service.EnvVars {
			sort.Strings(service.EnvVars[envIndex].OverrideFields)
		}
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
