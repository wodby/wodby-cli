package ops

import (
	"fmt"

	"github.com/spf13/cobra"
)

var stackOriginSyncServiceUpdateColumns = []string{"stackServiceId", "name", "title", "serviceTitle", "previousVersion", "version", "reason", "refusal"}

// newStackPreviewSyncOriginCommand shows what sync-origin would do with the same options, without changing the stack.
func newStackPreviewSyncOriginCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	cmd := &cobra.Command{
		Use:   "preview-sync-origin ID",
		Short: "Preview what syncing the stack with its origin would do",
		Long: "Preview what sync-origin with the same options would do, without changing the stack: the stack services " +
			"it has to update to the service version the origin uses (sync-origin --update-services lets it), the error " +
			"it would fail with, and the changes its task log would list.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				requestBody = changedStackSyncOptions(cmd)
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), escapedPath("/stacks/%s/actions/preview-sync-origin", args[0]), nil, requestBody, &result); err != nil {
				return err
			}
			if outputFormat(cmd, out) == outputJSON {
				return printJSON(cmd, result)
			}
			printStackOriginSyncPreview(cmd, result)
			return nil
		},
	}
	addBodyFlags(cmd, &body)
	addStackSyncFlags(cmd)
	return cmd
}

// printStackOriginSyncPreview prints the version change, the error the sync would fail with, the stack services it has
// to update and the changes it would make. The API returns null when the stack already follows the origin's latest
// revision.
func printStackOriginSyncPreview(cmd *cobra.Command, result interface{}) {
	w := cmd.OutOrStdout()
	preview, _ := result.(map[string]interface{})
	if preview == nil {
		fmt.Fprintln(w, "The stack already follows the origin's latest revision.")
		return
	}
	fmt.Fprintf(w, "Version: %s -> %s\n", formatValue(preview["previousVersion"]), formatValue(preview["version"]))
	if message := formatValue(preview["error"]); message != "" {
		fmt.Fprintf(w, "Error: %s\n", message)
	}
	if updates := asRows(preview["serviceUpdates"]); len(updates) > 0 {
		fmt.Fprintln(w, "\nStack services the sync has to update (sync-origin --update-services):")
		printTable(cmd, preview["serviceUpdates"], stackOriginSyncServiceUpdateColumns)
	}
	if changes := asRows(preview["changes"]); len(changes) > 0 {
		fmt.Fprintln(w, "\nChanges:")
		for _, change := range changes {
			fmt.Fprintf(w, "  %s\n", formatValue(change["message"]))
		}
	}
}
