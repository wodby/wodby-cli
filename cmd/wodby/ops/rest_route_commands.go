package ops

import (
	"net/url"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

// Commands for REST API routes that had no CLI command. Each add function attaches them to an existing group.

var (
	orgSubscriptionColumns      = []string{"status", "plan.name", "plan.title"}
	workspaceGitStatusColumns   = []string{"available", "reason", "branch", "upstream", "ahead", "behind", "commit", "commitSubject", "truncated", "checkedAt"}
	appAuthColumns              = []string{"id", "login", "realm", "scope", "appServiceId", "appRouteId", "createdAt"}
	resolveIntegrationColumns   = []string{"created", "integration.id", "integration.title", "integration.status"}
	gitRepoUsageColumns         = []string{"services", "stacks", "providers"}
	stackEnvVarColumns          = []string{"id", "name", "value", "envType", "createdAt"}
	routeSettingColumns         = []string{"id", "name", "value", "default", "updatedAt"}
	effectiveRouteSettingColumn = []string{"name", "value", "source"}
)

func addOrgRESTCommands(cmd *cobra.Command, out outputOptions) {
	cmd.AddCommand(newServiceChildListCommand("subscription ORG_ID", "Get organization subscription", "/orgs/%s/subscription", orgSubscriptionColumns, out))
}

func addMemberRESTCommands(cmd *cobra.Command, out outputOptions) {
	cmd.AddCommand(newGetCommand("get ID", "Get organization member", "/org-memberships/", memberColumns, out))
}

func addWorkspaceRESTCommands(cmd *cobra.Command, out outputOptions) {
	gitStatus := newServiceChildListCommand("git-status ENVIRONMENT_ID", "Get the live Git status of the workspace checkout", "/workspaces/%s/git-status", workspaceGitStatusColumns, out)
	gitStatus.Long = "Owner only. Read the branch, HEAD commit and uncommitted changes of the workspace checkout. Use -o json to list the changed files."
	cmd.AddCommand(gitStatus)
}

// addEnvironmentRESTCommands adds HTTP basic authentication credentials of an app environment.
func addEnvironmentRESTCommands(cmd *cobra.Command, out outputOptions) {
	auth := &cobra.Command{Use: "auth", Aliases: []string{"auths"}, Short: "Manage HTTP basic authentication of app environments"}
	list := &cobra.Command{
		Use:   "list ENVIRONMENT_ID",
		Short: "List HTTP basic authentication credentials",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/app-auths", url.Values{"appInstanceId": []string{args[0]}}, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, appAuthColumns)
		},
	}
	defaultToList(auth, list)
	auth.AddCommand(
		list,
		newAppAuthCreateCommand(out),
		newServiceChildUpdateCommand("update ID", "Update HTTP basic authentication", "/app-auths/%s", appAuthColumns, out, []jsonFlagSpec{
			stringJSONFlag("login", "login", "Login", true),
			stringJSONFlag("realm", "realm", "Realm shown in the browser prompt", true),
			stringJSONFlag("password", "password", "New password; omit to keep the current one", false),
		}),
		newDeleteCommand("delete ID", "Delete HTTP basic authentication", "/app-auths/", appAuthColumns, out),
	)
	cmd.AddCommand(auth)
}

func newAppAuthCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var serviceID, routeID string
	specs := []jsonFlagSpec{
		stringJSONFlag("login", "login", "Login", true),
		stringJSONFlag("password", "password", "Password", true),
		stringJSONFlag("realm", "realm", "Realm shown in the browser prompt", true),
	}
	cmd := &cobra.Command{
		Use:   "create ENVIRONMENT_ID",
		Short: "Protect an app environment, service or route with HTTP basic authentication",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				values, err := bodyFromJSONFlagSpecs(cmd, specs, false)
				if err != nil {
					return err
				}
				if err := addOptionalInt(values, "appServiceId", serviceID, "--service"); err != nil {
					return err
				}
				if err := addOptionalInt(values, "appRouteId", routeID, "--route"); err != nil {
					return err
				}
				requestBody = values
			}
			values, ok := requestBody.(map[string]interface{})
			if !ok {
				return errors.New("request body must be a JSON object")
			}
			if _, ok := values["appInstanceId"]; !ok {
				if err := addOptionalInt(values, "appInstanceId", args[0], "ENVIRONMENT_ID"); err != nil {
					return err
				}
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/app-auths", nil, values, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, appAuthColumns)
		},
	}
	addBodyFlags(cmd, &body)
	addJSONFlagSpecs(cmd, specs)
	cmd.Flags().StringVar(&serviceID, "service", "", "Protect only this app service")
	cmd.Flags().StringVar(&routeID, "route", "", "Protect only this route")
	return cmd
}

func addIntegrationRESTCommands(cmd *cobra.Command, out outputOptions) {
	cmd.AddCommand(
		newRawBodyPostCommand("search", "Search integrations (JSON body: orgId, projectIds, types, statuses, labels, variables, envType)", "/integrations/actions/search", integrationColumns, out),
		newRawBodyPostCommand("resolve", "Find a matching integration or create it (JSON body as for integration create)", "/integrations/actions/resolve", resolveIntegrationColumns, out),
	)
}

func addProviderRESTCommands(cmd *cobra.Command, out outputOptions) {
	settings := &cobra.Command{Use: "settings", Short: "Manage provider settings"}
	settings.AddCommand(newRawBodyPutCommand("update ID", "Update provider settings (JSON body: gitAutoUpdate)", "/providers/settings/%s", providerColumns, out))
	cmd.AddCommand(
		newRawBodyPostCommand("create-variable", "Create a variable provider (JSON body: orgId, projectId, name, title, fields)", "/providers/actions/create-variable", providerColumns, out),
		newProviderManifestCreateCommand(out),
		newProviderManifestUpdateCommand(out),
		newCatalogImportFromGitCommand("import", "Import providers from Git", "/providers/actions/import", operationColumns, out),
		newUpdateFromGitCommand("update-from-git ID", "Update provider from Git", "/providers/%s/actions/update-from-git", out),
		settings,
	)
}

func newProviderManifestCreateCommand(out outputOptions) *cobra.Command {
	opts := manifestFileOptions{}
	cmd := &cobra.Command{
		Use:   "create-from-manifest [MANIFEST]",
		Short: "Create provider from manifest",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := providerManifestBody(cmd, args, opts)
			if err != nil {
				return err
			}
			if err := addOptionalInt(body, "orgId", opts.orgID, "--org"); err != nil {
				return err
			}
			if err := addOptionalInt(body, "projectId", opts.projectID, "--project"); err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/providers/actions/create-from-manifest", nil, body, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, providerColumns)
		},
	}
	cmd.Flags().StringVarP(&opts.manifest, "manifest", "f", "", "Path to provider manifest YAML; use - for stdin")
	cmd.Flags().StringVar(&opts.orgID, "org", "", "Organization ID")
	cmd.Flags().StringVar(&opts.projectID, "project", "", "Project ID")
	return cmd
}

func newProviderManifestUpdateCommand(out outputOptions) *cobra.Command {
	opts := manifestFileOptions{}
	wait := waitOptions{}
	cmd := &cobra.Command{
		Use:   "update-from-manifest ID [MANIFEST]",
		Short: "Update provider from manifest",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := providerManifestBody(cmd, args[1:], opts)
			if err != nil {
				return err
			}
			return postAndPrintOperation(cmd, out, wait, escapedPath("/providers/%s/actions/update-from-manifest", args[0]), body)
		},
	}
	cmd.Flags().StringVarP(&opts.manifest, "manifest", "f", "", "Path to provider manifest YAML; use - for stdin")
	addWaitFlags(cmd, &wait)
	return cmd
}

func providerManifestBody(cmd *cobra.Command, args []string, opts manifestFileOptions) (map[string]interface{}, error) {
	manifestPath, err := resolveManifestPath(args, opts)
	if err != nil {
		return nil, err
	}
	manifestYAML, err := readTextFileOrStdin(cmd, manifestPath)
	if err != nil {
		return nil, errors.Wrap(err, "read manifest")
	}
	return map[string]interface{}{"manifestYaml": manifestYAML}, nil
}

// newUpdateFromGitCommand starts the Git update of the resource in pathPattern and prints the operation or, with
// --wait, the finished task.
func newUpdateFromGitCommand(use string, short string, pathPattern string, out outputOptions) *cobra.Command {
	body := bodyOptions{}
	wait := waitOptions{}
	var gitRef, gitRefType string
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if err := requireFlag(gitRef, "--git-ref"); err != nil {
					return err
				}
				if err := requireFlag(gitRefType, "--git-ref-type"); err != nil {
					return err
				}
				requestBody = map[string]interface{}{"gitRef": gitRef, "gitRefType": gitRefType}
			}
			return postAndPrintOperation(cmd, out, wait, escapedPath(pathPattern, args[0]), requestBody)
		},
	}
	addBodyFlags(cmd, &body)
	addWaitFlags(cmd, &wait)
	cmd.Flags().StringVar(&gitRef, "git-ref", "", "Git ref")
	cmd.Flags().StringVar(&gitRefType, "git-ref-type", "", "Git ref type: branch or tag")
	return cmd
}

// postAndPrintOperation posts body and prints the operation result, or the finished task when --wait is set.
func postAndPrintOperation(cmd *cobra.Command, out outputOptions, wait waitOptions, path string, body interface{}) error {
	client, err := newRESTClient()
	if err != nil {
		return err
	}
	var result interface{}
	if err := client.Post(cmd.Context(), path, nil, body, &result); err != nil {
		return err
	}
	columns := operationColumns
	if wait.wait && firstTaskID(result) != "" {
		result, err = waitForTask(cmd.Context(), client, firstTaskID(result), wait.timeout)
		if err != nil {
			return err
		}
		columns = taskColumns
	}
	return printClientResult(cmd, client, out, result, columns)
}

// newGitRepoCommand manages Git repositories shared by services, stacks and providers.
func newGitRepoCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{Use: "git-repo", Aliases: []string{"git-repos"}, Short: "Manage Git repositories shared by services, stacks and providers"}
	addOutputFlag(cmd, &out)
	update := newUpdateFromGitCommand("update-from-git ID", "Update every service, stack and provider of a Git repository to a ref", "/git-repos/%s/actions/update-from-git", out)
	update.Long = "Start one task that moves every service, stack and provider using the repository to the requested ref. Check git-repo usages first to see what changes."
	cmd.AddCommand(
		newServiceChildListCommand("usages ID", "List the services, stacks and providers that use a Git repository", "/git-repos/%s/usages", gitRepoUsageColumns, out),
		update,
	)
	return cmd
}

func addServiceRESTCommands(cmd *cobra.Command, out outputOptions) {
	opts := manifestFileOptions{}
	wait := waitOptions{}
	update := &cobra.Command{
		Use:   "update-from-manifest ID [MANIFEST]",
		Short: "Update service from manifest",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := manifestRequestBody(cmd, args[1:], opts)
			if err != nil {
				return err
			}
			return postAndPrintOperation(cmd, out, wait, escapedPath("/services/%s/actions/update-from-manifest", args[0]), body)
		},
	}
	update.Flags().StringVarP(&opts.manifest, "manifest", "f", "", "Path to Wodby manifest YAML; use - for stdin")
	update.Flags().StringVar(&opts.version, "version", "", "Revision version label")
	update.Flags().StringArrayVar(&opts.includes, "include", nil, "Referenced file to include; use PATH or MANIFEST_PATH=LOCAL_PATH")
	addWaitFlags(update, &wait)
	cmd.AddCommand(update)
}

func addStackRESTCommands(cmd *cobra.Command, out outputOptions) {
	catalog := &cobra.Command{
		Use:   "catalog",
		Short: "List the public stack catalog",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/catalog/stacks", nil, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, stackColumns)
		},
	}
	envVar := &cobra.Command{Use: "env-var", Aliases: []string{"env-vars"}, Short: "Manage stack environment variables"}
	envVar.AddCommand(
		newServiceChildListCommand("list REVISION_ID", "List stack environment variables of a stack revision", "/stack-revisions/%s/env-vars", stackEnvVarColumns, out),
		newServiceChildCreateCommand("create STACK_ID", "Create stack environment variable", "/stacks/%s/configuration/env-vars", stackEnvVarColumns, out, []jsonFlagSpec{
			stringJSONFlag("name", "name", "Variable name", true),
			stringJSONFlag("value", "value", "Variable value", true),
			boolJSONFlag("secret", "secret", "Store the value as a secret", true),
			stringJSONFlag("env-type", "envType", "Limit the variable to an environment type", false),
		}),
		newServiceChildUpdateCommand("update ID", "Update stack environment variable", "/stack-env-vars/%s", stackEnvVarColumns, out, []jsonFlagSpec{
			stringJSONFlag("value", "value", "Variable value", true),
			boolJSONFlag("secret", "secret", "Store the value as a secret", true),
		}),
		newDeleteCommand("delete ID", "Delete stack environment variable", "/stack-env-vars/", stackEnvVarColumns, out),
	)
	cmd.AddCommand(
		catalog,
		newServiceChildUpdateCommand("update ID", "Update stack name and title", "/stacks/%s", stackColumns, out, []jsonFlagSpec{
			stringJSONFlag("name", "name", "Stack name", true),
			stringJSONFlag("title", "title", "Stack title", true),
		}),
		newDeleteCommand("delete ID", "Delete stack", "/stacks/", stackColumns, out),
		envVar,
	)
}

func addBackupRESTCommands(cmd *cobra.Command, out outputOptions) {
	var yes bool
	voidFile := &cobra.Command{
		Use:   "void-file ID",
		Short: "Permanently remove a backup file from Wodby Blob Storage and keep the backup record",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := confirm(cmd, yes, "Permanently remove the backup file?"); err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), escapedPath("/backups/%s/actions/void-file", args[0]), nil, nil, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, backupColumns)
		},
	}
	voidFile.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm without prompting")
	cmd.AddCommand(voidFile)
}

func addTaskStepRESTCommands(cmd *cobra.Command, out outputOptions) {
	cmd.AddCommand(newServiceChildListCommand("log-url STEP_ID", "Get a download URL for the persisted step log", "/task-steps/%s/log-url", []string{"url"}, out))
}

func addRouteRESTCommands(cmd *cobra.Command, out outputOptions) {
	settings := &cobra.Command{Use: "settings", Aliases: []string{"setting"}, Short: "Manage route setting overrides"}
	list := newServiceChildListCommand("list ROUTE_ID", "List route setting overrides", "/app-routes/%s/settings", routeSettingColumns, out)
	defaultToList(settings, list)
	var yes bool
	deleteSetting := &cobra.Command{
		Use:   "delete ROUTE_ID NAME",
		Short: "Remove a route setting override so the route inherits the default",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := confirm(cmd, yes, "Remove the route setting override?"); err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Delete(cmd.Context(), escapedPath("/app-routes/%s/settings/%s", args[0], args[1]), nil, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, operationColumns)
		},
	}
	deleteSetting.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm without prompting")
	settings.AddCommand(
		list,
		newServiceChildListCommand("effective ROUTE_ID", "List the settings that apply to a route and where each comes from", "/app-routes/%s/effective-settings", effectiveRouteSettingColumn, out),
		newServiceChildSetCommand("set ROUTE_ID NAME", "Set a route setting override (NAME such as REQUEST_TIMEOUT)", "/app-routes/%s/settings/%s", routeSettingColumns, out, []jsonFlagSpec{
			stringJSONFlag("value", "value", "Setting value", true),
		}),
		deleteSetting,
	)
	cmd.AddCommand(
		newClusterActionCommand("retry-certificate ID", "Retry issuing the route's certificate", "/app-routes/%s/actions/retry-certificate", out),
		settings,
	)
}

func addCertRESTCommands(cmd *cobra.Command, out outputOptions) {
	cmd.AddCommand(newDeleteCommand("delete ID", "Delete an uploaded certificate that nothing uses", "/certs/", certColumns, out))
}
