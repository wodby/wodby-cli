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
	titlePaths        []string
	allowNumericTitle bool
}

var relationColumns = map[string]relationColumn{
	"app": {
		title:         "app",
		objectKey:     "app",
		idTitle:       "app id",
		idPaths:       []string{"appId", "app.id"},
		idScalarPaths: []string{"app"},
		pathPrefix:    "/apps/",
		titlePaths:    []string{"appTitle", "app.title", "app", "appName", "app.name"},
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
	"service": {
		title:         "service",
		objectKey:     "appService",
		idTitle:       "service id",
		idPaths:       []string{"appServiceId", "appService.id", "serviceId", "service.id"},
		idScalarPaths: []string{"appService", "service"},
		pathPrefix:    "/app-services/",
		titlePaths:    []string{"appServiceTitle", "appService.title", "serviceTitle", "service.title", "appService", "service", "appServiceName", "appService.name", "serviceName", "service.name"},
	},
	"instance": {
		title:         "instance",
		objectKey:     "appInstance",
		idTitle:       "instance id",
		idPaths:       []string{"appInstanceId", "appInstance.id", "instanceId", "instance.id"},
		idScalarPaths: []string{"appInstance", "instance"},
		pathPrefix:    "/app-instances/",
		titlePaths:    []string{"appInstanceTitle", "appInstance.title", "instanceTitle", "instance.title", "appInstance", "instance", "appInstanceName", "appInstance.name", "instanceName", "instance.name"},
	},
	"database": {
		title:         "database",
		objectKey:     "database",
		idTitle:       "database id",
		idPaths:       []string{"databaseId", "database.id"},
		idScalarPaths: []string{"database"},
		pathPrefix:    "/databases/",
		titlePaths:    []string{"databaseTitle", "database.title", "database", "databaseName", "database.name"},
	},
	"databaseDb": {
		title:         "database db",
		idTitle:       "database db id",
		idPaths:       []string{"databaseDbId", "databaseDb.id", "databaseDBId", "databaseDB.id", "dbId", "db.id"},
		idScalarPaths: []string{"databaseDb", "databaseDB", "db"},
		titlePaths:    []string{"databaseDbTitle", "databaseDb.title", "databaseDBTitle", "databaseDB.title", "dbTitle", "db.title", "databaseDb", "databaseDB", "db", "databaseDbName", "databaseDb.name", "databaseDBName", "databaseDB.name", "dbName", "db.name"},
	},
	"port": {
		title:             "port",
		idTitle:           "port id",
		idPaths:           []string{"portId", "port.id"},
		titlePaths:        []string{"portNumber", "port.number", "port", "portName", "port.name", "portTitle", "port.title", "port.port"},
		allowNumericTitle: true,
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
}

func addOutputFlag(cmd *cobra.Command, opts *outputOptions) {
	cmd.PersistentFlags().StringVarP(&opts.output, "output", "o", outputTable, "Output format: table, vertical, or json")
}

func addWaitFlags(cmd *cobra.Command, opts *waitOptions) {
	cmd.Flags().BoolVar(&opts.wait, "wait", false, "Wait for the created task or deployment to finish")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", 10*time.Minute, "Maximum time to wait")
}

func addBodyFlags(cmd *cobra.Command, opts *bodyOptions) {
	cmd.Flags().StringVar(&opts.data, "data", "", "JSON request body")
	cmd.Flags().StringVarP(&opts.file, "file", "f", "", "Path to JSON request body")
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
	switch opts.output {
	case outputJSON:
		return printJSON(cmd, value)
	case outputTable:
		printTable(cmd, normalizeItems(value), columns)
		return nil
	case outputVertical:
		printVerticalTable(cmd, normalizeItems(value), columns, false)
		return nil
	default:
		return errors.Errorf("unsupported output format %q", opts.output)
	}
}

func printGetResult(cmd *cobra.Command, opts outputOptions, value interface{}, columns []string) error {
	switch opts.output {
	case outputJSON:
		return printJSON(cmd, value)
	case outputTable, outputVertical:
		printVerticalTable(cmd, normalizeItem(value), columns, true)
		return nil
	default:
		return errors.Errorf("unsupported output format %q", opts.output)
	}
}

func printClientResult(cmd *cobra.Command, client *rest.Client, opts outputOptions, value interface{}, columns []string) error {
	items := normalizeItems(value)
	if opts.output != outputJSON && isCollection(items) {
		if err := enrichDisplayRelations(cmd.Context(), client, items, columns); err != nil {
			return err
		}
	}
	return printResult(cmd, opts, value, columns)
}

func printClientGetResult(cmd *cobra.Command, client *rest.Client, opts outputOptions, value interface{}, columns []string) error {
	if opts.output != outputJSON {
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
	cmd.Println(string(content))
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
		if isProviderColumn(column) {
			for _, row := range rows {
				enrichProviderRelation(ctx, client, row, cache, providersByRev, &providersLoaded)
			}
			continue
		}

		relation, ok := displayRelationFor(column)
		if !ok || relation.pathPrefix == "" {
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

			related, ok := fetchDisplayRelation(ctx, client, cache, relation.pathPrefix, id)
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
			values = append(values, formatColumnValue(row, column))
		}
		fmt.Fprintln(writer, strings.Join(values, "\t"))
	}
	_ = writer.Flush()
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
	default:
		if relation, ok := relationColumnFor(column); ok {
			return formatRelationColumn(row, relation)
		}
		return formatValue(row[column])
	}
}

func relationColumnFor(column string) (relationColumn, bool) {
	switch column {
	case "app", "appId":
		return relationColumns["app"], true
	case "env", "envId", "environment", "environmentId":
		return relationColumns["env"], true
	case "cluster", "clusterId":
		return relationColumns["cluster"], true
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
	case "task", "taskId":
		return relationColumns["task"], true
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

func formatIntegrationColumn(row map[string]interface{}) string {
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
	cmd.Print(message + " [y/N] ")
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
	return formatValue(m["taskId"])
}
