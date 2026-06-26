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
	outputTable = "table"
	outputJSON  = "json"
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

func addOutputFlag(cmd *cobra.Command, opts *outputOptions) {
	cmd.PersistentFlags().StringVarP(&opts.output, "output", "o", outputTable, "Output format: table or json")
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
		content, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return errors.WithStack(err)
		}
		cmd.Println(string(content))
		return nil
	case outputTable:
		printTable(cmd, normalizeItems(value), columns)
		return nil
	default:
		return errors.Errorf("unsupported output format %q", opts.output)
	}
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
	fmt.Fprintln(writer, strings.Join(columns, "\t"))
	for _, row := range rows {
		values := make([]string, 0, len(columns))
		for _, column := range columns {
			values = append(values, formatValue(row[column]))
		}
		fmt.Fprintln(writer, strings.Join(values, "\t"))
	}
	_ = writer.Flush()
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
