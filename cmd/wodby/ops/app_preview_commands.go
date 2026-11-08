package ops

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/wodby/wodby-cli/pkg/api/rest"
)

var (
	appPreviewSettingsColumns    = []string{"enabled", "baseAppEnvironmentId", "environmentType", "imports", "buildDrafts", "runCronJobs", "maxConcurrent", "idleTtlDays", "basicAuth", "basicAuthUsername", "previewsAvailable"}
	appPreviewPullRequestColumns = []string{"number", "title", "author", "headRef", "draft", "fromFork", "hasPreview", "previewAppEnvironmentId", "skipReason"}
)

// appPreviewSettingsInputKeys are the preview settings fields the update API accepts. Read-only fields of the stored
// settings, such as the basic auth username, are left out.
var appPreviewSettingsInputKeys = []string{"enabled", "baseAppEnvironmentId", "environmentType", "imports", "buildDrafts", "runCronJobs", "maxConcurrent", "idleTtlDays", "basicAuth"}

// appPreviewSettingsSpecs are the flags that set single pull request preview settings.
func appPreviewSettingsSpecs() []jsonFlagSpec {
	return []jsonFlagSpec{
		boolJSONFlag("previews", "enabled", "Turn pull request preview environments on; --previews=false turns them off", false),
		intJSONFlag("preview-base", "baseAppEnvironmentId", "ID of the base app environment; previews are made for pull requests into the branch it tracks", false),
		stringJSONFlag("preview-environment-type", "environmentType", "Environment type of previews: test, staging, dev or feature", false),
		boolJSONFlag("preview-build-drafts", "buildDrafts", "Create previews for draft pull requests too; --preview-build-drafts=false skips them", false),
		boolJSONFlag("preview-run-cron-jobs", "runCronJobs", "Run the base environment's cron jobs in previews; --preview-run-cron-jobs=false copies them disabled", false),
		intJSONFlag("preview-max", "maxConcurrent", "Maximum number of open preview environments; pull requests beyond it are skipped", false),
		intJSONFlag("preview-idle-days", "idleTtlDays", "Days without activity after which a preview is deleted; 0 never deletes idle previews", false),
		boolJSONFlag("preview-basic-auth", "basicAuth", "Protect previews with basic auth and one shared generated password; --preview-basic-auth=false turns it off", false),
	}
}

// newAppSettingsCommand shows and updates app-wide settings, which hold the pull request preview environment settings.
func newAppSettingsCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "settings", Short: "Manage app settings such as pull request preview environments"}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "get APP_ID",
			Short: "Show the app's pull request preview environment settings",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				client, err := newRESTClient()
				if err != nil {
					return err
				}
				app, err := getAppForPreviewSettings(cmd, client, args[0])
				if err != nil {
					return err
				}
				return printAppPreviewSettings(cmd, out, app, true)
			},
		},
		newAppSettingsUpdateCommand(out),
	)
	return cmd
}

// newAppSettingsUpdateCommand updates the app's preview environment settings. The API replaces the whole settings
// object, so settings not given as flags keep their current values.
func newAppSettingsUpdateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var imports []string
	var clearImports bool
	specs := appPreviewSettingsSpecs()
	cmd := &cobra.Command{
		Use:   "update APP_ID",
		Short: "Update the app's pull request preview environment settings",
		Long: "Update the app's pull request preview environment settings. Settings not given as flags keep their " +
			"current values. Enabling preview environments requires an active paid subscription.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if !hasChangedFlags(cmd.Flags(), "previews", "preview-base", "preview-environment-type", "preview-import", "clear-preview-imports",
					"preview-build-drafts", "preview-run-cron-jobs", "preview-max", "preview-idle-days", "preview-basic-auth") {
					return errors.New("pass at least one update flag or provide --data/--file")
				}
				if clearImports && len(imports) != 0 {
					return errors.New("use either --preview-import or --clear-preview-imports, not both")
				}
				values, err := bodyFromJSONFlagSpecs(cmd, specs, false)
				if err != nil {
					return err
				}
				if len(imports) != 0 {
					parsed, err := parseAppPreviewImports(imports)
					if err != nil {
						return err
					}
					values["imports"] = parsed
				} else if clearImports {
					values["imports"] = []interface{}{}
				}
				app, err := getAppForPreviewSettings(cmd, client, args[0])
				if err != nil {
					return err
				}
				fillAppPreviewSettingsDefaults(app, values)
				if _, ok := values["enabled"]; !ok {
					return errors.New("--previews is required because the app has no preview settings yet")
				}
				requestBody = map[string]interface{}{"previewEnvironments": values}
			}
			var result interface{}
			if err := client.Put(cmd.Context(), escapedPath("/apps/settings/%s", args[0]), nil, requestBody, &result); err != nil {
				return err
			}
			return printAppPreviewSettings(cmd, out, result, false)
		},
	}
	addBodyFlags(cmd, &body)
	addJSONFlagSpecs(cmd, specs)
	cmd.Flags().StringArrayVar(&imports, "preview-import", nil, "Import into each preview as APP_SERVICE=IMPORT=BACKUP; repeat for more; replaces the current imports")
	cmd.Flags().BoolVar(&clearImports, "clear-preview-imports", false, "Remove every preview import")
	return cmd
}

// getAppForPreviewSettings fetches the app, whose settings field holds its preview environment settings.
func getAppForPreviewSettings(cmd *cobra.Command, client *rest.Client, appID string) (map[string]interface{}, error) {
	var app map[string]interface{}
	if err := client.Get(cmd.Context(), escapedPath("/apps/%s", appID), nil, &app); err != nil {
		return nil, err
	}
	return app, nil
}

// appPreviewSettings returns the app's stored preview environment settings, or nil when it has none.
func appPreviewSettings(app interface{}) map[string]interface{} {
	row, ok := app.(map[string]interface{})
	if !ok {
		return nil
	}
	settings, _ := valueAtPath(row, "settings.previewEnvironments").(map[string]interface{})
	return settings
}

// fillAppPreviewSettingsDefaults copies the app's current preview settings into values for every field not given.
// Null and empty values are skipped, because the update API rejects them.
func fillAppPreviewSettingsDefaults(app map[string]interface{}, values map[string]interface{}) {
	current := appPreviewSettings(app)
	for _, key := range appPreviewSettingsInputKeys {
		if _, given := values[key]; given {
			continue
		}
		value := current[key]
		if value == nil || value == "" {
			continue
		}
		values[key] = value
	}
}

// parseAppPreviewImports turns APP_SERVICE=IMPORT=BACKUP values into preview import objects.
func parseAppPreviewImports(values []string) ([]interface{}, error) {
	result := make([]interface{}, 0, len(values))
	for _, value := range values {
		parts := strings.SplitN(value, "=", 3)
		if len(parts) != 3 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[2]) == "" {
			return nil, errors.Errorf("invalid --preview-import %q; expected APP_SERVICE=IMPORT=BACKUP", value)
		}
		result = append(result, map[string]interface{}{
			"appServiceName": strings.TrimSpace(parts[0]),
			"importName":     strings.TrimSpace(parts[1]),
			"backupName":     strings.TrimSpace(parts[2]),
		})
	}
	return result, nil
}

// printAppPreviewSettings prints an app's preview environment settings. JSON output is the app's settings object when
// settingsOnly is set, and the whole app as the API returned it otherwise.
func printAppPreviewSettings(cmd *cobra.Command, out outputOptions, app interface{}, settingsOnly bool) error {
	appRow, _ := app.(map[string]interface{})
	if outputFormat(cmd, out) == outputJSON {
		if settingsOnly && appRow != nil {
			return printJSON(cmd, appRow["settings"])
		}
		return printJSON(cmd, app)
	}
	row := map[string]interface{}{}
	for key, value := range appPreviewSettings(app) {
		row[key] = value
	}
	row["imports"] = formatAppPreviewImports(row["imports"])
	if appRow != nil {
		row["previewsAvailable"] = appRow["previewsAvailable"]
	}
	return printGetResult(cmd, out, row, appPreviewSettingsColumns)
}

// formatAppPreviewImports shows preview imports as APP_SERVICE=IMPORT=BACKUP, separated by commas.
func formatAppPreviewImports(value interface{}) string {
	labels := []string{}
	for _, item := range asRows(value) {
		labels = append(labels, fmt.Sprintf("%s=%s=%s", formatValue(item["appServiceName"]), formatValue(item["importName"]), formatValue(item["backupName"])))
	}
	return strings.Join(labels, ", ")
}

// newAppPreviewCommand lists an app's pull requests for preview environments, creates a preview environment for one
// by hand, and rotates the shared preview password.
func newAppPreviewCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "preview", Aliases: []string{"previews"}, Short: "Manage pull request preview environments"}
	cmd.AddCommand(
		newAppPreviewPullRequestsCommand(out),
		newAppPreviewCreateCommand(out),
		newAppPreviewRegeneratePasswordCommand(out),
	)
	return cmd
}

// newAppPreviewPullRequestsCommand lists the app's open pull requests with their preview environments.
func newAppPreviewPullRequestsCommand(out outputOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "pull-requests APP_ID",
		Short: "List open pull requests and their preview environments",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result map[string]interface{}
			if err := client.Get(cmd.Context(), escapedPath("/apps/preview-pull-requests/%s", args[0]), nil, &result); err != nil {
				return err
			}
			if outputFormat(cmd, out) == outputJSON {
				return printJSON(cmd, result)
			}
			if configured, _ := result["configured"].(bool); !configured {
				fmt.Fprintf(cmd.OutOrStdout(), "Preview environments are not configured for app %s. Turn them on with: wodby app settings update %s --previews --preview-base ENV_ID\n", args[0], args[0])
				return nil
			}
			pullRequests := asRows(result["pullRequests"])
			if len(pullRequests) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No open pull requests.")
				return nil
			}
			return printResult(cmd, out, pullRequests, appPreviewPullRequestColumns)
		},
	}
}

// newAppPreviewCreateCommand creates a preview environment for one open pull request and prints the task that
// creates it.
func newAppPreviewCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	wait := waitOptions{}
	specs := []jsonFlagSpec{
		intJSONFlag("pull-request", "number", "Pull request number", true),
		stringJSONFlag("repo", "remoteGitRepoId", "Remote repository ID of the pull request, as listed by pull-requests; required when the base environment tracks several repositories", false),
	}
	cmd := &cobra.Command{
		Use:   "create APP_ID",
		Short: "Create a preview environment for an open pull request",
		Long:  "Create a preview environment for an open pull request. An active paid subscription is required.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, err := readBodyOrJSONFlags(cmd, body, specs, false)
			if err != nil {
				return err
			}
			return postAndPrintOperation(cmd, out, wait, escapedPath("/apps/preview-environments/%s", args[0]), requestBody)
		},
	}
	addBodyFlags(cmd, &body)
	addJSONFlagSpecs(cmd, specs)
	addWaitFlags(cmd, &wait)
	return cmd
}

// newAppPreviewRegeneratePasswordCommand replaces the basic auth password shared by the app's preview environments
// after confirmation.
func newAppPreviewRegeneratePasswordCommand(out outputOptions) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "regenerate-password APP_ID",
		Short: "Replace the basic auth password shared by the app's preview environments",
		Long: "Replace the basic auth password shared by the app's preview environments. Every preview switches to " +
			"the new password, which members reveal in the dashboard. Basic auth must be on in the preview settings.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := confirm(cmd, yes, "Replace the preview password? The current password stops working."); err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), escapedPath("/apps/preview-password/regenerate/%s", args[0]), nil, nil, &result); err != nil {
				return err
			}
			return printAppPreviewSettings(cmd, out, result, false)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm without prompting")
	return cmd
}
