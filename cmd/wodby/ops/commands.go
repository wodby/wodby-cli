package ops

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/wodby/wodby-cli/pkg/api/rest"
)

var (
	orgColumns            = []string{"id", "name", "title", "domain"}
	memberColumns         = []string{"id", "member", "email", "role", "status"}
	projectColumns        = []string{"id", "name", "title"}
	envColumns            = []string{"id", "name", "title", "type"}
	databaseColumns       = []string{"id", "name", "title", "status", "kind", "type", "version", "env", "integration", "region", "zone"}
	clusterColumns        = []string{"id", "name", "title", "status", "integration", "region", "zone", "version", "serverless"}
	clusterGetColumns     = []string{"id", "name", "title", "status", "integration", "region", "zone", "kubernetesVersion", "infraVersion", "publicIp", "serverless", "instances"}
	integrationColumns    = []string{"id", "name", "title", "scope", "status", "provider", "createdAt"}
	providerColumns       = []string{"id", "name", "title", "status", "public", "revId"}
	stackColumns          = []string{"id", "name", "title", "status", "public", "revId", "latestRevNumber", "createdAt", "updatedAt"}
	stackGetColumns       = append(append([]string{}, stackColumns...), "services")
	catalogServiceColumns = []string{"id", "name", "title", "type", "status", "public", "external", "revId", "latestRevNumber"}
	appColumns            = []string{"id", "name", "title", "status", "stack", "clusterApp"}
	appGetColumns         = append(append([]string{}, appColumns...), "instances")
	instanceColumns       = []string{"id", "name", "title", "status", "app", "stack", "env", "cluster", "domain"}
	serviceColumns        = []string{"id", "name", "title", "type", "status", "version", "replicas", "disabled", "main", "needsRebuild", "needsRedeploy", "configurationReady"}
	routeColumns          = []string{"id", "host", "path", "pathType", "action", "status", "service", "port", "main", "primary", "disabled"}
	buildColumns          = []string{"id", "number", "status", "instance", "service", "task", "gitRefType", "gitRef", "commitHash", "createdAt"}
	deploymentColumns     = []string{"id", "number", "status", "instance", "task", "skipRollback", "createdAt", "startedAt", "endedAt"}
	backupColumns         = []string{"id", "name", "status", "instance", "service", "database", "databaseDb", "task", "createdAt"}
	importColumns         = []string{"id", "name", "source", "status", "task", "instance", "service", "database", "databaseDb", "createdAt"}
	taskColumns           = []string{"id", "title", "status", "author", "createdAt", "duration"}
	taskGetColumns        = []string{"id", "title", "status", "author", "app", "instance", "service", "database", "databaseDb", "createdAt", "duration", "jobs"}
	operationColumns      = []string{"success", "task"}
)

func Commands() []*cobra.Command {
	return []*cobra.Command{
		newOrgCommand(),
		newMemberCommand(),
		newProjectCommand(),
		newEnvCommand(),
		newDatabaseCommand(),
		newClusterCommand(),
		newIntegrationCommand(),
		newProviderCommand(),
		newStackCommand(),
		newServiceCommand(),
		newAppCommand(),
		newAppInstanceCommand("instance", "Manage app instances"),
		newBuildCommand(),
		newDeploymentCommand(),
		newBackupCommand(),
		newImportCommand(),
		newTaskCommand(),
		newAppRouteCommand("route", "Manage app routes"),
	}
}

func newOrgCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   "org",
		Short: "Manage organization context",
	}
	addOutputFlag(cmd, &out)

	cmd.AddCommand(&cobra.Command{
		Use:   "current",
		Short: "Show organizations available to the current credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/orgs", nil, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, orgColumns)
		},
	})

	return cmd
}

func newMemberCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     "member",
		Aliases: []string{"members"},
		Short:   "Manage organization members",
	}
	addOutputFlag(cmd, &out)

	var orgID string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List organization members",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
			if err != nil {
				return err
			}
			query := url.Values{"orgId": []string{resolvedOrgID}}
			var result interface{}
			if err := client.Get(cmd.Context(), "/org-memberships", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, memberColumns)
		},
	}
	listCmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")

	cmd.AddCommand(listCmd)
	return cmd
}

func newProjectCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects",
	}
	addOutputFlag(cmd, &out)

	var orgID string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
			if err != nil {
				return err
			}
			query := url.Values{"orgId": []string{resolvedOrgID}}
			var result interface{}
			if err := client.Get(cmd.Context(), "/projects", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, projectColumns)
		},
	}
	listCmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")

	getCmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrint(cmd, out, "/projects/"+args[0], projectColumns)
		},
	}

	cmd.AddCommand(listCmd, getCmd)
	return cmd
}

func newEnvCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage environments",
	}
	addOutputFlag(cmd, &out)

	var orgID string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List environments",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
			if err != nil {
				return err
			}
			query := url.Values{"orgId": []string{resolvedOrgID}}
			var result interface{}
			if err := client.Get(cmd.Context(), "/envs", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, envColumns)
		},
	}
	listCmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")

	getCmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get environment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrint(cmd, out, "/envs/"+args[0], envColumns)
		},
	}

	cmd.AddCommand(listCmd, getCmd, newEnvCreateCommand(out), newEnvUpdateCommand(out), newDeleteCommand("delete ID", "Delete environment", "/envs/", envColumns, out))
	return cmd
}

func newEnvCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var orgID, name, title, envType string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create environment",
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
				if err := requireFlag(name, "--name"); err != nil {
					return err
				}
				if err := requireFlag(title, "--title"); err != nil {
					return err
				}
				if err := requireFlag(envType, "--type"); err != nil {
					return err
				}
				resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
				if err != nil {
					return err
				}
				orgIDNumber, err := strconv.Atoi(resolvedOrgID)
				if err != nil {
					return errors.WithStack(err)
				}
				requestBody = map[string]interface{}{
					"orgId": orgIDNumber,
					"name":  name,
					"title": title,
					"type":  envType,
				}
			}

			var result interface{}
			if err := client.Post(cmd.Context(), "/envs", nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, envColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	cmd.Flags().StringVar(&name, "name", "", "Environment machine name")
	cmd.Flags().StringVar(&title, "title", "", "Environment title")
	cmd.Flags().StringVar(&envType, "type", "", "Environment type: prod, staging, test, dev, or feature")
	return cmd
}

func newEnvUpdateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var name, title, envType string
	cmd := &cobra.Command{
		Use:   "update ID",
		Short: "Update environment",
		Args:  cobra.ExactArgs(1),
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
				if err := requireFlag(name, "--name"); err != nil {
					return err
				}
				if err := requireFlag(title, "--title"); err != nil {
					return err
				}
				if err := requireFlag(envType, "--type"); err != nil {
					return err
				}
				requestBody = map[string]interface{}{
					"name":  name,
					"title": title,
					"type":  envType,
				}
			}
			var result interface{}
			if err := client.Put(cmd.Context(), "/envs/"+args[0], nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, envColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&name, "name", "", "Environment machine name")
	cmd.Flags().StringVar(&title, "title", "", "Environment title")
	cmd.Flags().StringVar(&envType, "type", "", "Environment type: prod, staging, test, dev, or feature")
	return cmd
}

func newDatabaseCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     "database",
		Aliases: []string{"databases"},
		Short:   "Manage databases",
	}
	addOutputFlag(cmd, &out)

	var orgID, projectIDs, kind string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List databases",
		RunE: func(cmd *cobra.Command, args []string) error {
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
			addQuery(query, "kind", kind)
			var result interface{}
			if err := client.Get(cmd.Context(), "/databases", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, databaseColumns)
		},
	}
	listCmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	listCmd.Flags().StringVar(&projectIDs, "project", "", "Project ID or comma-separated project IDs")
	listCmd.Flags().StringVar(&kind, "kind", "", "Database kind")

	getCmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get database",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrint(cmd, out, "/databases/"+args[0], databaseColumns)
		},
	}

	cmd.AddCommand(listCmd, getCmd, newDatabaseCreateCommand(out), newDatabaseUpdateCommand(out), newDeleteCommand("delete ID", "Delete database", "/databases/", databaseColumns, out))
	return cmd
}

func newDatabaseCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var orgID, projectID, envID, integrationKindID, name, title, dbType, version, machineType, region, zone, password, residedClusterID string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create database",
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
				if err := requireFlag(envID, "--env"); err != nil {
					return err
				}
				if err := requireFlag(integrationKindID, "--integration-kind"); err != nil {
					return err
				}
				if err := requireFlag(name, "--name"); err != nil {
					return err
				}
				if err := requireFlag(title, "--title"); err != nil {
					return err
				}
				if err := requireFlag(dbType, "--type"); err != nil {
					return err
				}
				if err := requireFlag(version, "--version"); err != nil {
					return err
				}
				if err := requireFlag(machineType, "--machine-type"); err != nil {
					return err
				}
				envIDNumber, err := strconv.Atoi(envID)
				if err != nil {
					return errors.Wrap(err, "invalid --env")
				}
				integrationKindIDNumber, err := strconv.Atoi(integrationKindID)
				if err != nil {
					return errors.Wrap(err, "invalid --integration-kind")
				}
				values := map[string]interface{}{
					"envId":             envIDNumber,
					"integrationKindId": integrationKindIDNumber,
					"name":              name,
					"title":             title,
					"type":              dbType,
					"version":           version,
					"machineType":       machineType,
				}
				resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
				if err != nil {
					return err
				}
				if err := addOptionalInt(values, "orgId", resolvedOrgID, "--org"); err != nil {
					return err
				}
				if err := addOptionalInt(values, "projectId", projectID, "--project"); err != nil {
					return err
				}
				if err := addOptionalInt(values, "residedClusterId", residedClusterID, "--resided-cluster"); err != nil {
					return err
				}
				addOptionalString(values, "region", region)
				addOptionalString(values, "zone", zone)
				addOptionalString(values, "password", password)
				if value, ok := changedBool(cmd, "high-availability"); ok {
					values["highAvailability"] = value
				}
				if value, ok := changedBool(cmd, "storage-autoscaling"); ok {
					values["storageAutoscaling"] = value
				}
				if value, ok := changedInt(cmd, "storage-size"); ok {
					values["storageSize"] = value
				}
				if value, ok := changedInt(cmd, "iops"); ok {
					values["iops"] = value
				}
				requestBody = values
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/databases", nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, databaseColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	cmd.Flags().StringVar(&projectID, "project", "", "Project ID")
	cmd.Flags().StringVar(&envID, "env", "", "Environment ID")
	cmd.Flags().StringVar(&integrationKindID, "integration-kind", "", "Integration kind ID")
	cmd.Flags().StringVar(&name, "name", "", "Database machine name")
	cmd.Flags().StringVar(&title, "title", "", "Database title")
	cmd.Flags().StringVar(&dbType, "type", "", "Database type")
	cmd.Flags().StringVar(&version, "version", "", "Database version")
	cmd.Flags().StringVar(&machineType, "machine-type", "", "Machine type")
	cmd.Flags().StringVar(&region, "region", "", "Provider region")
	cmd.Flags().StringVar(&zone, "zone", "", "Provider zone")
	cmd.Flags().StringVar(&password, "password", "", "Database password")
	cmd.Flags().StringVar(&residedClusterID, "resided-cluster", "", "Resided cluster ID")
	cmd.Flags().Bool("high-availability", false, "Enable high availability")
	cmd.Flags().Bool("storage-autoscaling", false, "Enable storage autoscaling")
	cmd.Flags().Int("storage-size", 0, "Storage size")
	cmd.Flags().Int("iops", 0, "Provisioned IOPS")
	return cmd
}

func newDatabaseUpdateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var title string
	cmd := &cobra.Command{
		Use:   "update ID",
		Short: "Update database",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if err := requireFlag(title, "--title"); err != nil {
					return err
				}
				requestBody = map[string]interface{}{"title": title}
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), "/databases/"+args[0], nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, databaseColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&title, "title", "", "Database title")
	return cmd
}

func newClusterCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     "cluster",
		Aliases: []string{"clusters"},
		Short:   "Manage clusters",
	}
	addOutputFlag(cmd, &out)

	var orgID, projectIDs, integrationID string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List clusters",
		RunE: func(cmd *cobra.Command, args []string) error {
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
			addQuery(query, "integrationId", integrationID)
			var result interface{}
			if err := client.Get(cmd.Context(), "/clusters", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, clusterColumns)
		},
	}
	listCmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	listCmd.Flags().StringVar(&projectIDs, "project", "", "Project ID or comma-separated project IDs")
	listCmd.Flags().StringVar(&integrationID, "integration", "", "Integration ID")

	getCmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrintWithInstances(cmd, out, "/clusters/"+args[0], clusterGetColumns, "clusterId", args[0])
		},
	}

	cmd.AddCommand(listCmd, getCmd, newClusterCreateCommand(out), newClusterUpdateCommand(out), newDeleteCommand("delete ID", "Delete cluster", "/clusters/", clusterColumns, out))
	return cmd
}

func newClusterCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var orgID, projectID, integrationID, name, title, region, zone, version, machineType, billingOption string
	var serverless, disableMonitoring bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create cluster",
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
				if err := requireFlag(integrationID, "--integration"); err != nil {
					return err
				}
				if err := requireFlag(name, "--name"); err != nil {
					return err
				}
				if err := requireFlag(title, "--title"); err != nil {
					return err
				}
				integrationIDNumber, err := strconv.Atoi(integrationID)
				if err != nil {
					return errors.Wrap(err, "invalid --integration")
				}
				values := map[string]interface{}{
					"integrationId":     integrationIDNumber,
					"name":              name,
					"title":             title,
					"serverless":        serverless,
					"disableMonitoring": disableMonitoring,
				}
				resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
				if err != nil {
					return err
				}
				if err := addOptionalInt(values, "orgId", resolvedOrgID, "--org"); err != nil {
					return err
				}
				if err := addOptionalInt(values, "projectId", projectID, "--project"); err != nil {
					return err
				}
				addOptionalString(values, "region", region)
				addOptionalString(values, "zone", zone)
				addOptionalString(values, "version", version)
				addOptionalString(values, "machineType", machineType)
				addOptionalString(values, "billingOption", billingOption)
				if value, ok := changedInt(cmd, "min-node-count"); ok {
					values["minNodeCount"] = value
				}
				if value, ok := changedInt(cmd, "max-node-count"); ok {
					values["maxNodeCount"] = value
				}
				if value, ok := changedInt(cmd, "node-disk-size"); ok {
					values["nodeDiskSize"] = value
				}
				if value, ok := changedBool(cmd, "single-node"); ok {
					values["singleNode"] = value
				}
				requestBody = values
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/clusters", nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, clusterColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	cmd.Flags().StringVar(&projectID, "project", "", "Project ID")
	cmd.Flags().StringVar(&integrationID, "integration", "", "Integration ID")
	cmd.Flags().StringVar(&name, "name", "", "Cluster machine name")
	cmd.Flags().StringVar(&title, "title", "", "Cluster title")
	cmd.Flags().BoolVar(&serverless, "serverless", false, "Create a serverless cluster")
	cmd.Flags().BoolVar(&disableMonitoring, "disable-monitoring", false, "Disable cluster monitoring")
	cmd.Flags().StringVar(&region, "region", "", "Provider region")
	cmd.Flags().StringVar(&zone, "zone", "", "Provider zone")
	cmd.Flags().StringVar(&version, "version", "", "Kubernetes version")
	cmd.Flags().StringVar(&machineType, "machine-type", "", "Machine type")
	cmd.Flags().StringVar(&billingOption, "billing-option", "", "Provider billing option")
	cmd.Flags().Int("min-node-count", 0, "Minimum node count")
	cmd.Flags().Int("max-node-count", 0, "Maximum node count")
	cmd.Flags().Int("node-disk-size", 0, "Node disk size")
	cmd.Flags().Bool("single-node", false, "Create a single-node cluster")
	return cmd
}

func newClusterUpdateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var title string
	cmd := &cobra.Command{
		Use:   "update ID",
		Short: "Update cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if err := requireFlag(title, "--title"); err != nil {
					return err
				}
				requestBody = map[string]interface{}{"title": title}
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), "/clusters/"+args[0], nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, clusterColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&title, "title", "", "Cluster title")
	return cmd
}

func newIntegrationCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     "integration",
		Aliases: []string{"integrations"},
		Short:   "Manage integrations",
	}
	addOutputFlag(cmd, &out)

	var orgID, projectIDs, labels string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List integrations",
		RunE: func(cmd *cobra.Command, args []string) error {
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
			addQuery(query, "labels", labels)
			var result interface{}
			if err := client.Get(cmd.Context(), "/integrations", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, integrationColumns)
		},
	}
	listCmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	listCmd.Flags().StringVar(&projectIDs, "project", "", "Project ID or comma-separated project IDs")
	listCmd.Flags().StringVar(&labels, "labels", "", "Comma-separated labels")

	getCmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get integration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrint(cmd, out, "/integrations/"+args[0], integrationColumns)
		},
	}

	cmd.AddCommand(listCmd, getCmd, newIntegrationCreateCommand(out), newIntegrationUpdateCommand(out), newDeleteCommand("delete ID", "Delete integration", "/integrations/", integrationColumns, out))
	return cmd
}

func newIntegrationCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var orgID, projectID, providerID, name, title, auth, scope string
	var kinds []string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create integration",
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
				if err := requireFlag(providerID, "--provider"); err != nil {
					return err
				}
				if err := requireFlag(name, "--name"); err != nil {
					return err
				}
				if err := requireFlag(title, "--title"); err != nil {
					return err
				}
				if len(kinds) == 0 {
					return errors.New("--kind is required")
				}
				providerIDNumber, err := strconv.Atoi(providerID)
				if err != nil {
					return errors.Wrap(err, "invalid --provider")
				}
				values := map[string]interface{}{
					"providerId": providerIDNumber,
					"name":       name,
					"title":      title,
					"kinds":      kinds,
				}
				resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
				if err != nil {
					return err
				}
				if err := addOptionalInt(values, "orgId", resolvedOrgID, "--org"); err != nil {
					return err
				}
				if err := addOptionalInt(values, "projectId", projectID, "--project"); err != nil {
					return err
				}
				addOptionalString(values, "auth", auth)
				addOptionalString(values, "scope", scope)
				requestBody = values
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/integrations", nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, integrationColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	cmd.Flags().StringVar(&projectID, "project", "", "Project ID")
	cmd.Flags().StringVar(&providerID, "provider", "", "Provider ID")
	cmd.Flags().StringVar(&name, "name", "", "Integration machine name")
	cmd.Flags().StringVar(&title, "title", "", "Integration title")
	cmd.Flags().StringArrayVar(&kinds, "kind", nil, "Integration kind; repeatable")
	cmd.Flags().StringVar(&auth, "auth", "", "Integration auth mode")
	cmd.Flags().StringVar(&scope, "scope", "", "Integration scope")
	return cmd
}

func newIntegrationUpdateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var name, title, scope string
	var kinds []string
	cmd := &cobra.Command{
		Use:   "update ID",
		Short: "Update integration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if err := requireFlag(name, "--name"); err != nil {
					return err
				}
				if err := requireFlag(title, "--title"); err != nil {
					return err
				}
				if len(kinds) == 0 {
					return errors.New("--kind is required")
				}
				values := map[string]interface{}{
					"name":  name,
					"title": title,
					"kinds": kinds,
				}
				addOptionalString(values, "scope", scope)
				requestBody = values
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), "/integrations/"+args[0], nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, integrationColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&name, "name", "", "Integration machine name")
	cmd.Flags().StringVar(&title, "title", "", "Integration title")
	cmd.Flags().StringArrayVar(&kinds, "kind", nil, "Integration kind; repeatable")
	cmd.Flags().StringVar(&scope, "scope", "", "Integration scope")
	return cmd
}

func newProviderCommand() *cobra.Command {
	return newCatalogCommand("provider", "providers", "Manage providers", "/providers", providerColumns, true)
}

func newStackCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     "stack",
		Aliases: []string{"stacks"},
		Short:   "Manage stacks",
	}
	addOutputFlag(cmd, &out)
	cmd.AddCommand(newCatalogListCommand("list", "List stacks", "/stacks", stackColumns, out, false), newGetCommand("get ID", "Get stack", "/stacks/", stackGetColumns, out))
	return cmd
}

func newServiceCommand() *cobra.Command {
	return newCatalogCommand("service", "services", "Manage services", "/services", catalogServiceColumns, false)
}

func newCatalogCommand(use string, alias string, short string, path string, columns []string, excludePublic bool) *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     use,
		Aliases: []string{alias},
		Short:   short,
	}
	addOutputFlag(cmd, &out)
	cmd.AddCommand(newCatalogListCommand("list", "List "+alias, path, columns, out, excludePublic), newGetCommand("get ID", "Get "+use, path+"/", columns, out))
	return cmd
}

func newCatalogListCommand(use string, short string, path string, columns []string, out outputOptions, excludePublic bool) *cobra.Command {
	var orgID, projectIDs, search string
	var page, pageSize int
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			query := url.Values{}
			addQuery(query, "orgId", orgID)
			addQuery(query, "projectIds", projectIDs)
			addQuery(query, "search", search)
			addPagination(query, page, pageSize)
			if excludePublic {
				addBoolQuery(cmd, query, "excludePublic", "exclude-public")
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), path, query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID")
	cmd.Flags().StringVar(&projectIDs, "project", "", "Project ID or comma-separated project IDs")
	cmd.Flags().StringVar(&search, "search", "", "Search query")
	cmd.Flags().IntVar(&page, "page", 0, "Page number")
	cmd.Flags().IntVar(&pageSize, "page-size", 0, "Page size")
	if excludePublic {
		cmd.Flags().Bool("exclude-public", false, "Exclude public resources")
	}
	return cmd
}

func newAppCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   "app",
		Short: "Manage apps",
	}
	addOutputFlag(cmd, &out)

	var orgID, projectIDs string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List apps",
		RunE: func(cmd *cobra.Command, args []string) error {
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
			addBoolQuery(cmd, query, "clusterApp", "cluster-app")
			var result interface{}
			if err := client.Get(cmd.Context(), "/apps", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, appColumns)
		},
	}
	listCmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	listCmd.Flags().StringVar(&projectIDs, "project", "", "Project ID or comma-separated project IDs")
	listCmd.Flags().Bool("cluster-app", false, "Filter cluster apps")

	getCmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get app",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrintWithInstances(cmd, out, "/apps/"+args[0], appGetColumns, "appId", args[0])
		},
	}
	statusCmd := &cobra.Command{
		Use:   "status ID",
		Short: "Show app status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrint(cmd, out, "/apps/"+args[0], appColumns)
		},
	}

	cmd.AddCommand(listCmd, getCmd, statusCmd)
	cmd.AddCommand(newAppInstanceCommand("instance", "Manage app instances"))
	cmd.AddCommand(newAppServiceCommand())
	cmd.AddCommand(newAppRouteCommand("route", "Manage app routes"))
	return cmd
}

func newAppInstanceCommand(use string, short string) *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
	}
	addOutputFlag(cmd, &out)

	var orgID, projectIDs, appID, clusterID string
	var clusterApp bool
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List app instances",
		RunE: func(cmd *cobra.Command, args []string) error {
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
			query.Set("clusterApp", strconv.FormatBool(clusterApp))
			var result interface{}
			if err := client.Get(cmd.Context(), "/app-instances", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, instanceColumns)
		},
	}
	listCmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	listCmd.Flags().StringVar(&projectIDs, "project", "", "Project ID or comma-separated project IDs")
	listCmd.Flags().StringVar(&appID, "app", "", "App ID")
	listCmd.Flags().StringVar(&clusterID, "cluster", "", "Cluster ID")
	listCmd.Flags().BoolVar(&clusterApp, "cluster-app", false, "Filter cluster app instances")

	getCmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get app instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrint(cmd, out, "/app-instances/"+args[0], instanceColumns)
		},
	}
	statusCmd := &cobra.Command{
		Use:   "status ID",
		Short: "Show app instance status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrint(cmd, out, "/app-instances/"+args[0], instanceColumns)
		},
	}

	cmd.AddCommand(listCmd, getCmd, statusCmd)
	return cmd
}

func newAppServiceCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage app services",
	}
	addOutputFlag(cmd, &out)

	var instanceID string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List app services",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireFlag(instanceID, "--instance"); err != nil {
				return err
			}
			query := url.Values{"appInstanceId": []string{instanceID}}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/app-services", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, serviceColumns)
		},
	}
	listCmd.Flags().StringVar(&instanceID, "instance", "", "App instance ID")

	getCmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get app service",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrint(cmd, out, "/app-services/"+args[0], serviceColumns)
		},
	}

	cmd.AddCommand(listCmd, getCmd, newAppServiceUpdateCommand(out), newAppServiceActionCommand(out))
	return cmd
}

func newAppServiceUpdateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	cmd := &cobra.Command{
		Use:   "update ID",
		Short: "Update app service",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if !hasChangedFlags(cmd.Flags(), "disabled", "main", "replicas", "version") {
					return errors.New("pass at least one update flag or provide --data/--file")
				}
				values := map[string]interface{}{}
				if value, ok := changedBool(cmd, "disabled"); ok {
					values["disabled"] = value
				}
				if value, ok := changedBool(cmd, "main"); ok {
					values["main"] = value
				}
				if value, ok := changedInt(cmd, "replicas"); ok {
					values["replicas"] = value
				}
				if value, ok := changedString(cmd, "version"); ok {
					values["version"] = value
				}
				requestBody = values
			}

			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), "/app-services/"+args[0], nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, serviceColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().Bool("disabled", false, "Set disabled state")
	cmd.Flags().Bool("main", false, "Set main service state")
	cmd.Flags().Int("replicas", 0, "Set replicas")
	cmd.Flags().String("version", "", "Set service version")
	return cmd
}

func newAppServiceActionCommand(out outputOptions) *cobra.Command {
	wait := waitOptions{}
	cmd := &cobra.Command{
		Use:   "action ID ACTION",
		Short: "Run app service action",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), fmt.Sprintf("/app-services/%s/actions/%s", args[0], args[1]), nil, nil, &result); err != nil {
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
		},
	}
	addWaitFlags(cmd, &wait)
	return cmd
}

func newAppRouteCommand(use string, short string) *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
	}
	addOutputFlag(cmd, &out)

	var instanceID string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List app routes",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireFlag(instanceID, "--instance"); err != nil {
				return err
			}
			query := url.Values{"appInstanceId": []string{instanceID}}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/app-routes", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, routeColumns)
		},
	}
	listCmd.Flags().StringVar(&instanceID, "instance", "", "App instance ID")

	getCmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get app route",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrint(cmd, out, "/app-routes/"+args[0], routeColumns)
		},
	}

	cmd.AddCommand(listCmd, getCmd, newAppRouteCreateCommand(out), newAppRouteUpdateCommand(out), newDeleteCommand("delete ID", "Delete app route", "/app-routes/", routeColumns, out))
	return cmd
}

func newAppRouteCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	var serviceID, host, path, pathType, action string
	var port int
	var main, primary, letsencrypt bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create app route",
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if err := requireFlag(serviceID, "--service"); err != nil {
					return err
				}
				if err := requireFlag(host, "--host"); err != nil {
					return err
				}
				if err := requireIntFlag(port, "--port"); err != nil {
					return err
				}
				appServiceID, err := strconv.Atoi(serviceID)
				if err != nil {
					return errors.WithStack(err)
				}
				requestBody = bodyFromMap(map[string]interface{}{
					"appServiceId": appServiceID,
					"host":         host,
					"port":         port,
					"main":         main,
					"primary":      primary,
					"letsencrypt":  letsencrypt,
					"path":         path,
					"pathType":     pathType,
					"action":       action,
				})
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/app-routes", nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, routeColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringVar(&serviceID, "service", "", "App service ID")
	cmd.Flags().StringVar(&host, "host", "", "Route host")
	cmd.Flags().IntVar(&port, "port", 0, "Service port")
	cmd.Flags().BoolVar(&main, "main", false, "Make route the app instance main route")
	cmd.Flags().BoolVar(&primary, "primary", false, "Make route primary for the service endpoint")
	cmd.Flags().BoolVar(&letsencrypt, "letsencrypt", false, "Request Let's Encrypt certificate")
	cmd.Flags().StringVar(&path, "path", "", "Route path")
	cmd.Flags().StringVar(&pathType, "path-type", "", "Route path type: PREFIX or EXACT")
	cmd.Flags().StringVar(&action, "action", "", "Route action: BACKEND or REDIRECT")
	return cmd
}

func newAppRouteUpdateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	cmd := &cobra.Command{
		Use:   "update ID",
		Short: "Update app route",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if !hasChangedFlags(cmd.Flags(), "disabled", "main", "primary", "path", "path-type", "action", "redirect-host", "redirect-path", "redirect-scheme", "redirect-status-code") {
					return errors.New("pass at least one update flag or provide --data/--file")
				}
				values := map[string]interface{}{}
				if value, ok := changedBool(cmd, "disabled"); ok {
					values["disabled"] = value
				}
				if value, ok := changedBool(cmd, "main"); ok {
					values["main"] = value
				}
				if value, ok := changedBool(cmd, "primary"); ok {
					values["primary"] = value
				}
				if value, ok := changedString(cmd, "path"); ok {
					values["path"] = value
				}
				if value, ok := changedString(cmd, "path-type"); ok {
					values["pathType"] = value
				}
				if value, ok := changedString(cmd, "action"); ok {
					values["action"] = value
				}
				if value, ok := changedString(cmd, "redirect-host"); ok {
					values["redirectHost"] = value
				}
				if value, ok := changedString(cmd, "redirect-path"); ok {
					values["redirectPath"] = value
				}
				if value, ok := changedString(cmd, "redirect-scheme"); ok {
					values["redirectScheme"] = value
				}
				if value, ok := changedInt(cmd, "redirect-status-code"); ok {
					values["redirectStatusCode"] = value
				}
				requestBody = values
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), "/app-routes/"+args[0], nil, requestBody, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, routeColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().Bool("disabled", false, "Set disabled state")
	cmd.Flags().Bool("main", false, "Set main route state")
	cmd.Flags().Bool("primary", false, "Set primary route state")
	cmd.Flags().String("path", "", "Route path")
	cmd.Flags().String("path-type", "", "Route path type: PREFIX or EXACT")
	cmd.Flags().String("action", "", "Route action: BACKEND or REDIRECT")
	cmd.Flags().String("redirect-host", "", "Redirect host")
	cmd.Flags().String("redirect-path", "", "Redirect path")
	cmd.Flags().String("redirect-scheme", "", "Redirect scheme")
	cmd.Flags().Int("redirect-status-code", 0, "Redirect status code")
	return cmd
}

func newBuildCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Manage app builds",
	}
	addOutputFlag(cmd, &out)

	var instanceID string
	var page, pageSize int
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List builds",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireFlag(instanceID, "--instance"); err != nil {
				return err
			}
			query := url.Values{"appInstanceId": []string{instanceID}}
			if page != 0 {
				query.Set("page", strconv.Itoa(page))
			}
			if pageSize != 0 {
				query.Set("pageSize", strconv.Itoa(pageSize))
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/app-builds", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, buildColumns)
		},
	}
	listCmd.Flags().StringVar(&instanceID, "instance", "", "App instance ID")
	listCmd.Flags().IntVar(&page, "page", 0, "Page number")
	listCmd.Flags().IntVar(&pageSize, "page-size", 0, "Page size")

	getCmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get build",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrint(cmd, out, "/app-builds/"+args[0], buildColumns)
		},
	}

	cmd.AddCommand(listCmd, getCmd, newBuildDeployCommand(out))
	return cmd
}

func newBuildDeployCommand(out outputOptions) *cobra.Command {
	wait := waitOptions{}
	cmd := &cobra.Command{
		Use:   "deploy ID",
		Short: "Deploy build",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/app-builds/"+args[0]+"/deploy", nil, nil, &result); err != nil {
				return err
			}
			if wait.wait {
				deploymentID := firstID(result)
				if deploymentID != "" {
					result, err = waitForDeployment(cmd.Context(), client, deploymentID, wait.timeout)
					if err != nil {
						return err
					}
				}
			}
			return printClientResult(cmd, client, out, result, deploymentColumns)
		},
	}
	addWaitFlags(cmd, &wait)
	return cmd
}

func newDeploymentCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   "deployment",
		Short: "Manage deployments",
	}
	addOutputFlag(cmd, &out)

	var instanceID string
	var page, pageSize int
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List deployments",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireFlag(instanceID, "--instance"); err != nil {
				return err
			}
			query := url.Values{"appInstanceId": []string{instanceID}}
			if page != 0 {
				query.Set("page", strconv.Itoa(page))
			}
			if pageSize != 0 {
				query.Set("pageSize", strconv.Itoa(pageSize))
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/app-deployments", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, deploymentColumns)
		},
	}
	listCmd.Flags().StringVar(&instanceID, "instance", "", "App instance ID")
	listCmd.Flags().IntVar(&page, "page", 0, "Page number")
	listCmd.Flags().IntVar(&pageSize, "page-size", 0, "Page size")

	getCmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrint(cmd, out, "/app-deployments/"+args[0], deploymentColumns)
		},
	}
	waitCmd := &cobra.Command{
		Use:   "wait ID",
		Short: "Wait for deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			timeout, _ := cmd.Flags().GetDuration("timeout")
			result, err := waitForDeployment(cmd.Context(), client, args[0], timeout)
			if err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, deploymentColumns)
		},
	}
	waitCmd.Flags().Duration("timeout", 10*time.Minute, "Maximum time to wait")

	cmd.AddCommand(listCmd, getCmd, waitCmd, newDeploymentCreateCommand(out), newDeploymentRedeployCommand(out))
	return cmd
}

func newDeploymentCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	wait := waitOptions{}
	var services []string
	var force, skipPostDeploy, skipRollback bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create deployment",
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if len(services) == 0 {
					return errors.New("--service is required")
				}
				serviceInputs := make([]map[string]interface{}, 0, len(services))
				for _, service := range services {
					serviceID, err := strconv.Atoi(service)
					if err != nil {
						return errors.WithStack(err)
					}
					serviceInputs = append(serviceInputs, map[string]interface{}{
						"appServiceId":       serviceID,
						"force":              force,
						"skipPostDeployment": skipPostDeploy,
					})
				}
				requestBody = map[string]interface{}{
					"services":     serviceInputs,
					"skipRollback": skipRollback,
				}
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/app-deployments", nil, requestBody, &result); err != nil {
				return err
			}
			if wait.wait {
				deploymentID := firstID(result)
				if deploymentID != "" {
					result, err = waitForDeployment(cmd.Context(), client, deploymentID, wait.timeout)
					if err != nil {
						return err
					}
				}
			}
			return printClientResult(cmd, client, out, result, deploymentColumns)
		},
	}
	addBodyFlags(cmd, &body)
	addWaitFlags(cmd, &wait)
	cmd.Flags().StringArrayVar(&services, "service", nil, "App service ID to deploy; repeatable")
	cmd.Flags().BoolVar(&force, "force", false, "Force service deployment")
	cmd.Flags().BoolVar(&skipPostDeploy, "skip-post-deploy", false, "Skip post-deployment scripts")
	cmd.Flags().BoolVar(&skipRollback, "skip-rollback", false, "Skip rollback on failure")
	return cmd
}

func newDeploymentRedeployCommand(out outputOptions) *cobra.Command {
	wait := waitOptions{}
	cmd := &cobra.Command{
		Use:   "redeploy ID",
		Short: "Redeploy deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/app-deployments/"+args[0]+"/redeploy", nil, nil, &result); err != nil {
				return err
			}
			if wait.wait {
				deploymentID := firstID(result)
				if deploymentID != "" {
					result, err = waitForDeployment(cmd.Context(), client, deploymentID, wait.timeout)
					if err != nil {
						return err
					}
				}
			}
			return printClientResult(cmd, client, out, result, deploymentColumns)
		},
	}
	addWaitFlags(cmd, &wait)
	return cmd
}

func newBackupCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Manage backups",
	}
	addOutputFlag(cmd, &out)
	cmd.AddCommand(newFilteredListCommand("list", "List backups", "/backups", backupColumns, out, true), newGetCommand("get ID", "Get backup", "/backups/", backupColumns, out), newBackupCreateCommand(out))
	return cmd
}

func newBackupCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	wait := waitOptions{}
	var serviceID, databaseDBID, bucket, backupName, storageClass string
	var integrationID int
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create backup",
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if err := requireIntFlag(integrationID, "--integration"); err != nil {
					return err
				}
				if err := requireFlag(bucket, "--bucket"); err != nil {
					return err
				}
				requestBody = bodyFromMap(map[string]interface{}{
					"integrationId": integrationID,
					"bucket":        bucket,
					"appServiceId":  optionalInt(serviceID),
					"databaseDbId":  optionalInt(databaseDBID),
					"backupName":    backupName,
					"storageClass":  storageClass,
				})
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/backups", nil, requestBody, &result); err != nil {
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
		},
	}
	addBodyFlags(cmd, &body)
	addWaitFlags(cmd, &wait)
	cmd.Flags().IntVar(&integrationID, "integration", 0, "Storage integration ID")
	cmd.Flags().StringVar(&bucket, "bucket", "", "Storage bucket")
	cmd.Flags().StringVar(&serviceID, "service", "", "App service ID")
	cmd.Flags().StringVar(&databaseDBID, "database-db", "", "Database DB ID")
	cmd.Flags().StringVar(&backupName, "name", "", "Backup name")
	cmd.Flags().StringVar(&storageClass, "storage-class", "", "Storage class")
	return cmd
}

func newImportCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Manage imports",
	}
	addOutputFlag(cmd, &out)
	cmd.AddCommand(newFilteredListCommand("list", "List imports", "/imports", importColumns, out, false), newGetCommand("get ID", "Get import", "/imports/", importColumns, out), newImportCreateCommand(out))
	return cmd
}

func newImportCreateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	wait := waitOptions{}
	var serviceID, databaseDBID, source, importURL, importName, backupID string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create import",
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if err := requireFlag(source, "--source"); err != nil {
					return err
				}
				requestBody = bodyFromMap(map[string]interface{}{
					"appServiceId": optionalInt(serviceID),
					"databaseDbId": optionalInt(databaseDBID),
					"import": bodyFromMap(map[string]interface{}{
						"source":     source,
						"url":        importURL,
						"importName": importName,
						"backupId":   optionalInt(backupID),
					}),
				})
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/imports", nil, requestBody, &result); err != nil {
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
		},
	}
	addBodyFlags(cmd, &body)
	addWaitFlags(cmd, &wait)
	cmd.Flags().StringVar(&serviceID, "service", "", "App service ID")
	cmd.Flags().StringVar(&databaseDBID, "database-db", "", "Database DB ID")
	cmd.Flags().StringVar(&source, "source", "", "Import source")
	cmd.Flags().StringVar(&importURL, "url", "", "Import archive URL")
	cmd.Flags().StringVar(&importName, "name", "", "Import name")
	cmd.Flags().StringVar(&backupID, "backup", "", "Backup ID")
	return cmd
}

func newTaskCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Manage tasks",
	}
	addOutputFlag(cmd, &out)

	var scope, orgID, projectIDs, statuses, search, appID, instanceID string
	var withoutOrigin bool
	var page, pageSize int
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			query := url.Values{}
			addQuery(query, "scope", scope)
			addQuery(query, "orgId", orgID)
			addQuery(query, "projectIds", projectIDs)
			addQuery(query, "statuses", statuses)
			addQuery(query, "search", search)
			addQuery(query, "appId", appID)
			addQuery(query, "appInstanceId", instanceID)
			if withoutOrigin {
				query.Set("withoutOrigin", "true")
			}
			if page != 0 {
				query.Set("page", strconv.Itoa(page))
			}
			if pageSize != 0 {
				query.Set("pageSize", strconv.Itoa(pageSize))
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/tasks", query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, taskColumns)
		},
	}
	listCmd.Flags().StringVar(&scope, "scope", "", "Task scope: project_and_org, org_only, or user_only")
	listCmd.Flags().StringVar(&orgID, "org", "", "Organization ID")
	listCmd.Flags().StringVar(&projectIDs, "project", "", "Project ID or comma-separated project IDs")
	listCmd.Flags().StringVar(&statuses, "statuses", "", "Comma-separated statuses")
	listCmd.Flags().StringVar(&search, "search", "", "Search query")
	listCmd.Flags().StringVar(&appID, "app", "", "App ID")
	listCmd.Flags().StringVar(&instanceID, "instance", "", "App instance ID")
	listCmd.Flags().BoolVar(&withoutOrigin, "without-origin", false, "Only tasks without origin")
	listCmd.Flags().IntVar(&page, "page", 0, "Page number")
	listCmd.Flags().IntVar(&pageSize, "page-size", 0, "Page size")

	getCmd := newTaskGetCommand(out)
	waitCmd := &cobra.Command{
		Use:   "wait ID",
		Short: "Wait for task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			timeout, _ := cmd.Flags().GetDuration("timeout")
			result, err := waitForTask(cmd.Context(), client, args[0], timeout)
			if err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, taskColumns)
		},
	}
	waitCmd.Flags().Duration("timeout", 10*time.Minute, "Maximum time to wait")

	var logJob string
	var logAllJobs bool
	logsCmd := &cobra.Command{
		Use:   "logs ID",
		Short: "Show task step logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			return printTaskLogs(cmd.Context(), cmd, client, out, args[0], logJob, logAllJobs)
		},
	}
	logsCmd.Flags().StringVar(&logJob, "job", "", "Job ID or name to show logs for")
	logsCmd.Flags().BoolVar(&logAllJobs, "all-jobs", false, "Show logs for all jobs when a task has multiple jobs")

	cancelCmd := newTaskCancelCommand(out)
	repeatCmd := newTaskRepeatCommand(out)
	cmd.AddCommand(listCmd, getCmd, waitCmd, logsCmd, cancelCmd, repeatCmd)
	return cmd
}

func newTaskGetCommand(out outputOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get ID",
		Short: "Get task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), "/tasks/"+args[0], nil, &result); err != nil {
				return err
			}
			return printClientGetResult(cmd, client, out, result, taskGetColumnsFor(result))
		},
	}
}

func taskGetColumnsFor(value interface{}) []string {
	rows := asRows(normalizeItem(value))
	if len(rows) == 0 {
		return taskGetColumns
	}

	columns := make([]string, 0, len(taskGetColumns))
	for _, column := range taskGetColumns {
		if isOptionalTaskRelationColumn(column) && !displayColumnPresent(rows[0], column) {
			continue
		}
		columns = append(columns, column)
	}
	return columns
}

func isOptionalTaskRelationColumn(column string) bool {
	switch column {
	case "app", "instance", "service", "database", "databaseDb", "jobs":
		return true
	default:
		return false
	}
}

func displayColumnPresent(row map[string]interface{}, column string) bool {
	if formatColumnValue(row, column) != "" {
		return true
	}
	relation, ok := relationColumnFor(column)
	return ok && firstRelationID(row, relation) != ""
}

func newTaskCancelCommand(out outputOptions) *cobra.Command {
	wait := waitOptions{}
	var yes bool
	cmd := &cobra.Command{
		Use:   "cancel ID",
		Short: "Cancel task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := confirm(cmd, yes, "Cancel task?"); err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/tasks/"+args[0]+"/cancel", nil, nil, &result); err != nil {
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
		},
	}
	addWaitFlags(cmd, &wait)
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm without prompting")
	return cmd
}

func newTaskRepeatCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	wait := waitOptions{}
	var force bool
	cmd := &cobra.Command{
		Use:   "repeat ID",
		Short: "Repeat task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				requestBody = map[string]interface{}{"force": force}
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/tasks/"+args[0]+"/repeat", nil, requestBody, &result); err != nil {
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
		},
	}
	addBodyFlags(cmd, &body)
	addWaitFlags(cmd, &wait)
	cmd.Flags().BoolVar(&force, "force", false, "Force repeat")
	return cmd
}

func newFilteredListCommand(use string, short string, path string, columns []string, out outputOptions, backup bool) *cobra.Command {
	var instanceID, serviceID, databaseID, databaseDBID, name string
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			query := url.Values{}
			addQuery(query, "appInstanceId", instanceID)
			addQuery(query, "appServiceId", serviceID)
			addQuery(query, "databaseId", databaseID)
			addQuery(query, "databaseDbId", databaseDBID)
			if backup {
				addQuery(query, "backupName", name)
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), path, query, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, columns)
		},
	}
	cmd.Flags().StringVar(&instanceID, "instance", "", "App instance ID")
	cmd.Flags().StringVar(&serviceID, "service", "", "App service ID")
	cmd.Flags().StringVar(&databaseID, "database", "", "Database ID")
	cmd.Flags().StringVar(&databaseDBID, "database-db", "", "Database DB ID")
	if backup {
		cmd.Flags().StringVar(&name, "name", "", "Backup name")
	}
	return cmd
}

func newDeleteCommand(use string, short string, pathPrefix string, columns []string, out outputOptions) *cobra.Command {
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
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Delete(cmd.Context(), pathPrefix+args[0], nil, &result); err != nil {
				return err
			}
			resultColumns := operationColumns
			if wait.wait && firstTaskID(result) != "" {
				result, err = waitForTask(cmd.Context(), client, firstTaskID(result), wait.timeout)
				if err != nil {
					return err
				}
				resultColumns = taskColumns
			}
			return printClientResult(cmd, client, out, result, resultColumns)
		},
	}
	addWaitFlags(cmd, &wait)
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm without prompting")
	return cmd
}

func newGetCommand(use string, short string, pathPrefix string, columns []string, out outputOptions) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrint(cmd, out, pathPrefix+args[0], columns)
		},
	}
}

func getAndPrint(cmd *cobra.Command, out outputOptions, path string, columns []string) error {
	client, err := newRESTClient()
	if err != nil {
		return err
	}
	var result interface{}
	if err := client.Get(cmd.Context(), path, nil, &result); err != nil {
		return err
	}
	return printClientGetResult(cmd, client, out, result, columns)
}

func getAndPrintWithInstances(cmd *cobra.Command, out outputOptions, path string, columns []string, filterName string, filterValue string) error {
	client, err := newRESTClient()
	if err != nil {
		return err
	}
	var result interface{}
	if err := client.Get(cmd.Context(), path, nil, &result); err != nil {
		return err
	}
	if out.output != outputJSON {
		enrichInstancesSummary(cmd.Context(), client, normalizeItem(result), filterName, filterValue)
	}
	return printClientGetResult(cmd, client, out, result, columns)
}

func enrichInstancesSummary(ctx context.Context, client *rest.Client, value interface{}, filterName string, filterValue string) {
	rows := asRows(value)
	if len(rows) == 0 || firstNonNilPath(rows[0], "instances", "appInstances") != nil {
		return
	}

	if filterValue == "" {
		filterValue = firstScalarPath(rows[0], "id")
	}
	if filterValue == "" {
		return
	}

	query := url.Values{filterName: []string{filterValue}}
	addQuery(query, "orgId", firstScalarPath(rows[0], "orgId", "org.id"))
	if filterName == "clusterId" {
		query.Set("clusterApp", "false")
	}

	var result interface{}
	if err := client.Get(ctx, "/app-instances", query, &result); err != nil {
		return
	}
	rows[0]["appInstances"] = normalizeItems(result)
}

func optionalInt(value string) interface{} {
	if value == "" {
		return nil
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return value
	}
	return number
}
