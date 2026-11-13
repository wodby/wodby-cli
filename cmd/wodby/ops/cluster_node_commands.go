package ops

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	clusterHostCheckColumns        = []string{"advisory", "node", "title", "state", "kernel", "checkedAt", "message"}
	clusterSecurityAdvisoryColumns = []string{"advisory", "title", "state", "action", "message"}
	k3sClusterNodeColumns          = []string{"name", "role", "ready", "schedulable", "version", "internalIp", "externalIp", "localVolumes", "createdAt"}
)

// newClusterHostChecksCommand lists what a k3s server last reported about the protections Wodby keeps on it against
// tracked kernel advisories.
func newClusterHostChecksCommand(out outputOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "host-checks CLUSTER_ID",
		Short: "List the security protections a k3s server last reported",
		Long: "List what a k3s cluster's server last reported about the protections Wodby keeps on it against tracked " +
			"security advisories. The server checks after each boot and once a day and puts back a missing protection. " +
			"A check is unknown when the server hasn't reported it or its last report is more than two days old. " +
			"Managed clusters return no checks.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrintList(cmd, out, escapedPath("/cluster-host-checks/%s", args[0]), nil, clusterHostCheckColumns)
		},
	}
}

// newClusterSecurityAdvisoriesCommand lists where a cluster stands on each security advisory Wodby tracks and the
// operation that fixes or mitigates it.
func newClusterSecurityAdvisoriesCommand(out outputOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "security-advisories CLUSTER_ID",
		Aliases: []string{"advisories"},
		Short:   "List where the cluster stands on each tracked security advisory",
		Long: "List where the cluster stands on each security advisory Wodby tracks. A k3s cluster is mitigated or not " +
			"by the protections Wodby keeps on its server; a managed cluster is fixed or not by the node images its " +
			"provider ships. The action column names the operation that fixes or mitigates the advisory: " +
			"upgrade_infrastructure (cluster upgrade-infra), recycle_nodes (cluster recycle-nodes) or update_kubernetes " +
			"(cluster update-kubernetes-version). Wodby Cloud and serverless clusters return no advisories.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrintList(cmd, out, escapedPath("/cluster-security-advisories/%s", args[0]), nil, clusterSecurityAdvisoryColumns)
		},
	}
}

// newK3SNodeCommand lists the nodes of a k3s cluster, prints the command that joins another server as a worker node,
// and removes a worker node.
func newK3SNodeCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "k3s-node",
		Aliases: []string{"k3s-nodes"},
		Short:   "List, join and remove the nodes of a k3s cluster",
	}
	list := &cobra.Command{
		Use:   "list CLUSTER_ID",
		Short: "List the nodes of a k3s cluster, read live from the cluster, the server first",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrintList(cmd, out, escapedPath("/cluster-k3s-nodes/%s", args[0]), nil, k3sClusterNodeColumns)
		},
	}
	defaultToList(cmd, list)
	cmd.AddCommand(list, newK3SJoinCommand(out), newK3SNodeRemoveCommand(out))
	return cmd
}

// newK3SJoinCommand prints the one-time command that joins another server to a k3s cluster as a worker node.
func newK3SJoinCommand(out outputOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "join-command CLUSTER_ID",
		Short: "Print the command that joins another server as a worker node; it contains short-lived credentials",
		Long: "Print a one-time shell command that joins another server to a k3s cluster as a worker node. Run it as " +
			"root on that server within 15 minutes. Both servers must share a private network, and the cluster must be " +
			"in OK status on current infrastructure. The command contains short-lived credentials; treat it as a secret.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), escapedPath("/clusters/k3s-join-command/%s", args[0]), nil, &result); err != nil {
				return err
			}
			if outputFormat(cmd, out) == outputJSON {
				return printJSON(cmd, result)
			}
			fmt.Fprintln(cmd.OutOrStdout(), formatValue(objectItem(result)["command"]))
			return nil
		},
	}
}

// newK3SNodeRemoveCommand starts a task that drains a k3s worker node and deletes it from the cluster.
func newK3SNodeRemoveCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	wait := waitOptions{}
	var force, yes bool
	cmd := &cobra.Command{
		Use:   "remove CLUSTER_ID NODE_NAME",
		Short: "Remove a worker node from a k3s cluster",
		Long: "Remove a worker node from a k3s cluster: its pods move to other nodes, k3s stops on it and the node is " +
			"deleted. The server can't be removed. A node that stores persistent volumes is refused unless --force is " +
			"given, which loses their data; k3s-node list -o vertical lists them. Afterwards run " +
			"sudo /usr/local/bin/wodby-k3s-uninstall on that server to remove k3s from it.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if hasBody && (len(args) == 2 || force) {
				return errors.New("pass NODE_NAME and --force or --data/--file, not both")
			}
			if !hasBody {
				if len(args) != 2 {
					return errors.New("NODE_NAME is required")
				}
				prompt := fmt.Sprintf("Remove node %s from the cluster?", args[1])
				if force {
					prompt = fmt.Sprintf("Remove node %s from the cluster and lose the data of the persistent volumes it stores?", args[1])
				}
				if err := confirm(cmd, yes, prompt); err != nil {
					return err
				}
				values := map[string]interface{}{"nodeName": args[1]}
				if force {
					values["force"] = true
				}
				requestBody = values
			}
			return postAndPrintOperation(cmd, out, wait, escapedPath("/clusters/%s/actions/remove-k3s-node", args[0]), requestBody)
		},
	}
	addBodyFlags(cmd, &body)
	addWaitFlags(cmd, &wait)
	cmd.Flags().BoolVar(&force, "force", false, "Remove the node even though it stores persistent volumes, losing their data")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm without prompting")
	return cmd
}
