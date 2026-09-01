package ops

import (
	"net/url"
	"strconv"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	appEnvironmentColumns                     = []string{"id", "name", "title", "environmentType", "status", "outdated", "app", "stack", "cluster", "domain", "routingMode", "routingPending", "configurationReady", "configurationIssues"}
	kubernetesVersionUpgradePlanColumns       = []string{"currentVersion", "providerVersion", "provider", "supported", "targets", "blockers", "warnings", "observedAt"}
	integrationProviderRevisionUpgradeColumns = []string{"state", "reasons", "removedFields", "canDropRemovedFields", "currentRevision", "targetRevision"}
	backupPresetColumns                       = []string{"id", "backupName", "backupCategory", "envTypes", "integration", "bucket", "storageClass", "auto", "disabled", "crontab", "nextRunAt", "createdAt", "updatedAt"}
)

func newAppEnvironmentCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{Use: "environment", Aliases: []string{"environments", "app-environment", "app-environments"}, Short: "Manage app environments"}
	addOutputFlag(cmd, &out)

	var orgID, projectIDs, appID, clusterID string
	listCmd := &cobra.Command{
		Use: "list", Short: "List app environments",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
			if err != nil {
				return err
			}
			query := url.Values{"orgId": []string{resolvedOrgID}}
			addQuery(query, "projectIds", projectIDs)
			addQuery(query, "appId", appID)
			addQuery(query, "clusterId", clusterID)
			var result interface{}
			if err := client.Get(cmd.Context(), "/app-environments", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, appEnvironmentColumns)
		},
	}
	listCmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	listCmd.Flags().StringVar(&projectIDs, "project", "", "Project ID or comma-separated project IDs")
	listCmd.Flags().StringVar(&appID, "app", "", "App ID")
	listCmd.Flags().StringVar(&clusterID, "cluster", "", "Cluster ID")

	defaultToList(cmd, listCmd)
	cmd.AddCommand(
		listCmd,
		newGetCommand("get ID", "Get app environment", "/app-environments/", appEnvironmentColumns, out),
		newAppEnvironmentGetByNameCommand(out),
		newAppEnvironmentCreateCommand(out),
		newTitleUpdateCommand("update ID", "Update app environment", "/app-environments/", appEnvironmentColumns, out),
		newAppEnvironmentDeleteCommand(out),
		newRawBodyPutCommand("update-settings ID", "Update app environment settings", "/app-environments/settings/%s", appEnvironmentColumns, out),
		newRawBodyPutCommand("maintenance-mode ID", "Update app environment maintenance mode", "/app-environments/%s/actions/maintenance-mode", operationColumns, out),
		newGetCommand("get-cicd-settings ID", "Get app environment CI/CD settings", "/app-environments/cicd-settings/", instanceCICDSettingsColumns, out),
		newRawBodyPutCommand("update-cicd-settings ID", "Update app environment CI/CD settings", "/app-environments/cicd-settings/%s", instanceCICDSettingsColumns, out),
		newRawBodyPostCommand("upgrade-stack ID", "Upgrade app environment stack", "/app-environments/%s/actions/upgrade-stack", operationColumns, out),
		newRawBodyPostCommand("reconcile-stack ID", "Reconcile app environment stack", "/app-environments/%s/actions/reconcile-stack", operationColumns, out),
	)
	return cmd
}

func newAppEnvironmentGetByNameCommand(out outputOptions) *cobra.Command {
	var orgID string
	cmd := &cobra.Command{Use: "get-by-name APP_NAME ENVIRONMENT_NAME", Short: "Get app environment by app and environment name", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		query := url.Values{}
		addQuery(query, "orgId", orgID)
		client, err := newRESTClient()
		if err != nil {
			return err
		}
		var result interface{}
		if err := client.Get(cmd.Context(), escapedPath("/app-environments/by-name/%s/%s", args[0], args[1]), query, &result); err != nil {
			return err
		}
		return printClientGetResult(cmd, client, out, result, appEnvironmentColumns)
	}}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID")
	return cmd
}

func newAppEnvironmentCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var orgID, app, environmentName, environmentTitle, environmentType, cluster, stack, stackRev, domain, ciIntegration, registryIntegration string
	var deferInitialDeployment bool
	cmd := &cobra.Command{Use: "create", Short: "Create app environment", RunE: func(cmd *cobra.Command, _ []string) error {
		requestBody, hasBody, err := readBody(body)
		if err != nil {
			return err
		}
		client, err := newRESTClient()
		if err != nil {
			return err
		}
		if !hasBody {
			if err := requireFlag(app, "--app"); err != nil {
				return err
			}
			if err := requireFlag(environmentName, "--name"); err != nil {
				return err
			}
			if err := requireFlag(environmentType, "--type"); err != nil {
				return err
			}
			resolvedAppID, err := resolveAppID(cmd.Context(), client, app, orgID)
			if err != nil {
				return err
			}
			resolvedStackRevID, err := resolveCreateStackRevID(cmd.Context(), client, stack, stackRev, orgID)
			if err != nil {
				return err
			}
			appIDNumber, err := strconv.Atoi(resolvedAppID)
			if err != nil {
				return errors.Wrap(err, "invalid --app")
			}
			stackRevIDNumber, err := strconv.Atoi(resolvedStackRevID)
			if err != nil {
				return errors.Wrap(err, "invalid --stack-rev")
			}
			values := map[string]interface{}{"appId": appIDNumber, "environmentName": environmentName, "environmentType": environmentType, "stackRevId": stackRevIDNumber, "deferInitialDeployment": deferInitialDeployment}
			addOptionalString(values, "environmentTitle", environmentTitle)
			addOptionalString(values, "domain", domain)
			if cluster != "" {
				resolvedClusterID, err := resolveClusterID(cmd.Context(), client, cluster, orgID)
				if err != nil {
					return err
				}
				if err := addOptionalInt(values, "clusterId", resolvedClusterID, "--cluster"); err != nil {
					return err
				}
			}
			if err := addOptionalInt(values, "ciIntegrationId", ciIntegration, "--ci-integration"); err != nil {
				return err
			}
			if err := addOptionalInt(values, "registryIntegrationId", registryIntegration, "--registry-integration"); err != nil {
				return err
			}
			requestBody = values
		}
		var result interface{}
		if err := client.Post(cmd.Context(), "/app-environments", nil, requestBody, &result); err != nil {
			return err
		}
		return printClientResult(cmd, client, out, result, resourceOrOperationColumns(result, appEnvironmentColumns))
	}}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID for resolving names")
	cmd.Flags().StringVar(&app, "app", "", "App ID or name")
	cmd.Flags().StringVar(&environmentName, "name", "", "Environment machine name")
	cmd.Flags().StringVar(&environmentTitle, "title", "", "Environment title")
	cmd.Flags().StringVar(&environmentType, "type", "", "Environment type: prod, test, staging, dev, or feature")
	cmd.Flags().StringVar(&cluster, "cluster", "", "Cluster ID or name")
	cmd.Flags().StringVar(&stack, "stack", "", "Stack ID or name; uses the current revision")
	cmd.Flags().StringVar(&stackRev, "stack-rev", "", "Stack revision ID")
	cmd.Flags().StringVar(&domain, "domain", "", "Environment domain")
	cmd.Flags().StringVar(&ciIntegration, "ci-integration", "", "CI integration ID")
	cmd.Flags().StringVar(&registryIntegration, "registry-integration", "", "Registry integration ID")
	cmd.Flags().BoolVar(&deferInitialDeployment, "defer-initial-deployment", false, "Create without starting the initial deployment")
	return cmd
}

func newAppEnvironmentDeleteCommand(out outputOptions) *cobra.Command {
	var yes, force bool
	cmd := &cobra.Command{Use: "delete ID", Short: "Delete app environment", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := confirm(cmd, yes, "Delete app environment?"); err != nil {
			return err
		}
		client, err := newRESTClient()
		if err != nil {
			return err
		}
		query := url.Values{"force": []string{strconv.FormatBool(force)}}
		var result interface{}
		if err := client.Delete(cmd.Context(), "/app-environments/"+url.PathEscape(args[0]), query, &result); err != nil {
			return err
		}
		return printClientResult(cmd, client, out, result, operationColumns)
	}}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm without prompting")
	cmd.Flags().BoolVar(&force, "force", false, "Force deletion")
	return cmd
}

func newStackUpdateServiceRevisionsCommand(out outputOptions) *cobra.Command {
	var scope string
	cmd := &cobra.Command{Use: "update-service-revisions ID", Short: "Update stack service revisions", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		query := url.Values{}
		addQuery(query, "scope", scope)
		client, err := newRESTClient()
		if err != nil {
			return err
		}
		var result interface{}
		if err := client.Post(cmd.Context(), escapedPath("/stacks/%s/actions/update-service-revisions", args[0]), query, nil, &result); err != nil {
			return err
		}
		return printClientResult(cmd, client, out, result, operationColumns)
	}}
	cmd.Flags().StringVar(&scope, "scope", "", "Update scope: all or stateless_only")
	return cmd
}

func newIntegrationProviderRevisionUpgradeCommand(out outputOptions) *cobra.Command {
	var dropRemovedFields bool
	cmd := &cobra.Command{Use: "upgrade-provider-revision ID", Short: "Upgrade integration provider revision", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newRESTClient()
		if err != nil {
			return err
		}
		var result interface{}
		body := map[string]interface{}{"dropRemovedFields": dropRemovedFields}
		if err := client.Post(cmd.Context(), escapedPath("/integrations/%s/actions/upgrade-provider-revision", args[0]), nil, body, &result); err != nil {
			return err
		}
		return printClientResult(cmd, client, out, result, operationColumns)
	}}
	cmd.Flags().BoolVar(&dropRemovedFields, "drop-removed-fields", false, "Permanently delete fields removed by the target provider revision")
	return cmd
}

func newIntegrationProviderRevisionUpgradePreviewCommand(out outputOptions) *cobra.Command {
	return &cobra.Command{Use: "provider-revision-upgrade ID", Short: "Preview provider revision upgrade", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return getAndPrint(cmd, out, escapedPath("/integration-provider-revision-upgrades/%s", args[0]), integrationProviderRevisionUpgradeColumns)
	}}
}

func newBackupPresetCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{Use: "backup-preset", Aliases: []string{"backup-presets"}, Short: "Manage backup presets"}
	addOutputFlag(cmd, &out)
	listCmd := newBackupPresetListCommand(out)
	defaultToList(cmd, listCmd)
	cmd.AddCommand(
		listCmd,
		newGetCommand("get ID", "Get backup preset", "/backup-presets/", backupPresetColumns, out),
		newRawBodyPostCommand("create", "Create backup preset", "/backup-presets", backupPresetColumns, out),
		newRawBodyPutCommand("update ID", "Update backup preset", "/backup-presets/%s", backupPresetColumns, out),
		newDeleteCommand("delete ID", "Delete backup preset", "/backup-presets/", backupPresetColumns, out),
		newBackupPresetBackupsCommand(out),
	)
	return cmd
}

func newBackupPresetListCommand(out outputOptions) *cobra.Command {
	var orgID, appEnvironmentID, appServiceID, databaseID, databaseDBID, backupName, applicableEnvironmentID, applicableCategory string
	cmd := &cobra.Command{Use: "list", Short: "List backup presets", RunE: func(cmd *cobra.Command, _ []string) error {
		query := url.Values{}
		addQuery(query, "orgId", orgID)
		addQuery(query, "appInstanceId", appEnvironmentID)
		addQuery(query, "appServiceId", appServiceID)
		addQuery(query, "databaseId", databaseID)
		addQuery(query, "databaseDbId", databaseDBID)
		addQuery(query, "backupName", backupName)
		addQuery(query, "applicableEnvId", applicableEnvironmentID)
		addQuery(query, "applicableBackupCategory", applicableCategory)
		client, err := newRESTClient()
		if err != nil {
			return err
		}
		var result interface{}
		if err := client.Get(cmd.Context(), "/backup-presets", query, &result); err != nil {
			return err
		}
		return printClientResult(cmd, client, out, result, backupPresetColumns)
	}}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID")
	cmd.Flags().StringVar(&appEnvironmentID, "environment", "", "App environment ID")
	cmd.Flags().StringVar(&appServiceID, "service", "", "App service ID")
	cmd.Flags().StringVar(&databaseID, "database", "", "Database ID")
	cmd.Flags().StringVar(&databaseDBID, "database-db", "", "Database DB ID")
	cmd.Flags().StringVar(&backupName, "backup-name", "", "Backup definition name")
	cmd.Flags().StringVar(&applicableEnvironmentID, "applicable-environment", "", "Environment ID used for applicability checks")
	cmd.Flags().StringVar(&applicableCategory, "applicable-category", "", "Backup category used for applicability checks")
	return cmd
}

func newBackupPresetBackupsCommand(out outputOptions) *cobra.Command {
	var page, pageSize int
	cmd := &cobra.Command{Use: "backups ID", Short: "List backups created from a preset", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		query := url.Values{}
		addPagination(query, page, pageSize)
		client, err := newRESTClient()
		if err != nil {
			return err
		}
		var result interface{}
		if err := client.Get(cmd.Context(), escapedPath("/backup-presets/%s/backups", args[0]), query, &result); err != nil {
			return err
		}
		return printClientResult(cmd, client, out, result, backupColumns)
	}}
	cmd.Flags().IntVar(&page, "page", 0, "Page number")
	cmd.Flags().IntVar(&pageSize, "page-size", 0, "Page size")
	return cmd
}

func newAppServiceBackupOptionDefaultsCommand(out outputOptions) *cobra.Command {
	var backupName string
	cmd := &cobra.Command{Use: "backup-option-defaults ID", Short: "List effective backup option defaults", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireFlag(backupName, "--backup-name"); err != nil {
			return err
		}
		client, err := newRESTClient()
		if err != nil {
			return err
		}
		query := url.Values{"backupName": []string{backupName}}
		var result interface{}
		if err := client.Get(cmd.Context(), escapedPath("/app-services/%s/options/backup-option-defaults", args[0]), query, &result); err != nil {
			return err
		}
		return printClientResult(cmd, client, out, result, []string{"name", "values"})
	}}
	cmd.Flags().StringVar(&backupName, "backup-name", "", "Backup definition name")
	return cmd
}

func newAppDeploymentCancelCommand(out outputOptions) *cobra.Command {
	return &cobra.Command{Use: "cancel ID", Short: "Cancel deployment", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newRESTClient()
		if err != nil {
			return err
		}
		var result interface{}
		if err := client.Post(cmd.Context(), escapedPath("/app-deployments/%s/actions/cancel", args[0]), nil, nil, &result); err != nil {
			return err
		}
		return printClientResult(cmd, client, out, result, deploymentColumns)
	}}
}
