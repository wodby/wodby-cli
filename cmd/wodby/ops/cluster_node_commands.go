package ops

import (
	"fmt"
	"strconv"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	clusterHostCheckColumns        = []string{"advisory", "node", "title", "state", "kernel", "checkedAt", "message"}
	clusterSecurityAdvisoryColumns = []string{"advisory", "title", "state", "action", "message"}
	k3sClusterNodeColumns          = []string{"name", "role", "ready", "schedulable", "version", "internalIp", "externalIp", "localVolumes", "createdAt"}
	k3sPrivateAddressColumns       = []string{"address", "networkInterface", "mtu", "persistent", "persistentSource"}
	// clusterNodeColumns leaves the zone, the external address and the disk to vertical and JSON output, so the
	// table fits a terminal.
	clusterNodeColumns         = []string{"name", "ready", "schedulable", "version", "machineType", "gpuNodePool", "internalIp", "cpu", "memory", "gpus", "createdAt"}
	clusterNodeVerticalColumns = []string{"name", "ready", "schedulable", "version", "machineType", "zone", "gpuNodePool", "internalIp", "externalIp", "cpu", "memory", "disk", "gpus", "createdAt"}
)

// newClusterNodesCommand lists the nodes of a cluster of any kind with what each node's machine provides.
func newClusterNodesCommand(out outputOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "nodes CLUSTER_ID",
		Aliases: []string{"node"},
		Short:   "List the nodes of a cluster, read live from the cluster",
		Long: "List the nodes of a cluster, read live from the cluster: the general nodes first, then the GPU node " +
			"pool. Each node shows the CPU cores, memory and GPUs its machine provides; -o vertical adds the zone, the " +
			"external address and the root disk. Wodby Cloud and serverless clusters don't list nodes. For the role " +
			"and the persistent volumes of a k3s cluster's nodes, use k3s-node list.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Get(cmd.Context(), escapedPath("/cluster-nodes/%s", args[0]), nil, &result); err != nil {
				return err
			}
			switch outputFormat(cmd, out) {
			case outputJSON:
				return printJSON(cmd, result)
			case outputVertical:
				return printResult(cmd, out, clusterNodeDisplayRows(result), clusterNodeVerticalColumns)
			default:
				return printResult(cmd, out, clusterNodeDisplayRows(result), clusterNodeColumns)
			}
		},
	}
}

// clusterNodeDisplayRows copies the nodes with each node's capacity as cpu, memory, disk and gpus columns, the
// memory and the disk in binary units instead of bytes.
func clusterNodeDisplayRows(value interface{}) []map[string]interface{} {
	nodes := asRows(normalizeItems(value))
	rows := make([]map[string]interface{}, 0, len(nodes))
	for _, node := range nodes {
		row := make(map[string]interface{}, len(node)+4)
		for key, field := range node {
			row[key] = field
		}
		row["cpu"] = valueAtPath(node, "capacity.cpu")
		row["gpus"] = valueAtPath(node, "capacity.gpus")
		for _, key := range []string{"memory", "disk"} {
			if size, ok := numericPathValue(node, "capacity."+key); ok {
				row[key] = formatByteSize(size)
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// formatByteSize shows a size in bytes in the largest binary unit that keeps it at 1 or more, with at most one
// decimal.
func formatByteSize(size float64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	unit := 0
	for size >= 1024 && unit < len(units)-1 {
		size /= 1024
		unit++
	}
	return strconv.FormatFloat(float64(int64(size*10+0.5))/10, 'f', -1, 64) + " " + units[unit]
}

// newK3SPrivateAddressesCommand lists the private addresses a k3s server could use for cluster traffic instead of
// its public one.
func newK3SPrivateAddressesCommand(out outputOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "k3s-private-addresses CLUSTER_ID",
		Short: "List the private addresses a k3s server could switch to",
		Long: "List the private addresses the server of a k3s cluster could switch to for cluster traffic, read from " +
			"the server itself. Only for a server that uses its public address for cluster traffic, which keeps worker " +
			"nodes out. An address that isn't persistent is not known to come back after a reboot; switching to it " +
			"needs --force. Needs permission to modify the cluster.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrintList(cmd, out, escapedPath("/cluster-k3s-private-addresses/%s", args[0]), nil, k3sPrivateAddressColumns)
		},
	}
}

// newK3SSwitchServerAddressCommand starts a task that moves a k3s server's cluster traffic to a private address.
func newK3SSwitchServerAddressCommand(out outputOptions) *cobra.Command {
	cmd := newK3SOperationCommand("switch-k3s-server-address CLUSTER_ID", "Switch a k3s server to a private address for cluster traffic",
		"/clusters/%s/actions/switch-k3s-server-address", out, []jsonFlagSpec{
			stringJSONFlag("address", "address", "Private address listed by k3s-private-addresses", true),
			boolJSONFlag("force", "force", "Switch to an address the server's network configuration isn't known to keep after a reboot", false),
		})
	cmd.Long = "Switch the server of a k3s cluster from its public address to a private one for cluster traffic, so " +
		"worker nodes can join. The public address keeps serving public traffic. k3s restarts once: the Kubernetes " +
		"API pauses for about a minute while apps keep running, and new deployments wait until the switch ends. " +
		"Refused while application operations are active. If k3s doesn't come back on the private address, the " +
		"server restores its previous settings. k3s doesn't start when its address is gone, so an address that " +
		"isn't persistent needs --force."
	return cmd
}

// newK3SUpdateMaxPodsCommand starts a task that sets how many pods each node of a k3s cluster runs at most.
func newK3SUpdateMaxPodsCommand(out outputOptions) *cobra.Command {
	cmd := newK3SOperationCommand("update-k3s-max-pods CLUSTER_ID", "Set how many pods each node of a k3s cluster runs at most",
		"/clusters/%s/actions/update-k3s-max-pods", out, []jsonFlagSpec{
			intJSONFlag("max-pods", "maxPods", "Pods each node runs at most, from 50 to 250", true),
		})
	cmd.Long = "Set how many pods each node of a k3s cluster runs at most. k3s restarts on every node whose setting " +
		"changes, one node at a time: apps keep running, the Kubernetes API pauses for about a minute while the " +
		"server restarts, and new deployments wait until it ends. Refused while application operations are active, " +
		"below the pods a node already runs, on a node with no free pod slot, and before the cluster's " +
		"infrastructure upgrade that sets it."
	return cmd
}

// newK3SOperationCommand posts a k3s cluster change built from flags or a JSON body and prints the task it starts,
// or the finished task with --wait.
func newK3SOperationCommand(use string, short string, pathPattern string, out outputOptions, specs []jsonFlagSpec) *cobra.Command {
	body := bodyOptions{}
	wait := waitOptions{}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, err := readBodyOrJSONFlags(cmd, body, specs, false)
			if err != nil {
				return err
			}
			return postAndPrintOperation(cmd, out, wait, escapedPath(pathPattern, args[0]), requestBody)
		},
	}
	addBodyFlags(cmd, &body)
	addJSONFlagSpecs(cmd, specs)
	addWaitFlags(cmd, &wait)
	return cmd
}

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
