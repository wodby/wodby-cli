package ops

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	sharingColumns                 = []string{"ownershipScope", "ownerProjectId", "availableToAllProjects", "sharedProjects"}
	workspaceGitDiffColumns        = []string{"available", "reason", "path", "binary", "truncated"}
	environmentMigrationColumns    = []string{"id", "status", "phase", "sourceClusterId", "targetClusterId", "taskId", "failureMessage", "retainUntil", "createdAt"}
	environmentMigrationOpColumns  = []string{"success", "taskId", "migration.id", "migration.status", "migration.phase"}
	integrationUsageColumns        = []string{"category", "kind", "target.type", "target.title", "target.parent.title", "detail"}
	buildImageRetentionFlagUsage   = "Void images of unused builds older than: never, one_month, three_months, six_months or one_year"
	availableToAllProjectsFlagName = "available-to-all-projects"
)

// addAvailableToAllProjectsFlag lets a create or import command make the new resource available to every project.
func addAvailableToAllProjectsFlag(cmd *cobra.Command) *cobra.Command {
	cmd.Flags().Bool(availableToAllProjectsFlagName, false, "Let every project in the organization use it; only for organization-owned resources created without --project")
	return cmd
}

// addAvailableToAllProjects copies --available-to-all-projects into a request body when the command has the flag and
// it was set.
func addAvailableToAllProjects(cmd *cobra.Command, values map[string]interface{}) {
	flag := cmd.Flags().Lookup(availableToAllProjectsFlagName)
	if flag == nil || !flag.Changed {
		return
	}
	value, _ := cmd.Flags().GetBool(availableToAllProjectsFlagName)
	values["availableToAllProjects"] = value
}

// newSharingCommand reads and changes who owns a resource and which projects can use it. allProjects tells whether
// the resource type can be made available to every project.
func newSharingCommand(resource string, pathPattern string, allProjects bool, out outputOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "sharing", Short: "Show and change " + resource + " ownership and project sharing"}
	cmd.AddCommand(newServiceChildListCommand("get ID", "Show "+resource+" ownership and project sharing", pathPattern, sharingColumns, out))

	body := bodyOptions{}
	var owner, ownerProject string
	var shares []string
	var noShares bool
	update := &cobra.Command{
		Use:   "update ID",
		Short: "Change " + resource + " owner and project shares",
		Long: "Change the owner and the projects the " + resource + " is shared with. Flags you leave out keep their " +
			"current value. --share replaces the whole share list; --no-shares removes every share. The owning project " +
			"always keeps write access. You need write access to every project whose share you add or remove.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			path := escapedPath(pathPattern, args[0])
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				var current interface{}
				if err := client.Get(cmd.Context(), path, nil, &current); err != nil {
					return err
				}
				requestBody, err = sharingRequestBody(cmd, objectItem(current), owner, ownerProject, shares, noShares)
				if err != nil {
					return err
				}
			}
			var result interface{}
			if err := client.Put(cmd.Context(), path, nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, sharingColumns)
		},
	}
	addBodyFlags(update, &body)
	update.Flags().StringVar(&owner, "owner", "", "New owner: org or project")
	update.Flags().StringVar(&ownerProject, "owner-project", "", "Owning project ID; implies --owner project")
	update.Flags().StringArrayVar(&shares, "share", nil, "Project share as PROJECT_ID=read or PROJECT_ID=write; repeat for each project")
	update.Flags().BoolVar(&noShares, "no-shares", false, "Remove every project share")
	if allProjects {
		update.Flags().Bool(availableToAllProjectsFlagName, false, "Let every project in the organization use it; only for organization-owned resources")
	}
	cmd.AddCommand(update)
	return cmd
}

// sharingRequestBody builds the complete sharing request from the current sharing and the changed flags.
func sharingRequestBody(cmd *cobra.Command, current map[string]interface{}, owner, ownerProject string, shares []string, noShares bool) (map[string]interface{}, error) {
	if noShares && len(shares) > 0 {
		return nil, errors.New("--share and --no-shares cannot be combined")
	}
	scope := formatValue(current["ownershipScope"])
	ownerProjectID := current["ownerProjectId"]
	if ownerProject != "" {
		id, err := strconv.Atoi(ownerProject)
		if err != nil {
			return nil, errors.Wrap(err, "invalid --owner-project")
		}
		scope, ownerProjectID = "project", id
	}
	if owner != "" {
		if owner != "org" && owner != "project" {
			return nil, errors.New("--owner must be org or project")
		}
		scope = owner
	}
	if scope == "org" {
		ownerProjectID = nil
	}
	if scope == "project" && ownerProjectID == nil {
		return nil, errors.New("--owner-project is required for a project owner")
	}

	sharedProjects := []interface{}{}
	switch {
	case noShares:
	case len(shares) > 0:
		for _, share := range shares {
			project, access, ok := strings.Cut(share, "=")
			id, err := strconv.Atoi(strings.TrimSpace(project))
			if !ok || err != nil || (access != "read" && access != "write") {
				return nil, errors.Errorf("invalid --share %q: use PROJECT_ID=read or PROJECT_ID=write", share)
			}
			sharedProjects = append(sharedProjects, map[string]interface{}{"projectId": id, "access": access})
		}
	default:
		// Keep the current shares; the owning project is listed with the owner role and keeps write access.
		if items, ok := current["sharedProjects"].([]interface{}); ok {
			for _, item := range items {
				share, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				access := "read"
				if role := formatValue(share["role"]); role == "write" || role == "owner" {
					access = "write"
				}
				sharedProjects = append(sharedProjects, map[string]interface{}{"projectId": share["projectId"], "access": access})
			}
		}
	}

	values := map[string]interface{}{"ownershipScope": scope, "sharedProjects": sharedProjects}
	if ownerProjectID != nil {
		values["ownerProjectId"] = ownerProjectID
	}
	addAvailableToAllProjects(cmd, values)
	return values, nil
}

func addSharingCommand(cmd *cobra.Command, resource string, pathPattern string, allProjects bool, out outputOptions) {
	cmd.AddCommand(newSharingCommand(resource, pathPattern, allProjects, out))
}

func addWorkspaceUpdateCommands(cmd *cobra.Command, out outputOptions) {
	var noScripts bool
	wait := waitOptions{}
	pull := &cobra.Command{
		Use:   "pull ENVIRONMENT_ID",
		Short: "Fast-forward the workspace checkout and run its post-deployment scripts",
		Long:  "Owner only. Fast-forward the workspace checkout to its upstream branch, keeping uncommitted changes, then run the environment's post-deployment scripts unless --no-scripts is set.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			body := map[string]interface{}{"scripts": !noScripts}
			if err := client.Post(cmd.Context(), escapedPath("/workspaces/%s/actions/pull", args[0]), nil, body, &result); err != nil {
				return err
			}
			if wait.wait {
				if id := formatValue(objectItem(result)["id"]); id != "" {
					if result, err = waitForTask(cmd.Context(), client, id, wait.timeout); err != nil {
						return err
					}
				}
			}
			return printClientResult(cmd, client, out, result, taskColumns)
		},
	}
	pull.Flags().BoolVar(&noScripts, "no-scripts", false, "Do not run post-deployment scripts after the pull")
	addWaitFlags(pull, &wait)

	var originalPath string
	diff := &cobra.Command{
		Use:   "git-diff ENVIRONMENT_ID PATH",
		Short: "Show how one file of the workspace checkout differs from the last commit",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			query := url.Values{"path": []string{args[1]}}
			addQuery(query, "originalPath", originalPath)
			var result interface{}
			if err := client.Get(cmd.Context(), escapedPath("/workspaces/%s/git-diff", args[0]), query, &result); err != nil {
				return err
			}
			// The table view prints the diff itself; the JSON view keeps every field.
			if outputFormat(cmd, out) != outputJSON {
				item := objectItem(result)
				if formatValue(item["available"]) == "true" {
					fmt.Fprint(cmd.OutOrStdout(), formatValue(item["diff"]))
					return nil
				}
			}
			return printClientResult(cmd, client, out, result, workspaceGitDiffColumns)
		},
	}
	diff.Flags().StringVar(&originalPath, "original-path", "", "Previous path of a renamed or copied file")
	cmd.AddCommand(pull, diff)
}

// addEnvironmentMigrationCommands moves an app environment to another cluster and follows the move.
func addEnvironmentMigrationCommands(cmd *cobra.Command, out outputOptions) {
	var targetCluster string
	var volumes []string
	wait := waitOptions{}
	migrate := &cobra.Command{
		Use:   "migrate ENVIRONMENT_ID",
		Short: "Move an app environment and its data to another cluster",
		Long:  "Start moving the app environment and its data to another cluster of the same organization. The source stays available for rollback until you clean it up or its retention ends.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireFlag(targetCluster, "--target-cluster"); err != nil {
				return err
			}
			body := map[string]interface{}{}
			if err := addOptionalInt(body, "targetClusterId", targetCluster, "--target-cluster"); err != nil {
				return err
			}
			if len(volumes) > 0 {
				items := make([]interface{}, 0, len(volumes))
				for _, volume := range volumes {
					id, class, ok := strings.Cut(volume, "=")
					number, err := strconv.Atoi(strings.TrimSpace(id))
					if !ok || err != nil || strings.TrimSpace(class) == "" {
						return errors.Errorf("invalid --volume %q: use VOLUME_ID=STORAGE_CLASS", volume)
					}
					items = append(items, map[string]interface{}{"appServiceVolumeId": number, "storageClassName": strings.TrimSpace(class)})
				}
				body["volumes"] = items
			}
			return postAndPrintMigration(cmd, out, wait, escapedPath("/app-environments/%s/actions/migrate", args[0]), body)
		},
	}
	migrate.Flags().StringVar(&targetCluster, "target-cluster", "", "Cluster to move the environment to")
	migrate.Flags().StringArrayVar(&volumes, "volume", nil, "Target storage class for a volume as VOLUME_ID=STORAGE_CLASS; repeat for each volume")
	addWaitFlags(migrate, &wait)

	migration := &cobra.Command{Use: "migration", Aliases: []string{"migrations"}, Short: "Follow app environment cluster migrations"}
	list := &cobra.Command{
		Use:   "list ENVIRONMENT_ID",
		Short: "List an app environment's cluster migrations",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/app-environment-migrations", url.Values{"appEnvironmentId": []string{args[0]}}, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, environmentMigrationColumns)
		},
	}
	defaultToList(migration, list)
	migration.AddCommand(
		list,
		newGetCommand("get ID", "Get a cluster migration with each service's progress", "/app-environment-migrations/", environmentMigrationColumns, out),
		newMigrationStepCommand("rollback ID", "Move the app environment back to its source cluster", "/app-environment-migrations/%s/actions/rollback", out),
		newMigrationStepCommand("cleanup ID", "Remove the retained source after a completed migration; it can no longer be rolled back", "/app-environment-migrations/%s/actions/cleanup", out),
	)
	cmd.AddCommand(migrate, migration)
}

func newMigrationStepCommand(use string, short string, pathPattern string, out outputOptions) *cobra.Command {
	var yes bool
	wait := waitOptions{}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := confirm(cmd, yes, short+"?"); err != nil {
				return err
			}
			return postAndPrintMigration(cmd, out, wait, escapedPath(pathPattern, args[0]), nil)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm without prompting")
	addWaitFlags(cmd, &wait)
	return cmd
}

func postAndPrintMigration(cmd *cobra.Command, out outputOptions, wait waitOptions, path string, body interface{}) error {
	client, err := newRESTClient()
	if err != nil {
		return err
	}
	var result interface{}
	if err := client.Post(cmd.Context(), path, nil, body, &result); err != nil {
		return err
	}
	columns := environmentMigrationOpColumns
	if wait.wait && firstTaskID(result) != "" {
		result, err = waitForTask(cmd.Context(), client, firstTaskID(result), wait.timeout)
		if err != nil {
			return err
		}
		columns = taskColumns
	}
	return printClientResult(cmd, client, out, result, columns)
}

// addEnvironmentRouteSettingsCommands manages the default route settings every route of an environment inherits.
func addEnvironmentRouteSettingsCommands(cmd *cobra.Command, out outputOptions) {
	settings := &cobra.Command{Use: "route-settings", Short: "Manage default route settings of app environments"}
	list := newServiceChildListCommand("list ENVIRONMENT_ID", "List default route settings", "/app-environments/route-settings/%s", routeSettingColumns, out)
	defaultToList(settings, list)
	settings.AddCommand(list, newRouteSettingsReplaceCommand("replace ENVIRONMENT_ID", "Replace all default route settings; a setting left out is removed", "/app-environments/route-settings/%s", out))
	cmd.AddCommand(settings)
}

func newRouteSettingsReplaceCommand(use string, short string, pathPattern string, out outputOptions) *cobra.Command {
	return newSettingsReplaceCommand(use, short, pathPattern, routeSettingColumns, "REQUEST_TIMEOUT=120", out)
}

// newSettingsReplaceCommand replaces a complete NAME=VALUE setting list at pathPattern.
func newSettingsReplaceCommand(use string, short string, pathPattern string, columns []string, example string, out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var settings []string
	var none bool
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
				if len(settings) == 0 && !none {
					return errors.New("pass --setting NAME=VALUE for each setting, or --none to remove all settings")
				}
				items := make([]interface{}, 0, len(settings))
				for _, setting := range settings {
					name, value, ok := strings.Cut(setting, "=")
					if !ok || strings.TrimSpace(name) == "" {
						return errors.Errorf("invalid --setting %q: use NAME=VALUE", setting)
					}
					items = append(items, map[string]interface{}{"name": strings.TrimSpace(name), "value": value})
				}
				requestBody = map[string]interface{}{"settings": items}
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), escapedPath(pathPattern, args[0]), nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringArrayVar(&settings, "setting", nil, "Setting as NAME=VALUE, such as "+example+"; repeat for each setting")
	cmd.Flags().BoolVar(&none, "none", false, "Remove every setting")
	return cmd
}

func addRouteUpdateCommands(cmd *cobra.Command, out outputOptions) {
	var routes []string
	var operation string
	var yes bool
	wait := waitOptions{}
	bulk := &cobra.Command{
		Use:   "bulk ENVIRONMENT_ID",
		Short: "Enable, disable or delete several routes of an app environment in one task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if operation != "enable" && operation != "disable" && operation != "delete" {
				return errors.New("--operation must be enable, disable or delete")
			}
			routeIDs, err := parseIntValues(routes, "--routes")
			if err != nil {
				return err
			}
			if len(routeIDs) == 0 {
				return errors.New("--routes is required")
			}
			if operation == "delete" {
				if err := confirm(cmd, yes, "Delete the selected routes?"); err != nil {
					return err
				}
			}
			body := map[string]interface{}{"routeIds": routeIDs, "operation": operation}
			if err := addOptionalInt(body, "appEnvironmentId", args[0], "ENVIRONMENT_ID"); err != nil {
				return err
			}
			return postAndPrintOperation(cmd, out, wait, "/app-routes/actions/bulk", body)
		},
	}
	bulk.Flags().StringArrayVar(&routes, "routes", nil, "Route IDs, comma-separated or repeated")
	bulk.Flags().StringVar(&operation, "operation", "", "enable, disable or delete")
	bulk.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm deletion without prompting")
	addWaitFlags(bulk, &wait)
	cmd.AddCommand(bulk)
	for _, child := range cmd.Commands() {
		if child.Name() == "settings" {
			child.AddCommand(newRouteSettingsReplaceCommand("replace ROUTE_ID", "Replace all setting overrides of a route; a setting left out is removed", "/app-routes/%s/settings", out))
		}
	}
}

func addOrgSettingsCommands(cmd *cobra.Command, out outputOptions) {
	var retention string
	settings := &cobra.Command{Use: "settings", Short: "Manage organization defaults"}
	update := &cobra.Command{
		Use:   "update ORG_ID",
		Short: "Update organization defaults",
		Long:  "Update organization defaults. A new build image retention also applies to app environments that still use the previous organization value; environments with their own value keep it.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireFlag(retention, "--build-image-retention"); err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			body := map[string]interface{}{"buildImageCleanup": map[string]interface{}{"retention": retention}}
			if err := client.Put(cmd.Context(), escapedPath("/orgs/%s/settings", args[0]), nil, body, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, append(append([]string{}, orgColumns...), "settings.buildImageCleanup.retention"))
		},
	}
	update.Flags().StringVar(&retention, "build-image-retention", "", buildImageRetentionFlagUsage)
	settings.AddCommand(update)
	cmd.AddCommand(settings)
}

func addBuildUpdateCommands(cmd *cobra.Command, out outputOptions) {
	var yes bool
	voidImages := &cobra.Command{
		Use:   "void-images ID",
		Short: "Remove a finished, unused build's images so it can no longer be deployed",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := confirm(cmd, yes, "Void the build's images?"); err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), escapedPath("/app-builds/%s/actions/void-images", args[0]), nil, nil, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, buildColumns)
		},
	}
	voidImages.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm without prompting")
	cmd.AddCommand(voidImages)
}

func addStackUpdateCommands(cmd *cobra.Command, out outputOptions) {
	cmd.AddCommand(newServiceChildCreateCommand("ignore-service-revision STACK_ID", "Stop offering one service revision as an update for a stack service", "/stacks/%s/actions/ignore-service-revision", stackColumns, out, []jsonFlagSpec{
		intJSONFlag("stack-service", "stackServiceId", "Root stack service whose update to skip; its derivatives skip it too", true),
		intJSONFlag("service-revision", "serviceRevId", "Service revision to stop offering", true),
	}))
}

func addIntegrationUsageCommands(cmd *cobra.Command, out outputOptions) {
	var page, pageSize int
	usages := &cobra.Command{
		Use:   "usages ID",
		Short: "List what uses an integration before you change or delete it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			query := url.Values{}
			if page > 0 {
				query.Set("page", strconv.Itoa(page))
			}
			if pageSize > 0 {
				query.Set("pageSize", strconv.Itoa(pageSize))
			}
			var result interface{}
			if err := client.Get(cmd.Context(), escapedPath("/integrations/usages/%s", args[0]), query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, integrationUsageColumns)
		},
	}
	usages.Flags().IntVar(&page, "page", 0, "Page number")
	usages.Flags().IntVar(&pageSize, "page-size", 0, "Page size")
	cmd.AddCommand(usages)
}

// objectItem returns a response object, unwrapped like table output, or an empty map.
func objectItem(value interface{}) map[string]interface{} {
	if item, ok := normalizeItem(value).(map[string]interface{}); ok {
		return item
	}
	return map[string]interface{}{}
}
