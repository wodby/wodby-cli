package ops

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	appServiceCustomLinkValueColumns = []string{"link", "name", "secret", "hasValue", "value"}
	appServiceLinkCandidateColumns   = []string{"id", "appServiceName", "appServiceTitle", "crossApp", "appTitle", "appInstanceTitle"}
)

// newAppServiceCustomLinkCommand lists, sets and removes the custom targets of app service links: values entered by
// hand instead of another app service.
func newAppServiceCustomLinkCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "custom-link",
		Aliases: []string{"custom-links"},
		Short:   "Manage app service links with custom values instead of another app service",
	}
	list := newAppServiceCustomLinkListCommand(out)
	defaultToList(cmd, list)
	cmd.AddCommand(
		list,
		newAppServiceCustomLinkSetCommand(out),
		newAppServiceCustomLinkDeleteCommand(out),
	)
	return cmd
}

// newAppServiceCustomLinkListCommand lists an app service's custom links, one row per value. Secret values are never
// returned by the API, so they show as empty with hasValue telling whether one is stored.
func newAppServiceCustomLinkListCommand(out outputOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list APP_SERVICE_ID",
		Short: "List an app service's custom links and their values",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), escapedPath("/app-services/%s/custom-links", args[0]), nil, &result); err != nil {
				return err
			}
			if outputFormat(cmd, out) == outputJSON {
				return printJSON(cmd, result)
			}
			return printResult(cmd, out, appServiceCustomLinkValueRows(result), appServiceCustomLinkValueColumns)
		},
	}
}

// appServiceCustomLinkValueRows flattens custom links into one row per value, labelled with the link name.
func appServiceCustomLinkValueRows(value interface{}) []map[string]interface{} {
	rows := []map[string]interface{}{}
	for _, link := range asRows(value) {
		for _, item := range asRows(link["values"]) {
			row := map[string]interface{}{"link": link["name"]}
			for key, value := range item {
				row[key] = value
			}
			rows = append(rows, row)
		}
	}
	return rows
}

// newAppServiceCustomLinkSetCommand gives a link custom values. The values are the complete set: every value that is
// not optional must be given each time, and an optional value left out is cleared.
func newAppServiceCustomLinkSetCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var values []string
	cmd := &cobra.Command{
		Use:   "set APP_SERVICE_ID NAME",
		Short: "Point a link at custom values instead of another app service",
		Long: "Point a link at custom values instead of another app service. Give the complete set of values each " +
			"time, secret ones included; an optional value left out is cleared. Use --file to keep secret values " +
			"out of the shell history.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if len(values) == 0 {
					return errors.New("--value is required")
				}
				parsed, err := parseNameValueInputs(values, "--value")
				if err != nil {
					return err
				}
				requestBody = map[string]interface{}{"values": parsed}
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), escapedPath("/app-services/%s/custom-links/%s", args[0], args[1]), nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, operationColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringArrayVar(&values, "value", nil, "Link value as NAME=VALUE; repeat for every value")
	return cmd
}

// newAppServiceCustomLinkDeleteCommand removes the custom target of a link after confirmation.
func newAppServiceCustomLinkDeleteCommand(out outputOptions) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete APP_SERVICE_ID NAME",
		Short: "Remove the custom values of a link",
		Long:  "Remove the custom values of a link. A required link cannot be removed.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := confirm(cmd, yes, "Remove the custom values of link "+args[1]+"?"); err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Delete(cmd.Context(), escapedPath("/app-services/%s/custom-links/%s", args[0], args[1]), nil, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, operationColumns)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm without prompting")
	return cmd
}

// newAppServiceLinkCandidatesCommand lists the app services a named link of an app service may point at, including
// services of other app environments when the link accepts them.
func newAppServiceLinkCandidatesCommand(out outputOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "link-candidates APP_SERVICE_ID LINK_NAME",
		Short: "List the app services a link may point at",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), escapedPath("/app-services/%s/links/%s/candidates", args[0], args[1]), nil, &result); err != nil {
				return err
			}
			if outputFormat(cmd, out) == outputJSON {
				return printJSON(cmd, result)
			}
			rows := asRows(result)
			// The ID column shows the app service ID to pass to link set --linked-service.
			for _, row := range rows {
				row["id"] = row["appServiceId"]
			}
			return printResult(cmd, out, rows, appServiceLinkCandidateColumns)
		},
	}
}
