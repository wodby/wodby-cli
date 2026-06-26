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
	title      string
	idTitle    string
	idPaths    []string
	titlePaths []string
}

var relationColumns = map[string]relationColumn{
	"app": {
		title:      "app",
		idTitle:    "app id",
		idPaths:    []string{"appId", "app.id"},
		titlePaths: []string{"appTitle", "app.title", "app", "appName", "app.name"},
	},
	"env": {
		title:      "env",
		idTitle:    "env id",
		idPaths:    []string{"envId", "env.id", "environmentId", "environment.id"},
		titlePaths: []string{"envTitle", "env.title", "environmentTitle", "environment.title", "env", "environment", "envName", "env.name", "environment.name"},
	},
	"cluster": {
		title:      "cluster",
		idTitle:    "cluster id",
		idPaths:    []string{"clusterId", "cluster.id"},
		titlePaths: []string{"clusterTitle", "cluster.title", "cluster", "clusterName", "cluster.name"},
	},
	"service": {
		title:      "service",
		idTitle:    "service id",
		idPaths:    []string{"appServiceId", "appService.id", "serviceId", "service.id"},
		titlePaths: []string{"appServiceTitle", "appService.title", "serviceTitle", "service.title", "appService", "service", "appServiceName", "appService.name", "serviceName", "service.name"},
	},
	"instance": {
		title:      "instance",
		idTitle:    "instance id",
		idPaths:    []string{"appInstanceId", "appInstance.id", "instanceId", "instance.id"},
		titlePaths: []string{"appInstanceTitle", "appInstance.title", "instanceTitle", "instance.title", "appInstance", "instance", "appInstanceName", "appInstance.name", "instanceName", "instance.name"},
	},
	"database": {
		title:      "database",
		idTitle:    "database id",
		idPaths:    []string{"databaseId", "database.id"},
		titlePaths: []string{"databaseTitle", "database.title", "database", "databaseName", "database.name"},
	},
	"databaseDb": {
		title:      "database db",
		idTitle:    "database db id",
		idPaths:    []string{"databaseDbId", "databaseDb.id", "databaseDBId", "databaseDB.id", "dbId", "db.id"},
		titlePaths: []string{"databaseDbTitle", "databaseDb.title", "databaseDBTitle", "databaseDB.title", "dbTitle", "db.title", "databaseDb", "databaseDB", "db", "databaseDbName", "databaseDb.name", "databaseDBName", "databaseDB.name", "dbName", "db.name"},
	},
	"port": {
		title:      "port",
		idTitle:    "port id",
		idPaths:    []string{"portId", "port.id"},
		titlePaths: []string{"portNumber", "port.number", "port", "portName", "port.name", "portTitle", "port.title", "port.port"},
	},
	"task": {
		title:      "task",
		idTitle:    "task id",
		idPaths:    []string{"taskId", "task.id"},
		titlePaths: []string{"taskTitle", "task.title", "task", "taskName", "task.name"},
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
		printVerticalTable(cmd, value, columns, true)
		return nil
	default:
		return errors.Errorf("unsupported output format %q", opts.output)
	}
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
	items, ok := m["items"]
	if !ok {
		return value
	}
	return items
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

func printVerticalTable(cmd *cobra.Command, value interface{}, columns []string, inlineRelationIDs bool) {
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
			displayValue := value
			if inlineRelationIDs {
				displayValue = appendInlineRelationID(row, column, value)
			}
			fmt.Fprintf(writer, "%s:\t%s\n", title, displayValue)
			for _, extra := range verticalExtraRows(row, column, value) {
				fmt.Fprintf(writer, "%s:\t%s\n", extra.title, extra.value)
			}
		}
	}
	_ = writer.Flush()
}

func appendInlineRelationID(row map[string]interface{}, column string, displayedValue string) string {
	if displayedValue == "" {
		return displayedValue
	}

	id := inlineRelationID(row, column)
	if id == "" || id == displayedValue || strings.Contains(displayedValue, "(id:") {
		return displayedValue
	}

	return fmt.Sprintf("%s (id: %s)", displayedValue, id)
}

func inlineRelationID(row map[string]interface{}, column string) string {
	switch column {
	case "integration", "integrationId":
		return firstScalarPath(row, "integrationId", "integration.id")
	case "provider", "providerId", "providerRevId":
		return firstScalarPath(row, "providerId", "provider.id", "providerRev.providerId", "providerRev.provider.id", "providerRevision.providerId", "providerRevision.provider.id", "providerRevId", "providerRev.id", "providerRevision.id")
	default:
		relation, ok := relationColumnFor(column)
		if !ok {
			return ""
		}
		return firstScalarPath(row, relation.idPaths...)
	}
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
	value := firstScalarPath(row, relation.idPaths...)
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
		return column
	}
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
	value := firstScalarPath(row, relation.titlePaths...)
	if value != "" {
		return value
	}
	return firstScalarPath(row, relation.idPaths...)
}

func asRows(value interface{}) []map[string]interface{} {
	switch v := value.(type) {
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
	title := firstScalar(
		row["integrationTitle"],
		valueAtPath(row, "integration.title"),
		row["integration"],
		valueAtPath(row, "integration.name"),
	)
	if title == "" {
		return firstScalar(row["integrationId"])
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
	title := firstScalarPath(
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
		return firstScalar(row["providerId"], row["providerRevId"])
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
		"providerRev.rev",
		"providerRev.number",
		"providerRev.revNumber",
		"providerRev.revision",
		"providerRevision.rev",
		"providerRevision.number",
		"providerRevision.revNumber",
		"providerRevision.revision",
		"provider.rev",
		"provider.revNumber",
		"provider.latestRevNumber",
		"providerRevId",
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
