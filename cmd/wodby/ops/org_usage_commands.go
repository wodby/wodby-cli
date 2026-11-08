package ops

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wodby/wodby-cli/pkg/api/rest"
	"github.com/wodby/wodby-cli/pkg/types"
)

var (
	orgUsageColumns       = []string{"projectId", "project", "item", "quantity", "unit", "amount", "invoiceShare"}
	orgUsagePeriodColumns = []string{"start", "end", "current"}
)

// orgUsageOptions are the flags the usage report commands share.
type orgUsageOptions struct {
	orgID       string
	periodStart string
}

// newOrgUsageCommand shows an organization's usage per project and billing item in one billing period. It is meant for
// the org command, whose --output flag it uses.
func newOrgUsageCommand() *cobra.Command {
	out := outputOptions{output: outputTable}
	opts := orgUsageOptions{}
	cmd := &cobra.Command{
		Use:   "usage",
		Short: "Show the organization's usage by project in a billing period",
		Long: "Show the organization's usage per project and billing item in a billing period, at public list prices in " +
			"USD before included usage and discounts. For a past period with final invoices, the invoice share splits " +
			"what the invoices charged between projects. Requires billing access to the organization.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, path, query, err := orgUsageRequest(cmd, opts, "")
			if err != nil {
				return err
			}
			var report map[string]interface{}
			if err := client.Get(cmd.Context(), path, query, &report); err != nil {
				return err
			}
			return printOrgUsageReport(cmd, out, report)
		},
	}
	cmd.PersistentFlags().StringVar(&opts.orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	addOrgUsagePeriodFlag(cmd, &opts)

	periods := &cobra.Command{
		Use:   "periods",
		Short: "List the billing periods in the usage report, newest first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, path, _, err := orgUsageRequest(cmd, orgUsageOptions{orgID: opts.orgID}, "/periods")
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), path, nil, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, orgUsagePeriodColumns)
		},
	}

	csvOpts := orgUsageOptions{}
	var saveTo string
	csv := &cobra.Command{
		Use:   "csv",
		Short: "Export the usage report as CSV to standard output, or to a file with --save-to",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, path, query, err := orgUsageRequest(cmd, orgUsageOptions{orgID: opts.orgID, periodStart: csvOpts.periodStart}, "/csv")
			if err != nil {
				return err
			}
			content, err := getRawAPIContent(cmd, path, query, "text/csv")
			if err != nil {
				return err
			}
			if saveTo == "" {
				_, err := cmd.OutOrStdout().Write(content)
				return errors.WithStack(err)
			}
			if err := os.WriteFile(saveTo, content, 0o644); err != nil {
				return errors.WithStack(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Saved %d bytes to %s\n", len(content), saveTo)
			return nil
		},
	}
	addOrgUsagePeriodFlag(csv, &csvOpts)
	csv.Flags().StringVar(&saveTo, "save-to", "", "Save the CSV to this path")

	cmd.AddCommand(periods, csv)
	return cmd
}

// addOrgUsagePeriodFlag adds --period-start, which picks a billing period from usage periods.
func addOrgUsagePeriodFlag(cmd *cobra.Command, opts *orgUsageOptions) {
	cmd.Flags().StringVar(&opts.periodStart, "period-start", "", "Start of the billing period as usage periods lists it, in RFC 3339; defaults to the current period")
}

// orgUsageRequest returns a client and the path and query of the organization's usage route ending in suffix.
func orgUsageRequest(cmd *cobra.Command, opts orgUsageOptions, suffix string) (*rest.Client, string, url.Values, error) {
	query := url.Values{}
	if opts.periodStart != "" {
		if _, err := time.Parse(time.RFC3339Nano, opts.periodStart); err != nil {
			return nil, "", nil, errors.Errorf("invalid --period-start %q: use an RFC 3339 time as usage periods lists it", opts.periodStart)
		}
		query.Set("periodStart", opts.periodStart)
	}
	client, err := newRESTClient()
	if err != nil {
		return nil, "", nil, err
	}
	orgID, err := inferOrgID(cmd.Context(), client, opts.orgID)
	if err != nil {
		return nil, "", nil, err
	}
	return client, escapedPath("/orgs/%s/usage", orgID) + suffix, query, nil
}

// printOrgUsageReport prints the report: JSON as the API returned it, otherwise the period and totals followed by one
// row per project and billing item.
func printOrgUsageReport(cmd *cobra.Command, out outputOptions, report map[string]interface{}) error {
	output := outputFormat(cmd, out)
	switch output {
	case outputJSON:
		return printJSON(cmd, report)
	case outputTable, outputVertical:
		w := cmd.OutOrStdout()
		period, _ := report["period"].(map[string]interface{})
		current := ""
		if truthyPath(period, "current") {
			current = " (current)"
		}
		fmt.Fprintf(w, "Period:    %s to %s%s\n", formatValue(period["start"]), formatValue(period["end"]), current)
		fmt.Fprintf(w, "Amount:    %s USD at list prices\n", formatOrgUsageAmount(report["amount"]))
		invoiced := "unknown"
		if report["invoiceAmount"] != nil {
			invoiced = formatOrgUsageAmount(report["invoiceAmount"]) + " USD"
		}
		fmt.Fprintf(w, "Invoiced:  %s\n\n", invoiced)
		rows := orgUsageRows(report)
		if output == outputVertical {
			printVerticalTable(cmd, rows, orgUsageColumns, false)
		} else {
			printTable(cmd, rows, orgUsageColumns)
		}
		return nil
	default:
		return errors.Errorf("unsupported output format %q", output)
	}
}

// orgUsageRows flattens the report into one row per project and billing item, with the amounts rounded for display.
func orgUsageRows(report map[string]interface{}) []map[string]interface{} {
	rows := []map[string]interface{}{}
	for _, project := range asRows(report["projects"]) {
		label := firstScalarPath(project, "title", "name")
		if project["projectId"] == nil {
			label = "Organization"
		}
		if truthyPath(project, "deleted") {
			label += " (deleted)"
		}
		items := asRows(project["items"])
		if len(items) == 0 {
			items = []map[string]interface{}{{"amount": project["amount"], "invoiceShare": project["invoiceShare"]}}
		}
		for _, item := range items {
			rows = append(rows, map[string]interface{}{
				"projectId":    formatValue(project["projectId"]),
				"project":      label,
				"item":         firstScalarPath(item, "title", "item"),
				"quantity":     formatOrgUsageNumber(item["quantity"], 2),
				"unit":         formatValue(item["unit"]),
				"amount":       formatOrgUsageAmount(item["amount"]),
				"invoiceShare": formatOrgUsageAmount(item["invoiceShare"]),
			})
		}
	}
	return rows
}

// formatOrgUsageAmount shows a USD amount with two decimals, and leaves other values as they are.
func formatOrgUsageAmount(value interface{}) string {
	number, err := strconv.ParseFloat(formatValue(value), 64)
	if err != nil {
		return formatValue(value)
	}
	return strconv.FormatFloat(number, 'f', 2, 64)
}

// formatOrgUsageNumber rounds a number to at most decimals digits after the point, and leaves other values as they are.
func formatOrgUsageNumber(value interface{}, decimals int) string {
	number, err := strconv.ParseFloat(formatValue(value), 64)
	if err != nil {
		return formatValue(value)
	}
	formatted := strconv.FormatFloat(number, 'f', decimals, 64)
	if strings.Contains(formatted, ".") {
		formatted = strings.TrimRight(strings.TrimRight(formatted, "0"), ".")
	}
	return formatted
}

// getRawAPIContent sends an authenticated GET to the API and returns the response body as it is, for routes that do
// not return JSON. Errors are reported like those of other API requests.
func getRawAPIContent(cmd *cobra.Command, path string, query url.Values, accept string) ([]byte, error) {
	if _, err := newRESTClient(); err != nil {
		return nil, err
	}
	capture := &rawAPIBodyTransport{next: http.DefaultTransport, accept: accept}
	client, err := rest.NewClient(types.APIConfig{
		Key:         viper.GetString("api_key"),
		AccessToken: viper.GetString("access_token"),
		Endpoint:    apiBaseURL(),
		Transport:   capture,
	})
	if err != nil {
		return nil, err
	}
	var ignored interface{}
	if err := client.Get(cmd.Context(), path, query, &ignored); err != nil {
		return nil, err
	}
	return capture.body, nil
}

// rawAPIBodyTransport keeps the body of a successful API response and gives the REST client an empty JSON body in its
// place, so a non-JSON download reuses the client's authentication, redirect checks and error reporting.
type rawAPIBodyTransport struct {
	next   http.RoundTripper
	accept string
	body   []byte
}

// RoundTrip sends the request with the wanted content type and keeps a successful response's body.
func (t *rawAPIBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Accept", t.accept)
	resp, err := t.next.RoundTrip(req)
	if err != nil || resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return resp, err
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, errors.WithStack(err)
	}
	t.body = body
	resp.Body = io.NopCloser(strings.NewReader("null"))
	resp.ContentLength = int64(len("null"))
	resp.Header.Del("Content-Length")
	return resp, nil
}
