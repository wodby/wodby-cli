package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/wodby/wodby-cli/pkg/api/rest"
	"github.com/wodby/wodby-cli/pkg/types"
)

const (
	outputTable    = "table"
	outputVertical = "vertical"
	outputJSON     = "json"
)

type outputOptions struct {
	output string
}

type waitOptions struct {
	wait    bool
	timeout time.Duration
}

type bodyOptions struct {
	data string
	file string
}

type relationColumn struct {
	title             string
	objectKey         string
	idTitle           string
	idPaths           []string
	idScalarPaths     []string
	pathPrefix        string
	pathPrefixes      []string
	titlePaths        []string
	allowNumericTitle bool
}

var relationColumns = map[string]relationColumn{
	"app": {
		title:         "app",
		objectKey:     "app",
		idTitle:       "app id",
		idPaths:       []string{"appId", "app.id", "appInstance.appId", "appInstance.app.id", "instance.appId", "instance.app.id", "origin.appId", "origin.app.id"},
		idScalarPaths: []string{"app", "appInstance.app", "instance.app", "origin.app"},
		pathPrefix:    "/apps/",
		titlePaths:    []string{"appTitle", "app.title", "app", "appName", "app.name", "appInstance.appTitle", "appInstance.app.title", "appInstance.app.name", "instance.appTitle", "instance.app.title", "instance.app.name", "origin.appTitle", "origin.app.title", "origin.app.name"},
	},
	"env": {
		title:         "env",
		objectKey:     "env",
		idTitle:       "env id",
		idPaths:       []string{"envId", "env.id", "environmentId", "environment.id"},
		idScalarPaths: []string{"env", "environment"},
		pathPrefix:    "/envs/",
		titlePaths:    []string{"envTitle", "env.title", "environmentTitle", "environment.title", "env", "environment", "envName", "env.name", "environment.name"},
	},
	"cluster": {
		title:         "cluster",
		objectKey:     "cluster",
		idTitle:       "cluster id",
		idPaths:       []string{"clusterId", "cluster.id"},
		idScalarPaths: []string{"cluster"},
		pathPrefix:    "/clusters/",
		titlePaths:    []string{"clusterTitle", "cluster.title", "cluster", "clusterName", "cluster.name"},
	},
	"stack": {
		title:         "stack",
		objectKey:     "stack",
		idTitle:       "stack id",
		idPaths:       []string{"stackId", "stack.id", "stackRev.stackId", "stackRev.stack.id", "stackRevision.stackId", "stackRevision.stack.id", "app.stackId", "app.stack.id", "app.stackRev.stackId", "app.stackRev.stack.id", "app.stackRevision.stackId", "app.stackRevision.stack.id", "appStackId", "appStack.id", "appInstance.stackId", "appInstance.stack.id", "appInstance.app.stackId", "appInstance.app.stack.id", "appInstance.app.stackRev.stackId", "appInstance.app.stackRev.stack.id", "appInstance.app.stackRevision.stackId", "appInstance.app.stackRevision.stack.id", "instance.stackId", "instance.stack.id", "instance.app.stackId", "instance.app.stack.id", "instance.app.stackRev.stackId", "instance.app.stackRev.stack.id", "instance.app.stackRevision.stackId", "instance.app.stackRevision.stack.id"},
		idScalarPaths: []string{"stack", "stackRev.stack", "stackRevision.stack", "app.stack", "app.stackRev.stack", "app.stackRevision.stack", "appInstance.stack", "appInstance.app.stack", "appInstance.app.stackRev.stack", "appInstance.app.stackRevision.stack", "instance.stack", "instance.app.stack", "instance.app.stackRev.stack", "instance.app.stackRevision.stack"},
		pathPrefix:    "/stacks/",
		titlePaths:    []string{"stackTitle", "stack.title", "stack", "stackName", "stack.name", "stackRev.stackTitle", "stackRev.stack.title", "stackRev.stack.name", "stackRevision.stackTitle", "stackRevision.stack.title", "stackRevision.stack.name", "app.stackTitle", "app.stack.title", "app.stack.name", "app.stackRev.stackTitle", "app.stackRev.stack.title", "app.stackRev.stack.name", "app.stackRevision.stackTitle", "app.stackRevision.stack.title", "app.stackRevision.stack.name", "appInstance.stackTitle", "appInstance.stack.title", "appInstance.stack.name", "appInstance.app.stackTitle", "appInstance.app.stack.title", "appInstance.app.stack.name", "appInstance.app.stackRev.stackTitle", "appInstance.app.stackRev.stack.title", "appInstance.app.stackRev.stack.name", "appInstance.app.stackRevision.stackTitle", "appInstance.app.stackRevision.stack.title", "appInstance.app.stackRevision.stack.name", "instance.stackTitle", "instance.stack.title", "instance.stack.name", "instance.app.stackTitle", "instance.app.stack.title", "instance.app.stack.name", "instance.app.stackRev.stackTitle", "instance.app.stackRev.stack.title", "instance.app.stackRev.stack.name", "instance.app.stackRevision.stackTitle", "instance.app.stackRevision.stack.title", "instance.app.stackRevision.stack.name"},
	},
	"service": {
		title:         "service",
		objectKey:     "appService",
		idTitle:       "service id",
		idPaths:       []string{"appServiceId", "appService.id", "serviceId", "service.id", "origin.appServiceId", "origin.appService.id", "origin.serviceId", "origin.service.id"},
		idScalarPaths: []string{"appService", "service", "origin.appService", "origin.service"},
		pathPrefix:    "/app-services/",
		titlePaths:    []string{"appServiceTitle", "appService.title", "serviceTitle", "service.title", "appService", "service", "appServiceName", "appService.name", "serviceName", "service.name", "origin.appServiceTitle", "origin.appService.title", "origin.appService.name", "origin.serviceTitle", "origin.service.title", "origin.service.name"},
	},
	"instance": {
		title:         "instance",
		objectKey:     "appInstance",
		idTitle:       "instance id",
		idPaths:       []string{"appInstanceId", "appInstance.id", "instanceId", "instance.id", "origin.appInstanceId", "origin.appInstance.id", "origin.instanceId", "origin.instance.id"},
		idScalarPaths: []string{"appInstance", "instance", "origin.appInstance", "origin.instance"},
		pathPrefix:    "/app-instances/",
		titlePaths:    []string{"appInstanceTitle", "appInstance.title", "instanceTitle", "instance.title", "appInstance", "instance", "appInstanceName", "appInstance.name", "instanceName", "instance.name", "origin.appInstanceTitle", "origin.appInstance.title", "origin.appInstance.name", "origin.instanceTitle", "origin.instance.title", "origin.instance.name"},
	},
	"database": {
		title:         "database",
		objectKey:     "database",
		idTitle:       "database id",
		idPaths:       []string{"databaseId", "database.id", "origin.databaseId", "origin.database.id"},
		idScalarPaths: []string{"database", "origin.database"},
		pathPrefix:    "/databases/",
		titlePaths:    []string{"databaseTitle", "database.title", "database", "databaseName", "database.name", "origin.databaseTitle", "origin.database.title", "origin.database.name"},
	},
	"databaseDb": {
		title:         "db",
		objectKey:     "databaseDb",
		idTitle:       "db id",
		idPaths:       []string{"databaseDbId", "databaseDb.id", "databaseDBId", "databaseDB.id", "dbId", "db.id", "origin.databaseDbId", "origin.databaseDb.id", "origin.databaseDBId", "origin.databaseDB.id", "origin.dbId", "origin.db.id"},
		idScalarPaths: []string{"databaseDb", "databaseDB", "db", "origin.databaseDb", "origin.databaseDB", "origin.db"},
		pathPrefix:    "/database-dbs/",
		titlePaths:    []string{"databaseDbTitle", "databaseDb.title", "databaseDBTitle", "databaseDB.title", "dbTitle", "db.title", "databaseDb", "databaseDB", "db", "databaseDbName", "databaseDb.name", "databaseDBName", "databaseDB.name", "dbName", "db.name", "origin.databaseDbTitle", "origin.databaseDb.title", "origin.databaseDb.name", "origin.databaseDBTitle", "origin.databaseDB.title", "origin.databaseDB.name", "origin.dbTitle", "origin.db.title", "origin.db.name"},
	},
	"port": {
		title:             "port",
		objectKey:         "port",
		idTitle:           "port id",
		idPaths:           []string{"portId", "port.id"},
		idScalarPaths:     []string{"port"},
		pathPrefix:        "/app-ports/",
		titlePaths:        []string{"portNumber", "portNumber.number", "port.number", "port.port", "port", "number", "portName", "port.name", "portTitle", "port.title"},
		allowNumericTitle: true,
	},
	"route": {
		title:         "route",
		objectKey:     "route",
		idTitle:       "route id",
		idPaths:       []string{"appRouteId", "appRoute.id", "routeId", "route.id"},
		idScalarPaths: []string{"appRoute", "route"},
		pathPrefix:    "/app-routes/",
		titlePaths:    []string{"appRouteHost", "appRoute.host", "appRoute.hostname", "routeHost", "route.host", "route.hostname", "host", "hostname", "domain", "url", "appRoute", "route"},
	},
	"cert": {
		title:         "cert",
		objectKey:     "cert",
		idTitle:       "cert id",
		idPaths:       []string{"appCertId", "appCert.id", "certId", "cert.id", "certificateId", "certificate.id"},
		idScalarPaths: []string{"appCert", "cert", "certificate"},
		pathPrefix:    "/app-certs/",
		pathPrefixes:  []string{"/certs/", "/certificates/"},
		titlePaths:    []string{"appCertHost", "appCert.host", "appCert.hostname", "certHost", "cert.host", "cert.hostname", "certificateHost", "certificate.host", "certificate.hostname", "commonName", "cert.commonName", "certificate.commonName", "host", "hostname", "domain", "appCert", "cert", "certificate"},
	},
	"task": {
		title:         "task",
		objectKey:     "task",
		idTitle:       "task id",
		idPaths:       []string{"taskId", "task.id"},
		idScalarPaths: []string{"task"},
		pathPrefix:    "/tasks/",
		titlePaths:    []string{"taskTitle", "task.title", "task", "taskName", "task.name"},
	},
	"backup": {
		title:         "backup",
		objectKey:     "backup",
		idTitle:       "backup id",
		idPaths:       []string{"backupId", "backup.id"},
		idScalarPaths: []string{"backup"},
		pathPrefix:    "/backups/",
		titlePaths:    []string{"backupTitle", "backup.title", "backup", "backupName", "backup.name"},
	},
	"author": {
		title:         "author",
		objectKey:     "author",
		idTitle:       "author id",
		idPaths:       []string{"authorId", "author.id", "createdById", "createdBy.id", "createdByUserId", "createdByUser.id", "createdByMembershipId", "createdByMembership.id", "createdByOrgMembershipId", "createdByOrgMembership.id", "orgMembershipId", "orgMembership.id", "membershipId", "membership.id"},
		idScalarPaths: []string{"author", "createdBy", "createdByUser", "createdByMembership", "createdByOrgMembership", "orgMembership", "membership"},
		pathPrefix:    "/org-memberships/",
		titlePaths:    []string{"authorName", "author.name", "author.title", "author.email", "author.user.name", "author.user.email", "createdByName", "createdBy.name", "createdBy.email", "createdBy.user.name", "createdBy.user.email", "createdByUser.name", "createdByUser.email", "createdByMembership.name", "createdByMembership.email", "createdByMembership.user.name", "createdByMembership.user.email", "createdByOrgMembership.name", "createdByOrgMembership.email", "createdByOrgMembership.user.name", "createdByOrgMembership.user.email", "orgMembership.name", "orgMembership.email", "orgMembership.user.name", "orgMembership.user.email", "membership.name", "membership.email", "membership.user.name", "membership.user.email"},
	},
}

func addOutputFlag(cmd *cobra.Command, opts *outputOptions) {
	cmd.PersistentFlags().StringVarP(&opts.output, "output", "o", outputTable, "Output format: table, vertical, or json")
}

func outputFormat(cmd *cobra.Command, opts outputOptions) string {
	if flag := cmd.Flag("output"); flag != nil {
		return flag.Value.String()
	}
	return opts.output
}

func addWaitFlags(cmd *cobra.Command, opts *waitOptions) {
	cmd.Flags().BoolVar(&opts.wait, "wait", false, "Wait for the created task or deployment to finish")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", 10*time.Minute, "Maximum time to wait")
}

func addBodyFlags(cmd *cobra.Command, opts *bodyOptions) {
	cmd.Flags().StringVar(&opts.data, "data", "", "JSON request body")
	cmd.Flags().StringVarP(&opts.file, "file", "f", "", "Path to JSON request body")
}

func defaultToList(cmd *cobra.Command, listCmd *cobra.Command) {
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return errors.Errorf("subcommand is required; use %q explicitly", listCmd.Name())
	}
}

func newRESTClient() (*rest.Client, error) {
	if viper.GetString("api_key") == "" && viper.GetString("access_token") == "" {
		return nil, errors.New("either api-key or access-token must be specified")
	}
	endpoint := apiBaseURL()
	if endpoint == "" {
		return nil, errors.New("api-base-url flag is required")
	}

	return rest.NewClient(types.APIConfig{
		Key:         viper.GetString("api_key"),
		AccessToken: viper.GetString("access_token"),
		Endpoint:    endpoint,
	})
}

func apiBaseURL() string {
	if endpoint := viper.GetString("api_endpoint"); endpoint != "" {
		return endpoint
	}
	return viper.GetString("api_base_url")
}

func readBody(opts bodyOptions) (interface{}, bool, error) {
	if opts.data != "" && opts.file != "" {
		return nil, false, errors.New("use either --data or --file, not both")
	}
	if opts.data == "" && opts.file == "" {
		return nil, false, nil
	}

	content := []byte(opts.data)
	if opts.file != "" {
		var err error
		content, err = os.ReadFile(opts.file)
		if err != nil {
			return nil, false, errors.WithStack(err)
		}
	}

	var body interface{}
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.UseNumber()
	if err := decoder.Decode(&body); err != nil {
		return nil, false, errors.WithStack(err)
	}

	return body, true, nil
}

func printResult(cmd *cobra.Command, opts outputOptions, value interface{}, columns []string) error {
	output := outputFormat(cmd, opts)
	switch output {
	case outputJSON:
		return printJSON(cmd, value)
	case outputTable:
		printTable(cmd, normalizeItems(value), columns)
		return nil
	case outputVertical:
		printVerticalTable(cmd, normalizeItems(value), columns, false)
		return nil
	default:
		return errors.Errorf("unsupported output format %q", output)
	}
}

func printGetResult(cmd *cobra.Command, opts outputOptions, value interface{}, columns []string) error {
	output := outputFormat(cmd, opts)
	switch output {
	case outputJSON:
		return printJSON(cmd, value)
	case outputTable, outputVertical:
		printVerticalTable(cmd, normalizeItem(value), columns, true)
		return nil
	default:
		return errors.Errorf("unsupported output format %q", output)
	}
}

func printClientResult(cmd *cobra.Command, client *rest.Client, opts outputOptions, value interface{}, columns []string) error {
	items := normalizeItems(value)
	if outputFormat(cmd, opts) != outputJSON && isCollection(items) {
		if err := enrichDisplayRelations(cmd.Context(), client, items, columns); err != nil {
			return err
		}
	}
	return printResult(cmd, opts, value, columns)
}

func printClientGetResult(cmd *cobra.Command, client *rest.Client, opts outputOptions, value interface{}, columns []string) error {
	if outputFormat(cmd, opts) != outputJSON {
		if err := enrichDisplayRelations(cmd.Context(), client, normalizeItem(value), columns); err != nil {
			return err
		}
	}
	return printGetResult(cmd, opts, value, columns)
}

func printJSON(cmd *cobra.Command, value interface{}) error {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return errors.WithStack(err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(content))
	return nil
}

func normalizeItems(value interface{}) interface{} {
	m, ok := value.(map[string]interface{})
	if !ok {
		return value
	}
	if !looksLikeResponseWrapper(m) {
		return value
	}
	for _, key := range []string{"items", "results"} {
		items, ok := m[key]
		if ok {
			return items
		}
	}
	if data, ok := m["data"]; ok && isCollection(data) {
		return data
	}
	return value
}

func normalizeItem(value interface{}) interface{} {
	m, ok := value.(map[string]interface{})
	if !ok {
		return value
	}
	if !looksLikeResponseWrapper(m) {
		return value
	}
	for _, key := range []string{"item", "result"} {
		item, ok := m[key]
		if ok {
			return item
		}
	}
	if data, ok := m["data"]; ok && !isCollection(data) {
		return data
	}
	return value
}

func isCollection(value interface{}) bool {
	switch value.(type) {
	case []interface{}, []map[string]interface{}:
		return true
	default:
		return false
	}
}

func enrichDisplayRelations(ctx context.Context, client *rest.Client, value interface{}, columns []string) error {
	rows := asRows(value)
	if len(rows) == 0 {
		return nil
	}

	cache := make(map[string]map[string]interface{})
	providersByRev := make(map[string]map[string]interface{})
	providersLoaded := false
	for _, column := range columns {
		if (column == "services" || column == "images") && !usesCompactServiceCount(columns) {
			enrichNestedServiceRelations(ctx, client, rows, cache)
		}
		if isProviderColumn(column) {
			for _, row := range rows {
				enrichProviderRelation(ctx, client, row, cache, providersByRev, &providersLoaded)
			}
			continue
		}

		relation, ok := displayRelationFor(column)
		if !ok || len(relationPathPrefixes(relation)) == 0 {
			continue
		}
		for _, row := range rows {
			if formatColumnValue(row, column) != "" {
				continue
			}
			id := firstRelationID(row, relation)
			if id == "" {
				continue
			}

			related, ok := fetchDisplayRelationForRelation(ctx, client, cache, relation, id)
			if !ok {
				continue
			}
			row[relation.objectKey] = related
			if relation.objectKey == "integration" {
				enrichProviderRelation(ctx, client, related, cache, providersByRev, &providersLoaded)
			}
		}
	}

	return nil
}

func usesCompactServiceCount(columns []string) bool {
	return sameColumns(columns, buildListColumns) || sameColumns(columns, deploymentListColumns)
}

func sameColumns(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func enrichNestedServiceRelations(ctx context.Context, client *rest.Client, rows []map[string]interface{}, cache map[string]map[string]interface{}) {
	relation := relationColumns["service"]
	for _, row := range rows {
		for _, path := range serviceCollectionPaths() {
			for _, item := range asRows(valueAtPath(row, path)) {
				if formatRelationColumn(item, relation) != "" {
					continue
				}
				id := firstRelationID(item, relation)
				if id == "" {
					continue
				}
				related, ok := fetchDisplayRelationForRelation(ctx, client, cache, relation, id)
				if !ok {
					continue
				}
				item[relation.objectKey] = related
			}
		}
	}
}

func serviceCollectionPaths() []string {
	return []string{
		"services",
		"appServices",
		"buildServices",
		"deploymentServices",
		"builtServices",
		"deployedServices",
		"appServiceBuilds",
		"appServiceDeployments",
		"serviceBuilds",
		"serviceDeployments",
		"appBuildServices",
		"appDeploymentServices",
		"build.services",
		"deployment.services",
		"appBuild.services",
		"appDeployment.services",
		"config.services",
	}
}

func relationPathPrefixes(relation relationColumn) []string {
	prefixes := make([]string, 0, 1+len(relation.pathPrefixes))
	if relation.pathPrefix != "" {
		prefixes = append(prefixes, relation.pathPrefix)
	}
	prefixes = append(prefixes, relation.pathPrefixes...)
	return prefixes
}

func fetchDisplayRelationForRelation(ctx context.Context, client *rest.Client, cache map[string]map[string]interface{}, relation relationColumn, id string) (map[string]interface{}, bool) {
	for _, pathPrefix := range relationPathPrefixes(relation) {
		if related, ok := fetchDisplayRelation(ctx, client, cache, pathPrefix, id); ok {
			return related, true
		}
	}
	return nil, false
}

func fetchDisplayRelation(ctx context.Context, client *rest.Client, cache map[string]map[string]interface{}, pathPrefix string, id string) (map[string]interface{}, bool) {
	cacheKey := pathPrefix + id
	if related, ok := cache[cacheKey]; ok {
		return related, related != nil
	}

	var result interface{}
	if err := client.Get(ctx, pathPrefix+url.PathEscape(id), nil, &result); err != nil {
		cache[cacheKey] = nil
		return nil, false
	}
	relatedRows := responseRows(result)
	if len(relatedRows) == 0 {
		cache[cacheKey] = nil
		return nil, false
	}

	cache[cacheKey] = relatedRows[0]
	return relatedRows[0], true
}

func enrichProviderRelation(ctx context.Context, client *rest.Client, row map[string]interface{}, cache map[string]map[string]interface{}, providersByRev map[string]map[string]interface{}, providersLoaded *bool) {
	if row == nil || formatProviderColumn(row) != "" {
		return
	}

	if providerID := firstScalarPath(row, "providerId", "provider.id"); providerID != "" {
		if provider, ok := fetchDisplayRelation(ctx, client, cache, "/providers/", providerID); ok {
			row["provider"] = provider
			return
		}
	}

	providerRevID := firstScalarPath(row, "providerRevId", "providerRev.id", "providerRevision.id")
	if providerRevID == "" {
		return
	}

	if !*providersLoaded {
		loadProvidersByRev(ctx, client, providersByRev)
		*providersLoaded = true
	}
	if provider := providersByRev[providerRevID]; provider != nil {
		row["provider"] = provider
		if _, ok := row["providerRev"]; !ok {
			row["providerRev"] = map[string]interface{}{"id": providerRevID}
		}
	}
}

func loadProvidersByRev(ctx context.Context, client *rest.Client, providersByRev map[string]map[string]interface{}) {
	var result interface{}
	if err := client.Get(ctx, "/providers", nil, &result); err != nil {
		return
	}

	for _, provider := range responseRows(result) {
		for _, path := range []string{"revId", "latestRevId", "providerRevId", "providerRev.id", "providerRevision.id"} {
			revID := firstScalarPath(provider, path)
			if revID != "" {
				providersByRev[revID] = provider
			}
		}
	}
}

func isProviderColumn(column string) bool {
	switch column {
	case "provider", "providerId", "providerRevId":
		return true
	default:
		return false
	}
}

func displayRelationFor(column string) (relationColumn, bool) {
	switch column {
	case "integration", "integrationId":
		return relationColumn{
			objectKey: "integration",
			idPaths: []string{
				"integrationId",
				"integration.id",
				"integrationID",
				"clusterIntegrationId",
				"clusterIntegration.id",
				"cloudIntegrationId",
				"cloudIntegration.id",
				"cloudProviderIntegrationId",
				"cloudProviderIntegration.id",
				"providerIntegrationId",
				"providerIntegration.id",
				"kubernetesIntegrationId",
				"kubernetesIntegration.id",
			},
			idScalarPaths: []string{"integration", "clusterIntegration", "cloudIntegration", "cloudProviderIntegration", "providerIntegration", "kubernetesIntegration"},
			pathPrefix:    "/integrations/",
		}, true
	default:
		return relationColumnFor(column)
	}
}

func printTable(cmd *cobra.Command, value interface{}, columns []string) {
	rows := asRows(value)
	if len(rows) == 0 {
		return
	}
	if len(columns) == 0 {
		columns = inferColumns(rows)
	}

	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	headers := make([]string, 0, len(columns))
	for _, column := range columns {
		headers = append(headers, tableColumnTitle(column))
	}
	fmt.Fprintln(writer, strings.Join(headers, "\t"))
	for _, row := range rows {
		values := make([]string, 0, len(columns))
		for _, column := range columns {
			values = append(values, formatTableColumnValue(row, column))
		}
		fmt.Fprintln(writer, strings.Join(values, "\t"))
	}
	_ = writer.Flush()
}

func formatTableColumnValue(row map[string]interface{}, column string) string {
	if isTimeColumn(column) {
		return formatRelativeDisplayTime(timeColumnValue(row, column))
	}
	if column == "services" {
		if count := serviceCount(row); count != 0 {
			return pluralizeCount(count, "service", "services")
		}
	}
	return formatColumnValue(row, column)
}

func timeColumnValue(row map[string]interface{}, column string) interface{} {
	if value := row[column]; value != nil {
		return value
	}
	switch column {
	case "certExpiresAt", "certificateExpiresAt", "expiresAt":
		return certTimeColumnValue(row, column)
	default:
		return nil
	}
}

func printVerticalTable(cmd *cobra.Command, value interface{}, columns []string, showRelationIDs bool) {
	rows := asRows(value)
	if len(rows) == 0 {
		return
	}
	if len(columns) == 0 {
		columns = inferColumns(rows)
	}

	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	for rowIndex, row := range rows {
		if rowIndex > 0 {
			fmt.Fprintln(writer)
		}
		for _, column := range columns {
			title := tableColumnTitle(column)
			value := formatColumnValue(row, column)
			fmt.Fprintf(writer, "%s:\t%s\n", title, value)
			if showRelationIDs {
				for _, extra := range verticalExtraRows(row, column, value) {
					fmt.Fprintf(writer, "%s:\t%s\n", extra.title, extra.value)
				}
			}
		}
	}
	_ = writer.Flush()
}

type verticalRow struct {
	title string
	value string
}

func verticalExtraRows(row map[string]interface{}, column string, displayedValue string) []verticalRow {
	switch column {
	case "integration", "integrationId":
		if clusterIntegrationSpecialLabel(row) != "" {
			return nil
		}
		return extraIDRows(row, displayedValue, relationColumn{
			idTitle: "integration id",
			idPaths: []string{"integrationId", "integration.id"},
		})
	case "provider", "providerId", "providerRevId":
		rows := extraIDRows(row, displayedValue, relationColumn{
			idTitle: "provider id",
			idPaths: []string{"providerId", "provider.id", "providerRev.providerId", "providerRev.provider.id", "providerRevision.providerId", "providerRevision.provider.id"},
		})
		rows = append(rows, extraIDRows(row, displayedValue, relationColumn{
			idTitle: "provider rev id",
			idPaths: []string{"providerRevId", "providerRev.id", "providerRevision.id"},
		})...)
		return rows
	default:
		relation, ok := relationColumnFor(column)
		if !ok {
			return nil
		}
		return extraIDRows(row, displayedValue, relation)
	}
}

func extraIDRows(row map[string]interface{}, displayedValue string, relation relationColumn) []verticalRow {
	value := firstRelationID(row, relation)
	if value == "" || value == displayedValue {
		return nil
	}
	return []verticalRow{{title: relation.idTitle, value: value}}
}

func tableColumnTitle(column string) string {
	switch column {
	case "integration", "integrationId":
		return "integration"
	case "provider", "providerId", "providerRevId":
		return "provider"
	case "domain", "mainDomain":
		return "domain"
	case "infraAppInstanceId":
		return "instance id"
	case "ips":
		return "ips"
	case "imageCount":
		return "images"
	default:
		if relation, ok := relationColumnFor(column); ok {
			return relation.title
		}
		return humanizeColumnTitle(column)
	}
}

func humanizeColumnTitle(column string) string {
	if column == "" {
		return ""
	}

	var words []string
	var current []rune
	runes := []rune(column)
	for index, char := range runes {
		if char == '_' || char == '-' || char == ' ' {
			if len(current) != 0 {
				words = append(words, string(current))
				current = nil
			}
			continue
		}

		if index > 0 && isUpperASCII(char) {
			prev := runes[index-1]
			nextIsLower := index+1 < len(runes) && isLowerASCII(runes[index+1])
			if len(current) != 0 && (isLowerASCII(prev) || isDigitASCII(prev) || nextIsLower) {
				words = append(words, string(current))
				current = nil
			}
		}
		current = append(current, char)
	}
	if len(current) != 0 {
		words = append(words, string(current))
	}

	for index, word := range words {
		words[index] = strings.ToLower(word)
	}
	return strings.Join(words, " ")
}

func isUpperASCII(char rune) bool {
	return char >= 'A' && char <= 'Z'
}

func isLowerASCII(char rune) bool {
	return char >= 'a' && char <= 'z'
}

func isDigitASCII(char rune) bool {
	return char >= '0' && char <= '9'
}

func formatColumnValue(row map[string]interface{}, column string) string {
	switch column {
	case "integration", "integrationId":
		return formatIntegrationColumn(row)
	case "provider", "providerId", "providerRevId":
		return formatProviderColumn(row)
	case "domain", "mainDomain":
		return firstScalarPath(row, "domain", "mainDomain")
	case "author", "authorId":
		return formatAuthorColumn(row)
	case "member":
		return formatOrgMembershipLabel(row)
	case "email":
		return firstScalarPath(row, "email", "user.email", "account.email", "profile.email")
	case "role":
		return firstScalarPath(row, "role", "orgRole", "organizationRole", "membershipRole")
	case "progress":
		return formatProgressColumn(row)
	case "projects":
		return formatProjectsColumn(row)
	case "originTask":
		return firstScalarPath(row, "originTaskId", "originTask.id", "origin.id")
	case "repeatedTask":
		return firstScalarPath(row, "repeatedTaskId", "repeatedTask.id")
	case "spawnedTasks":
		return formatSpawnedTasksColumn(row)
	case "duration":
		if duration := firstScalarPath(row, "duration"); duration != "" {
			return duration
		}
		return formatDurationColumn(row)
	case "services":
		return formatServicesColumn(row)
	case "imageCount":
		return formatImageCountColumn(row)
	case "images":
		return formatImagesColumn(row)
	case "builds":
		return formatBuildsColumn(row)
	case "cert", "appCert", "certificate":
		return formatCertColumn(row)
	case "certStatus", "certificateStatus":
		return firstScalarPath(row, "certStatus", "certificateStatus", "tlsStatus", "appCertStatus", "appCert.status", "cert.status", "certificate.status")
	case "issuer", "certIssuer", "certificateIssuer":
		return formatCertIssuerColumn(row)
	case "certType", "certificateType":
		return firstScalarPath(row, "certType", "certificateType", "appCertType", "type", "kind", "appCert.type", "appCert.kind", "cert.type", "cert.kind", "certificate.type", "certificate.kind")
	case "certExpiresAt", "certificateExpiresAt", "expiresAt":
		return formatDisplayTime(certTimeColumnValue(row, column))
	case "host":
		return firstScalarPath(row, "host", "hostname", "domain", "commonName", "appCert.host", "appCert.hostname", "appCert.commonName", "cert.host", "cert.hostname", "cert.commonName", "certificate.host", "certificate.hostname", "certificate.commonName")
	case "domains":
		return formatDomainsColumn(row)
	case "instances":
		return formatInstancesColumn(row)
	case "dbs":
		return formatDBsColumn(row)
	case "kubernetesVersion":
		return firstScalarPath(row, "kubernetesVersion", "kubernetes.version", "version")
	case "infraVersion":
		return firstScalarPath(row, "infraVersion", "infrastructureVersion", "infra.version", "infrastructure.version")
	case "ips", "publicIp":
		return formatIPsColumn(row)
	case "nodes":
		return formatClusterNodesColumn(row)
	case "singleNode":
		return firstScalarPath(row, "singleNode", "single_node", "single-node")
	case "scalable":
		return formatClusterScalableColumn(row)
	case "username":
		if username := firstScalarPath(row, "username", "userName", "databaseUsername", "login"); username != "" {
			return username
		}
		if firstRelationID(row, relationColumns["database"]) != "" {
			return firstScalarPath(row, "name")
		}
		return ""
	case "jobs":
		return formatTaskJobsColumn(row)
	case "outdated":
		return formatOutdatedColumn(row)
	case "currentRevNumber", "revision":
		return formatCurrentRevisionNumber(row)
	case "currentVersion":
		return firstScalarPath(row, "currentVersion", "version", "originStackRevVersion", "stackRevVersion", "stackRevisionVersion", "stackRev.version", "stackRevision.version")
	case "infraAppInstanceId":
		return formatInfraAppInstanceIDColumn(row)
	default:
		if relation, ok := relationColumnFor(column); ok {
			return formatRelationColumn(row, relation)
		}
		if isTimeColumn(column) {
			return formatDisplayTime(row[column])
		}
		return formatValue(row[column])
	}
}

func isTimeColumn(column string) bool {
	return strings.HasSuffix(column, "At") || strings.HasSuffix(column, "_at")
}

func formatDisplayTime(value interface{}) string {
	t, ok := parseDisplayTime(value)
	if !ok {
		return formatValue(value)
	}
	return t.Format("2006-01-02 15:04")
}

func formatRelativeDisplayTime(value interface{}) string {
	t, ok := parseDisplayTime(value)
	if !ok {
		return formatValue(value)
	}
	return formatRelativeTime(t, time.Now())
}

func formatRelativeTime(t time.Time, now time.Time) string {
	duration := now.Sub(t)
	if duration < 0 {
		return "in " + formatRelativeDuration(-duration)
	}
	if duration < time.Second {
		return "just now"
	}
	return formatRelativeDuration(duration) + " ago"
}

func formatRelativeDuration(duration time.Duration) string {
	switch {
	case duration < time.Minute:
		seconds := int(duration / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		return fmt.Sprintf("%ds", seconds)
	case duration < time.Hour:
		return fmt.Sprintf("%dm", int(duration/time.Minute))
	case duration < 24*time.Hour:
		return fmt.Sprintf("%dh", int(duration/time.Hour))
	case duration < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(duration/(24*time.Hour)))
	case duration < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(duration/(30*24*time.Hour)))
	default:
		return fmt.Sprintf("%dy", int(duration/(365*24*time.Hour)))
	}
}

func parseDisplayTime(value interface{}) (time.Time, bool) {
	switch v := value.(type) {
	case time.Time:
		if v.IsZero() {
			return time.Time{}, false
		}
		return v, true
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return time.Time{}, false
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			t, err := time.Parse(layout, v)
			if err == nil && !t.IsZero() {
				return t, true
			}
		}
		for _, layout := range []string{
			"2006-01-02T15:04:05.999999999",
			"2006-01-02T15:04:05",
			"2006-01-02T15:04",
			"2006-01-02 15:04:05",
			"2006-01-02 15:04",
		} {
			t, err := time.ParseInLocation(layout, v, time.Local)
			if err == nil && !t.IsZero() {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

func formatDurationColumn(row map[string]interface{}) string {
	startedAt, ok := parseDisplayTime(valueAtPath(row, "startedAt"))
	if !ok {
		return ""
	}

	endedAt, ok := parseDisplayTime(valueAtPath(row, "endedAt"))
	if !ok {
		endedAt = time.Now()
	}

	duration := endedAt.Sub(startedAt)
	if duration < 0 {
		return ""
	}
	return formatDisplayDuration(duration)
}

func formatDisplayDuration(duration time.Duration) string {
	if duration < time.Second {
		return "<1s"
	}

	duration = duration.Round(time.Second)
	days := int64(duration / (24 * time.Hour))
	duration -= time.Duration(days) * 24 * time.Hour
	hours := int64(duration / time.Hour)
	duration -= time.Duration(hours) * time.Hour
	minutes := int64(duration / time.Minute)
	duration -= time.Duration(minutes) * time.Minute
	seconds := int64(duration / time.Second)

	parts := make([]string, 0, 2)
	addPart := func(value int64, unit string) {
		if value != 0 && len(parts) < 2 {
			parts = append(parts, fmt.Sprintf("%d%s", value, unit))
		}
	}
	addPart(days, "d")
	addPart(hours, "h")
	addPart(minutes, "m")
	addPart(seconds, "s")
	if len(parts) == 0 {
		return "0s"
	}
	return strings.Join(parts, " ")
}

func relationColumnFor(column string) (relationColumn, bool) {
	switch column {
	case "app", "appId":
		return relationColumns["app"], true
	case "env", "envId", "environment", "environmentId":
		return relationColumns["env"], true
	case "cluster", "clusterId":
		return relationColumns["cluster"], true
	case "stack", "stackId":
		return relationColumns["stack"], true
	case "service", "appService", "appServiceId", "serviceId":
		return relationColumns["service"], true
	case "instance", "appInstance", "appInstanceId", "instanceId":
		return relationColumns["instance"], true
	case "database", "databaseId":
		return relationColumns["database"], true
	case "databaseDb", "databaseDbId", "databaseDB", "databaseDBId", "db", "dbId":
		return relationColumns["databaseDb"], true
	case "port", "portId":
		return relationColumns["port"], true
	case "route", "appRoute", "appRouteId", "routeId":
		return relationColumns["route"], true
	case "cert", "appCert", "appCertId", "certificate", "certificateId", "certId":
		return relationColumns["cert"], true
	case "task", "taskId":
		return relationColumns["task"], true
	case "backup", "backupId":
		return relationColumns["backup"], true
	case "author", "authorId", "createdBy", "createdById", "orgMembership", "orgMembershipId", "membership", "membershipId":
		return relationColumns["author"], true
	default:
		return relationColumn{}, false
	}
}

func formatRelationColumn(row map[string]interface{}, relation relationColumn) string {
	if relation.allowNumericTitle {
		return firstScalarPath(row, relation.titlePaths...)
	}
	return firstTitlePath(row, relation.titlePaths...)
}

func asRows(value interface{}) []map[string]interface{} {
	switch v := value.(type) {
	case []map[string]interface{}:
		return v
	case []interface{}:
		rows := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			if row, ok := item.(map[string]interface{}); ok {
				rows = append(rows, row)
			}
		}
		return rows
	case map[string]interface{}:
		return []map[string]interface{}{v}
	default:
		return nil
	}
}

func responseRows(value interface{}) []map[string]interface{} {
	if row, ok := value.(map[string]interface{}); ok && !looksLikeResponseWrapper(row) {
		return []map[string]interface{}{row}
	}
	if row, ok := value.(map[string]interface{}); ok {
		for _, key := range []string{"items", "results", "item", "result", "data"} {
			if nested, ok := row[key]; ok {
				if rows := responseRows(nested); len(rows) != 0 {
					return rows
				}
			}
		}
	}
	return asRows(value)
}

func looksLikeResponseWrapper(row map[string]interface{}) bool {
	for _, key := range []string{"id", "title", "name"} {
		if _, ok := row[key]; ok {
			return false
		}
	}
	for _, key := range []string{"items", "results", "item", "result", "data"} {
		if _, ok := row[key]; ok {
			return true
		}
	}
	return false
}

func inferColumns(rows []map[string]interface{}) []string {
	seen := make(map[string]bool)
	for _, row := range rows {
		for key := range row {
			seen[key] = true
		}
	}
	columns := make([]string, 0, len(seen))
	for key := range seen {
		columns = append(columns, key)
	}
	sort.Strings(columns)
	return columns
}

func formatValue(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case []interface{}, map[string]interface{}:
		content, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(content)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func formatOutdatedColumn(row map[string]interface{}) string {
	value := firstNonNilPath(
		row,
		"outdated",
		"isOutdated",
		"stackOutdated",
		"isStackOutdated",
		"instanceOutdated",
		"isInstanceOutdated",
		"appInstanceOutdated",
		"isAppInstanceOutdated",
		"needsUpdate",
		"needsUpgrade",
		"updateAvailable",
	)
	if value != nil {
		return formatValue(value)
	}

	currentRev := firstScalarPath(
		row,
		"revNumber",
		"revisionNumber",
		"currentRevNumber",
		"currentRevisionNumber",
		"stackRevNumber",
		"stackRevisionNumber",
		"stackRev.number",
		"stackRev.revNumber",
		"stackRev.revision",
		"stackRevision.number",
		"stackRevision.revNumber",
		"stackRevision.revision",
		"app.stackRev.number",
		"app.stackRev.revNumber",
		"app.stackRev.revision",
		"app.stackRevision.number",
		"app.stackRevision.revNumber",
		"app.stackRevision.revision",
	)
	latestRev := firstScalarPath(
		row,
		"latestRevNumber",
		"latestRevisionNumber",
		"stack.latestRevNumber",
		"stack.latestRevisionNumber",
		"stackRev.stack.latestRevNumber",
		"stackRevision.stack.latestRevNumber",
		"app.stack.latestRevNumber",
		"app.stack.latestRevisionNumber",
		"app.stackRev.stack.latestRevNumber",
		"app.stackRevision.stack.latestRevNumber",
	)
	current, currentOK := parseRevisionNumber(currentRev)
	latest, latestOK := parseRevisionNumber(latestRev)
	if currentOK && latestOK {
		return strconv.FormatBool(latest > current)
	}

	return ""
}

func formatProgressColumn(row map[string]interface{}) string {
	value := firstNonNilPath(row, "progress", "taskProgress")
	if value == nil {
		return ""
	}

	switch v := value.(type) {
	case json.Number:
		if number, err := v.Float64(); err == nil {
			return formatProgressNumber(number)
		}
	case float64:
		return formatProgressNumber(v)
	case float32:
		return formatProgressNumber(float64(v))
	case int:
		return formatProgressNumber(float64(v))
	case int64:
		return formatProgressNumber(float64(v))
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" || strings.HasSuffix(trimmed, "%") {
			return trimmed
		}
		if number, err := strconv.ParseFloat(trimmed, 64); err == nil {
			return formatProgressNumber(number)
		}
	}
	return formatValue(value)
}

func formatProgressNumber(value float64) string {
	if value > 0 && value <= 1 {
		value *= 100
	}
	if value == float64(int64(value)) {
		return fmt.Sprintf("%d%%", int64(value))
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", value), "0"), ".") + "%"
}

func formatProjectsColumn(row map[string]interface{}) string {
	labels := make([]string, 0)
	for _, path := range []string{"projects", "taskProjects"} {
		for _, project := range asRows(valueAtPath(row, path)) {
			label := firstTitlePath(project, "title", "name", "projectTitle", "projectName", "project.title", "project.name")
			if label == "" {
				label = firstScalarPath(project, "id", "projectId", "project.id")
			}
			if label != "" {
				labels = append(labels, label)
			}
		}
	}
	if len(labels) != 0 {
		return strings.Join(labels, ", ")
	}

	return strings.Join(scalarListValues(firstNonNilPath(row, "projectIds", "projectIDs", "projectsIds", "projectId", "project.id")), ", ")
}

func formatSpawnedTasksColumn(row map[string]interface{}) string {
	labels := make([]string, 0)
	for _, task := range asRows(valueAtPath(row, "spawnedTasks")) {
		label := firstTitlePath(task, "title", "name")
		id := firstScalarPath(task, "id")
		if label != "" && id != "" && label != id {
			label += " [" + id + "]"
		}
		if label == "" {
			label = id
		}
		if label != "" {
			labels = append(labels, label)
		}
	}
	if len(labels) != 0 {
		return strings.Join(labels, ", ")
	}

	return strings.Join(scalarListValues(firstNonNilPath(row, "spawnedTaskIds", "spawnedTaskIDs")), ", ")
}

func scalarListValues(value interface{}) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case []interface{}:
		values := make([]string, 0, len(v))
		for _, item := range v {
			if formatted := formatValue(item); formatted != "" {
				values = append(values, formatted)
			}
		}
		return values
	case []string:
		values := make([]string, 0, len(v))
		for _, item := range v {
			if item != "" {
				values = append(values, item)
			}
		}
		return values
	case []int:
		values := make([]string, 0, len(v))
		for _, item := range v {
			values = append(values, strconv.Itoa(item))
		}
		return values
	case []float64:
		values := make([]string, 0, len(v))
		for _, item := range v {
			values = append(values, strconv.FormatFloat(item, 'f', -1, 64))
		}
		return values
	default:
		formatted := formatValue(value)
		if formatted == "" {
			return nil
		}
		return strings.Split(formatted, ",")
	}
}

func formatCurrentRevisionNumber(row map[string]interface{}) string {
	return firstScalarPath(
		row,
		"currentRevNumber",
		"currentRevisionNumber",
		"revNumber",
		"revisionNumber",
		"originStackRevNumber",
		"stackRevNumber",
		"stackRevisionNumber",
		"stackRev.number",
		"stackRev.revNumber",
		"stackRev.revision",
		"stackRevision.number",
		"stackRevision.revNumber",
		"stackRevision.revision",
	)
}

func formatInfraAppInstanceIDColumn(row map[string]interface{}) string {
	if id := firstScalarPath(
		row,
		"infraAppInstanceId",
		"clusterAppInstanceId",
		"appInstanceId",
		"appInstance.id",
		"instanceId",
		"instance.id",
	); id != "" {
		return id
	}

	for _, path := range []string{"appInstances", "instances"} {
		if id := firstInstanceID(valueAtPath(row, path)); id != "" {
			return id
		}
	}

	return ""
}

func firstInstanceID(value interface{}) string {
	for _, row := range asRows(value) {
		if id := firstScalarPath(row, "id", "appInstanceId", "appInstance.id", "instanceId", "instance.id"); id != "" {
			return id
		}
	}
	return ""
}

func parseRevisionNumber(value string) (int, bool) {
	value = strings.TrimSpace(strings.TrimPrefix(value, "#"))
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(value)
	return parsed, err == nil
}

func formatIntegrationColumn(row map[string]interface{}) string {
	if label := clusterIntegrationSpecialLabel(row); label != "" {
		return label
	}

	title := firstTitlePath(
		row,
		"integrationTitle",
		"integration.title",
		"integration.name",
		"integrationName",
		"integration",
		"clusterIntegrationTitle",
		"clusterIntegration.title",
		"clusterIntegration.name",
		"clusterIntegration",
		"cloudIntegrationTitle",
		"cloudIntegration.title",
		"cloudIntegration.name",
		"cloudIntegration",
		"cloudProviderIntegrationTitle",
		"cloudProviderIntegration.title",
		"cloudProviderIntegration.name",
		"cloudProviderIntegration",
		"providerIntegrationTitle",
		"providerIntegration.title",
		"providerIntegration.name",
		"providerIntegration",
		"kubernetesIntegrationTitle",
		"kubernetesIntegration.title",
		"kubernetesIntegration.name",
		"kubernetesIntegration",
	)
	if title == "" {
		return ""
	}

	providerTitle := firstScalarPath(
		row,
		"integration.providerTitle",
		"integration.provider.title",
		"integration.provider.name",
		"integration.providerRev.providerTitle",
		"integration.providerRev.provider.title",
		"integration.providerRev.provider.name",
		"integration.providerRevision.providerTitle",
		"integration.providerRevision.provider.title",
		"integration.providerRevision.provider.name",
		"providerTitle",
		"provider.title",
		"provider.name",
	)
	if providerTitle != "" && providerTitle != title {
		return fmt.Sprintf("%s (%s)", title, providerTitle)
	}

	return title
}

func clusterIntegrationSpecialLabel(row map[string]interface{}) string {
	if truthyPath(row, "wodby", "isWodby", "wodbyCloud", "isWodbyCloud") {
		return "Wodby Cloud"
	}
	if truthyPath(row, "k3s", "isK3s") {
		return "k3s"
	}
	if truthyPath(row, "demo", "isDemo") {
		return "Demo"
	}
	if !looksLikeClusterRow(row) {
		return ""
	}

	for _, path := range []string{
		"clusterType",
		"clusterKind",
		"clusterProvider",
		"clusterProviderType",
		"type",
		"kind",
		"provider",
		"providerType",
		"integrationKind",
		"integration.kind",
		"integration.type",
		"integration.provider",
		"integration.providerType",
	} {
		switch normalizeDisplayToken(firstScalarPath(row, path)) {
		case "wodby", "wodbycloud":
			return "Wodby Cloud"
		case "k3s":
			return "k3s"
		case "demo":
			return "Demo"
		}
	}

	return ""
}

func looksLikeClusterRow(row map[string]interface{}) bool {
	for _, key := range []string{"serverless", "clusterType", "clusterKind", "clusterProvider", "clusterProviderType"} {
		if _, ok := row[key]; ok {
			return true
		}
	}
	return false
}

func truthyPath(row map[string]interface{}, paths ...string) bool {
	for _, path := range paths {
		switch v := valueAtPath(row, path).(type) {
		case bool:
			if v {
				return true
			}
		case string:
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "true", "yes", "1":
				return true
			}
		case json.Number:
			if v.String() == "1" {
				return true
			}
		case float64:
			if v == 1 {
				return true
			}
		case int:
			if v == 1 {
				return true
			}
		}
	}
	return false
}

func normalizeDisplayToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, " ", "")
	return value
}

func formatServicesColumn(row map[string]interface{}) string {
	value := firstNonNilPath(row,
		"services",
		"appServices",
		"buildServices",
		"deploymentServices",
		"builtServices",
		"deployedServices",
		"appServiceBuilds",
		"appServiceDeployments",
		"serviceBuilds",
		"serviceDeployments",
		"appBuildServices",
		"appDeploymentServices",
		"build.services",
		"deployment.services",
		"appBuild.services",
		"appDeployment.services",
		"config.services",
		"stackServices",
		"catalogServices",
	)
	if value == nil {
		return ""
	}

	labels := serviceLabels(value)
	if len(labels) != 0 {
		return strings.Join(labels, ", ")
	}
	return formatValue(value)
}

func serviceCount(row map[string]interface{}) int {
	for _, path := range serviceCollectionPaths() {
		rows := asRows(valueAtPath(row, path))
		if len(rows) != 0 {
			return len(rows)
		}
	}
	return 0
}

func formatImageCountColumn(row map[string]interface{}) string {
	if count := collectionCountFromPaths(row, []string{
		"imageCount",
		"imagesCount",
		"appServiceBuilds",
		"serviceBuilds",
		"images",
		"appImages",
		"buildImages",
		"builtImages",
		"serviceImages",
	}); count != 0 {
		return pluralizeCount(count, "image", "images")
	}
	if labels := deploymentImageLabels(row); len(labels) != 0 {
		return pluralizeCount(len(labels), "image", "images")
	}
	return ""
}

func formatBuildsColumn(row map[string]interface{}) string {
	count := collectionCountFromPaths(row, []string{"buildCount", "buildsCount", "builds", "appBuilds"})
	if count == 0 {
		return ""
	}
	return pluralizeCount(count, "build", "builds")
}

func collectionCountFromPaths(row map[string]interface{}, paths []string) int {
	for _, path := range paths {
		value := valueAtPath(row, path)
		switch v := value.(type) {
		case nil:
			continue
		case json.Number:
			if count, err := strconv.Atoi(v.String()); err == nil {
				return count
			}
		case float64:
			return int(v)
		case int:
			return v
		case []interface{}:
			return len(v)
		case []map[string]interface{}:
			return len(v)
		case []string:
			return len(v)
		default:
			if formatted := scalarString(v); formatted != "" {
				if count, err := strconv.Atoi(formatted); err == nil {
					return count
				}
			}
		}
	}
	return 0
}

func pluralizeCount(count int, singular string, plural string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, singular)
	}
	return fmt.Sprintf("%d %s", count, plural)
}

func formatImagesColumn(row map[string]interface{}) string {
	if labels := deploymentImageLabels(row); len(labels) != 0 {
		return strings.Join(labels, ", ")
	}

	var fallback interface{}
	for _, path := range imageCollectionPaths() {
		value := valueAtPath(row, path)
		if value == nil {
			continue
		}

		if fallback == nil {
			fallback = value
		}
		labels := imageLabels(value)
		if len(labels) != 0 {
			return strings.Join(labels, ", ")
		}
	}

	return formatValue(fallback)
}

func imageCollectionPaths() []string {
	return []string{
		"images",
		"appImages",
		"buildImages",
		"deploymentImages",
		"builtImages",
		"deployedImages",
		"serviceImages",
		"appBuildImages",
		"appDeploymentImages",
		"build.images",
		"deployment.images",
		"appBuild.images",
		"appDeployment.images",
		"appServiceBuilds",
		"serviceBuilds",
		"builds",
		"appBuilds",
		"build",
		"appBuild",
		"appServiceDeployments",
		"serviceDeployments",
		"services",
		"appServices",
		"buildServices",
		"deploymentServices",
		"builtServices",
		"deployedServices",
		"appBuildServices",
		"appDeploymentServices",
		"build.services",
		"deployment.services",
		"appBuild.services",
		"appDeployment.services",
	}
}

func deploymentImageLabels(row map[string]interface{}) []string {
	buildImagesByID := map[string]string{}
	buildImagesByServiceID := map[string]string{}
	for _, build := range asRows(firstNonNilPath(row, "builds", "appBuilds", "build", "appBuild")) {
		for _, serviceBuild := range asRows(firstNonNilPath(build, "appServiceBuilds", "serviceBuilds", "services")) {
			image := imageReference(serviceBuild)
			if image == "" {
				continue
			}
			if id := firstScalarPath(serviceBuild, "id", "appServiceBuildId", "serviceBuildId"); id != "" {
				buildImagesByID[id] = image
			}
			if serviceID := firstRelationID(serviceBuild, relationColumns["service"]); serviceID != "" {
				buildImagesByServiceID[serviceID] = image
			}
		}
	}

	if len(buildImagesByID) == 0 && len(buildImagesByServiceID) == 0 {
		return nil
	}

	serviceDeployments := firstNonNilPath(row, "appServiceDeployments", "serviceDeployments", "deploymentServices", "appDeploymentServices", "services")
	labels := make([]string, 0)
	for _, deployment := range asRows(serviceDeployments) {
		image := ""
		if buildID := firstScalarPath(deployment, "appServiceBuildId", "appServiceBuild.id", "serviceBuildId", "serviceBuild.id"); buildID != "" {
			image = buildImagesByID[buildID]
		}
		if image == "" {
			serviceID := firstRelationID(deployment, relationColumns["service"])
			image = buildImagesByServiceID[serviceID]
		}
		if image == "" {
			continue
		}
		if service := serviceLabel(deployment); service != "" && service != image {
			labels = append(labels, service+"="+image)
			continue
		}
		labels = append(labels, image)
	}
	return labels
}

func imageLabels(value interface{}) []string {
	switch v := value.(type) {
	case []interface{}:
		labels := make([]string, 0, len(v))
		for _, item := range v {
			if label := imageLabel(item); label != "" {
				labels = append(labels, label)
			}
		}
		return labels
	case []map[string]interface{}:
		labels := make([]string, 0, len(v))
		for _, item := range v {
			if label := imageLabel(item); label != "" {
				labels = append(labels, label)
			}
		}
		return labels
	case []string:
		labels := make([]string, 0, len(v))
		for _, item := range v {
			if item = strings.TrimSpace(item); item != "" {
				labels = append(labels, item)
			}
		}
		return labels
	default:
		if label := imageLabel(value); label != "" {
			return []string{label}
		}
		return nil
	}
}

func imageLabel(value interface{}) string {
	m, ok := value.(map[string]interface{})
	if !ok {
		return scalarString(value)
	}

	if nested := firstNonNilPath(m, "images", "appImages", "builtImages", "deployedImages", "serviceImages", "appServiceBuilds", "serviceBuilds"); nested != nil {
		if labels := imageLabels(nested); len(labels) != 0 {
			service := serviceLabel(value)
			if service != "" {
				for index, label := range labels {
					if !strings.Contains(label, "=") {
						labels[index] = service + "=" + label
					}
				}
			}
			return strings.Join(labels, ", ")
		}
	}

	image := imageReference(m)
	if image == "" {
		return ""
	}

	if service := serviceLabel(value); service != "" && service != image {
		return service + "=" + image
	}
	return image
}

func imageReference(row map[string]interface{}) string {
	return firstScalarPath(
		row,
		"image",
		"image.image",
		"image.name",
		"image.ref",
		"image.reference",
		"image.url",
		"builtImage",
		"builtImage.image",
		"builtImage.name",
		"builtImage.ref",
		"builtImage.reference",
		"deployedImage",
		"deployedImage.image",
		"deployedImage.name",
		"deployedImage.ref",
		"deployedImage.reference",
		"targetImage",
		"containerImage",
		"dockerImage",
		"imageRef",
		"imageReference",
		"repositoryTag",
		"tag",
		"ref",
		"reference",
	)
}

func formatCertColumn(row map[string]interface{}) string {
	title := firstTitlePath(
		row,
		"appCertTitle",
		"appCert.title",
		"appCert.name",
		"certTitle",
		"cert.title",
		"cert.name",
		"certificateTitle",
		"certificate.title",
		"certificate.name",
		"appCert.commonName",
		"cert.commonName",
		"certificate.commonName",
		"appCert.host",
		"appCert.hostname",
		"cert.host",
		"cert.hostname",
		"certificate.host",
		"certificate.hostname",
	)
	if title == "" {
		title = firstScalarPath(
			row,
			"appCertHost",
			"certHost",
			"certificateHost",
			"commonName",
		)
	}
	if title == "" {
		return ""
	}

	details := compactNonEmpty(formatCertStatusColumn(row), formatCertIssuerColumn(row))
	if len(details) == 0 {
		return title
	}
	return fmt.Sprintf("%s (%s)", title, strings.Join(details, ", "))
}

func formatCertStatusColumn(row map[string]interface{}) string {
	return firstScalarPath(row, "certStatus", "certificateStatus", "tlsStatus", "appCertStatus", "appCert.status", "cert.status", "certificate.status")
}

func formatCertIssuerColumn(row map[string]interface{}) string {
	return firstTitlePath(
		row,
		"certIssuer",
		"certificateIssuer",
		"issuerTitle",
		"issuer.title",
		"issuer.name",
		"issuer",
		"appCert.issuerTitle",
		"appCert.issuer.title",
		"appCert.issuer.name",
		"appCert.issuer",
		"cert.issuerTitle",
		"cert.issuer.title",
		"cert.issuer.name",
		"cert.issuer",
		"certificate.issuerTitle",
		"certificate.issuer.title",
		"certificate.issuer.name",
		"certificate.issuer",
	)
}

func certTimeColumnValue(row map[string]interface{}, column string) interface{} {
	paths := []string{column}
	switch column {
	case "certExpiresAt", "certificateExpiresAt":
		paths = append(paths,
			"certExpiresAt",
			"certificateExpiresAt",
			"cert.expiresAt",
			"certificate.expiresAt",
			"appCert.expiresAt",
			"cert.notAfter",
			"certificate.notAfter",
			"appCert.notAfter",
			"cert.validUntil",
			"certificate.validUntil",
			"appCert.validUntil",
		)
	case "expiresAt":
		paths = append(paths,
			"notAfter",
			"validUntil",
			"validTo",
			"certExpiresAt",
			"certificateExpiresAt",
			"appCert.expiresAt",
			"cert.expiresAt",
			"certificate.expiresAt",
			"appCert.notAfter",
			"cert.notAfter",
			"certificate.notAfter",
		)
	}
	return firstNonNilPath(row, paths...)
}

func formatDomainsColumn(row map[string]interface{}) string {
	value := firstNonNilPath(
		row,
		"domains",
		"domainNames",
		"dnsNames",
		"altNames",
		"subjectAltNames",
		"sans",
		"appCert.domains",
		"appCert.domainNames",
		"appCert.dnsNames",
		"appCert.altNames",
		"cert.domains",
		"cert.domainNames",
		"cert.dnsNames",
		"cert.altNames",
		"certificate.domains",
		"certificate.domainNames",
		"certificate.dnsNames",
		"certificate.altNames",
	)
	switch v := value.(type) {
	case []interface{}:
		values := make([]string, 0, len(v))
		for _, item := range v {
			if formatted := scalarString(item); formatted != "" {
				values = append(values, formatted)
			}
		}
		return strings.Join(values, ", ")
	case []string:
		return strings.Join(v, ", ")
	default:
		return scalarString(v)
	}
}

func firstNonNilPath(row map[string]interface{}, paths ...string) interface{} {
	for _, path := range paths {
		value := valueAtPath(row, path)
		if value != nil {
			return value
		}
	}
	return nil
}

func serviceLabels(value interface{}) []string {
	switch v := value.(type) {
	case []interface{}:
		labels := make([]string, 0, len(v))
		for _, item := range v {
			if label := serviceLabel(item); label != "" {
				labels = append(labels, label)
			}
		}
		return labels
	case []map[string]interface{}:
		labels := make([]string, 0, len(v))
		for _, item := range v {
			if label := serviceLabel(item); label != "" {
				labels = append(labels, label)
			}
		}
		return labels
	default:
		if label := serviceLabel(value); label != "" {
			return []string{label}
		}
		return nil
	}
}

func serviceLabel(value interface{}) string {
	m, ok := value.(map[string]interface{})
	if !ok {
		return scalarString(value)
	}
	return firstTitlePath(
		m,
		"title",
		"name",
		"serviceTitle",
		"service.title",
		"service.name",
		"catalogServiceTitle",
		"catalogService.title",
		"catalogService.name",
		"appServiceTitle",
		"appService.title",
		"appService.name",
		"builtServiceTitle",
		"builtService.title",
		"builtService.name",
		"deployedServiceTitle",
		"deployedService.title",
		"deployedService.name",
		"buildServiceTitle",
		"buildService.title",
		"buildService.name",
		"appServiceBuildTitle",
		"appServiceBuild.title",
		"appServiceBuild.name",
		"deploymentServiceTitle",
		"deploymentService.title",
		"deploymentService.name",
		"appServiceDeploymentTitle",
		"appServiceDeployment.title",
		"appServiceDeployment.name",
	)
}

func formatInstancesColumn(row map[string]interface{}) string {
	value := firstNonNilPath(row, "instances", "appInstances")
	if value == nil {
		return ""
	}

	labels := instanceLabels(value)
	if len(labels) == 0 {
		return "none"
	}
	return strings.Join(labels, ", ")
}

func instanceLabels(value interface{}) []string {
	switch v := value.(type) {
	case []interface{}:
		labels := make([]string, 0, len(v))
		for _, item := range v {
			if label := instanceLabel(item); label != "" {
				labels = append(labels, label)
			}
		}
		return labels
	case []map[string]interface{}:
		labels := make([]string, 0, len(v))
		for _, item := range v {
			if label := instanceLabel(item); label != "" {
				labels = append(labels, label)
			}
		}
		return labels
	default:
		if label := instanceLabel(value); label != "" {
			return []string{label}
		}
		return nil
	}
}

func instanceLabel(value interface{}) string {
	m, ok := value.(map[string]interface{})
	if !ok {
		return scalarString(value)
	}

	title := firstTitlePath(
		m,
		"title",
		"name",
		"appInstanceTitle",
		"appInstance.title",
		"appInstance.name",
		"instanceTitle",
		"instance.title",
		"instance.name",
	)
	if title == "" {
		title = firstScalarPath(m, "domain", "mainDomain")
	}
	if title == "" {
		title = firstScalarPath(m, "id", "appInstanceId", "appInstance.id", "instanceId", "instance.id")
	}
	if title == "" {
		return ""
	}

	id := firstScalarPath(m, "id", "appInstanceId", "appInstance.id", "instanceId", "instance.id")
	status := firstScalarPath(m, "status", "appInstance.status", "instance.status")
	details := make([]string, 0, 2)
	if id != "" && id != title {
		details = append(details, fmt.Sprintf("[%s]", id))
	}
	if status != "" {
		details = append(details, fmt.Sprintf("(%s)", status))
	}
	if len(details) == 0 {
		return title
	}
	return title + " " + strings.Join(details, " ")
}

func formatDBsColumn(row map[string]interface{}) string {
	value := firstNonNilPath(row, "dbs", "databaseDbs", "databaseDBs", "databaseDb")
	if value == nil {
		return ""
	}

	labels := databaseDBLabels(value)
	if len(labels) == 0 {
		return "none"
	}
	return strings.Join(labels, ", ")
}

func databaseDBLabels(value interface{}) []string {
	switch v := value.(type) {
	case []interface{}:
		labels := make([]string, 0, len(v))
		for _, item := range v {
			if label := databaseDBLabel(item); label != "" {
				labels = append(labels, label)
			}
		}
		return labels
	case []map[string]interface{}:
		labels := make([]string, 0, len(v))
		for _, item := range v {
			if label := databaseDBLabel(item); label != "" {
				labels = append(labels, label)
			}
		}
		return labels
	default:
		if label := databaseDBLabel(value); label != "" {
			return []string{label}
		}
		return nil
	}
}

func databaseDBLabel(value interface{}) string {
	m, ok := value.(map[string]interface{})
	if !ok {
		return scalarString(value)
	}
	return firstTitlePath(m, "name", "title", "databaseDb.name", "databaseDB.name", "db.name")
}

func formatIPsColumn(row map[string]interface{}) string {
	value := firstNonNilPath(row, "ips", "publicIps", "publicIPs", "publicIp", "publicIP", "publicIPAddress", "publicIpAddress", "publicAddress")
	switch v := value.(type) {
	case []interface{}:
		values := make([]string, 0, len(v))
		for _, item := range v {
			if formatted := scalarString(item); formatted != "" {
				values = append(values, formatted)
			}
		}
		return strings.Join(values, ", ")
	case []string:
		return strings.Join(v, ", ")
	default:
		return scalarString(v)
	}
}

func formatClusterNodesColumn(row map[string]interface{}) string {
	current := firstScalarPath(
		row,
		"currentNodeCount",
		"nodeCount",
		"nodesCount",
		"currentNodes",
		"nodesCurrent",
		"nodes.current",
		"nodes.currentCount",
		"node.current",
		"node.currentCount",
	)
	maximum := firstScalarPath(
		row,
		"maxNodeCount",
		"nodesMax",
		"maxNodes",
		"nodes.max",
		"nodes.maxCount",
		"node.max",
		"node.maxCount",
	)
	if current == "" && maximum == "" {
		if firstScalarPath(row, "singleNode", "single_node", "single-node") == "true" {
			return "1/1"
		}
		return ""
	}
	if current == "" {
		return maximum
	}
	if maximum == "" {
		return current
	}
	return current + "/" + maximum
}

func formatClusterScalableColumn(row map[string]interface{}) string {
	if scalable := firstScalarPath(row, "scalable", "autoscaling", "nodeAutoscaling"); scalable != "" {
		return scalable
	}
	singleNode := firstScalarPath(row, "singleNode", "single_node", "single-node")
	switch singleNode {
	case "true":
		return "false"
	case "false":
		return "true"
	default:
		return ""
	}
}

func formatAuthorColumn(row map[string]interface{}) string {
	for _, path := range []string{"author", "createdBy", "createdByUser", "createdByMembership", "createdByOrgMembership", "orgMembership", "membership", "user"} {
		if m, ok := valueAtPath(row, path).(map[string]interface{}); ok {
			if label := formatOrgMembershipLabel(m); label != "" {
				return label
			}
		}
	}

	name := firstTitlePath(
		row,
		"authorName",
		"author",
		"createdByName",
		"createdBy",
		"createdByUserName",
		"userName",
		"user",
	)
	email := firstScalarPath(
		row,
		"authorEmail",
		"author.email",
		"author.user.email",
		"createdByEmail",
		"createdBy.email",
		"createdBy.user.email",
		"createdByUser.email",
		"userEmail",
		"user.email",
	)
	return joinNameEmail(name, email)
}

func formatOrgMembershipLabel(row map[string]interface{}) string {
	name := firstTitlePath(
		row,
		"member",
		"name",
		"title",
		"fullName",
		"displayName",
		"user.name",
		"user.fullName",
		"user.displayName",
		"account.name",
		"profile.name",
	)
	email := firstScalarPath(row, "email", "user.email", "account.email", "profile.email")
	return joinNameEmail(name, email)
}

func joinNameEmail(name string, email string) string {
	switch {
	case name != "" && email != "" && name != email:
		return fmt.Sprintf("%s <%s>", name, email)
	case name != "":
		return name
	default:
		return email
	}
}

func formatProviderColumn(row map[string]interface{}) string {
	title := firstTitlePath(
		row,
		"providerTitle",
		"provider.title",
		"provider",
		"provider.name",
		"providerRev.providerTitle",
		"providerRev.provider.title",
		"providerRev.provider.name",
		"providerRevision.providerTitle",
		"providerRevision.provider.title",
		"providerRevision.provider.name",
		"providerRev.title",
		"providerRevision.title",
	)
	if title == "" {
		return ""
	}

	version := firstScalarPath(
		row,
		"providerVersion",
		"provider.version",
		"providerRev.version",
		"providerRevision.version",
		"providerRev.provider.version",
		"providerRevision.provider.version",
	)
	rev := firstScalarPath(
		row,
		"providerRev",
		"providerRevNumber",
		"providerRev.id",
		"providerRev.rev",
		"providerRev.number",
		"providerRev.revNumber",
		"providerRev.revision",
		"providerRevision.id",
		"providerRevision.rev",
		"providerRevision.number",
		"providerRevision.revNumber",
		"providerRevision.revision",
		"provider.rev",
		"provider.revId",
		"provider.revNumber",
		"provider.latestRevNumber",
		"provider.latestRevId",
	)

	return appendVersionRev(title, version, rev)
}

func appendVersionRev(title string, version string, rev string) string {
	details := make([]string, 0, 2)
	if version != "" {
		details = append(details, version)
	}
	if rev != "" {
		if !strings.HasPrefix(rev, "#") {
			rev = "#" + rev
		}
		details = append(details, rev)
	}
	if len(details) == 0 {
		return title
	}

	return fmt.Sprintf("%s (%s)", title, strings.Join(details, " "))
}

func firstScalarPath(row map[string]interface{}, paths ...string) string {
	for _, path := range paths {
		formatted := scalarString(valueAtPath(row, path))
		if formatted != "" {
			return formatted
		}
	}
	return ""
}

func firstTitlePath(row map[string]interface{}, paths ...string) string {
	for _, path := range paths {
		value := valueAtPath(row, path)
		formatted := scalarString(value)
		if formatted != "" && !isLikelyIDValue(value, formatted) {
			return formatted
		}
	}
	return ""
}

func firstRelationID(row map[string]interface{}, relation relationColumn) string {
	if id := firstScalarPath(row, relation.idPaths...); id != "" {
		return id
	}
	for _, path := range relation.idScalarPaths {
		value := valueAtPath(row, path)
		formatted := scalarString(value)
		if formatted != "" && isLikelyIDValue(value, formatted) {
			return formatted
		}
	}
	return ""
}

func valueAtPath(row map[string]interface{}, path string) interface{} {
	if row == nil || path == "" {
		return nil
	}

	var value interface{} = row
	for _, part := range strings.Split(path, ".") {
		m, ok := value.(map[string]interface{})
		if !ok {
			return nil
		}
		value = m[part]
	}
	return value
}

func firstScalar(values ...interface{}) string {
	for _, value := range values {
		formatted := scalarString(value)
		if formatted != "" {
			return formatted
		}
	}
	return ""
}

func scalarString(value interface{}) string {
	switch value.(type) {
	case nil, []interface{}, map[string]interface{}:
		return ""
	default:
		return strings.TrimSpace(formatValue(value))
	}
}

func isLikelyIDValue(value interface{}, formatted string) bool {
	switch value.(type) {
	case json.Number, float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	}

	formatted = strings.TrimSpace(formatted)
	if formatted == "" {
		return false
	}
	allDigits := true
	for _, char := range formatted {
		if !isDigitASCII(char) {
			allDigits = false
			break
		}
	}
	if allDigits {
		return true
	}
	if len(formatted) == 36 && formatted[8] == '-' && formatted[13] == '-' && formatted[18] == '-' && formatted[23] == '-' {
		for _, char := range formatted {
			if char == '-' {
				continue
			}
			if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') && !(char >= 'A' && char <= 'F') {
				return false
			}
		}
		return true
	}
	return false
}

func addQuery(query url.Values, name string, value string) {
	if value != "" {
		query.Set(name, value)
	}
}

func addBoolQuery(cmd *cobra.Command, query url.Values, name string, flag string) {
	if cmd.Flags().Changed(flag) {
		value, _ := cmd.Flags().GetBool(flag)
		query.Set(name, strconv.FormatBool(value))
	}
}

func addPagination(query url.Values, page int, pageSize int) {
	if page != 0 {
		query.Set("page", strconv.Itoa(page))
	}
	if pageSize != 0 {
		query.Set("pageSize", strconv.Itoa(pageSize))
	}
}

func addOptionalInt(values map[string]interface{}, key string, value string, flag string) error {
	if value == "" {
		return nil
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return errors.Wrapf(err, "invalid %s", flag)
	}
	values[key] = number
	return nil
}

func parseIntValues(values []string, flag string) ([]int, error) {
	result := make([]int, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			number, err := strconv.Atoi(part)
			if err != nil {
				return nil, errors.Wrapf(err, "invalid %s", flag)
			}
			result = append(result, number)
		}
	}
	return result, nil
}

func addOptionalString(values map[string]interface{}, key string, value string) {
	if value != "" {
		values[key] = value
	}
}

func requireFlag(value string, name string) error {
	if value == "" {
		return errors.Errorf("%s is required", name)
	}
	return nil
}

func requireIntFlag(value int, name string) error {
	if value == 0 {
		return errors.Errorf("%s is required", name)
	}
	return nil
}

func inferOrgID(ctx context.Context, client *rest.Client, explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}

	var orgs interface{}
	if err := client.Get(ctx, "/orgs", nil, &orgs); err != nil {
		return "", err
	}

	rows := asRows(orgs)
	if len(rows) == 1 {
		return formatValue(rows[0]["id"]), nil
	}
	if len(rows) == 0 {
		return "", errors.New("no organization is available for the current credentials")
	}

	return "", errors.New("multiple organizations are available; pass --org explicitly")
}

func confirm(cmd *cobra.Command, yes bool, message string) error {
	if yes {
		return nil
	}
	fmt.Fprint(cmd.OutOrStdout(), message+" [y/N] ")
	var answer string
	if _, err := fmt.Fscan(cmd.InOrStdin(), &answer); err != nil {
		return errors.WithStack(err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		return errors.New("aborted")
	}
	return nil
}

func bodyFromMap(values map[string]interface{}) interface{} {
	body := make(map[string]interface{})
	for key, value := range values {
		if value != nil && value != "" {
			body[key] = value
		}
	}
	return body
}

func changedBool(cmd *cobra.Command, name string) (bool, bool) {
	if !cmd.Flags().Changed(name) {
		return false, false
	}
	value, _ := cmd.Flags().GetBool(name)
	return value, true
}

func changedInt(cmd *cobra.Command, name string) (int, bool) {
	if !cmd.Flags().Changed(name) {
		return 0, false
	}
	value, _ := cmd.Flags().GetInt(name)
	return value, true
}

func changedString(cmd *cobra.Command, name string) (string, bool) {
	if !cmd.Flags().Changed(name) {
		return "", false
	}
	value, _ := cmd.Flags().GetString(name)
	return value, true
}

func hasChangedFlags(flags *pflag.FlagSet, names ...string) bool {
	for _, name := range names {
		if flags.Changed(name) {
			return true
		}
	}
	return false
}

func firstID(value interface{}) string {
	m, ok := value.(map[string]interface{})
	if !ok {
		return ""
	}
	return formatValue(m["id"])
}

func firstTaskID(value interface{}) string {
	m, ok := value.(map[string]interface{})
	if !ok {
		return ""
	}
	return firstScalarPath(m, "taskId", "task.id", "task")
}

func resourceOrOperationColumns(value interface{}, resourceColumns []string) []string {
	m, ok := value.(map[string]interface{})
	if !ok {
		return resourceColumns
	}
	if _, ok := m["taskId"]; ok {
		return operationColumns
	}
	if _, ok := m["task"]; ok {
		return operationColumns
	}
	if _, ok := m["success"]; ok {
		return operationColumns
	}
	return resourceColumns
}
