package ops

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/wodby/wodby-cli/pkg/api/rest"
)

var (
	clusterComponentColumns       = []string{"name", "version", "status", "latestVersion", "updatePath", "manager", "source", "namespace", "workload", "count", "observedAt"}
	clusterComponentUpdateColumns = []string{"name", "kind", "currentVersion", "targetVersion", "components", "note"}
)

// newClusterComponentCommand lists the infrastructure software observed on a cluster, the single-component updates
// Wodby can apply, and applies one of them.
func newClusterComponentCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "component",
		Aliases: []string{"components"},
		Short:   "List and update cluster infrastructure components",
	}
	list := &cobra.Command{
		Use:   "list CLUSTER_ID",
		Short: "List the infrastructure software last observed running on a cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrintList(cmd, out, escapedPath("/cluster-components/%s", args[0]), nil, clusterComponentColumns)
		},
	}
	updates := &cobra.Command{
		Use:   "updates CLUSTER_ID",
		Short: "List the component updates Wodby can apply without a Kubernetes or infrastructure upgrade",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getAndPrintList(cmd, out, escapedPath("/cluster-component-updates/%s", args[0]), nil, clusterComponentUpdateColumns)
		},
	}
	defaultToList(cmd, list)
	cmd.AddCommand(list, updates, newClusterComponentUpdateCommand(out))
	return cmd
}

// newClusterComponentUpdateCommand starts a task that applies one of the component updates listed for a cluster.
func newClusterComponentUpdateCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	wait := waitOptions{}
	cmd := &cobra.Command{
		Use:   "update CLUSTER_ID NAME",
		Short: "Apply a component update listed by updates",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if hasBody && len(args) == 2 {
				return errors.New("pass the update NAME or --data/--file, not both")
			}
			if !hasBody {
				if len(args) != 2 {
					return errors.New("update NAME is required")
				}
				requestBody = map[string]interface{}{"name": args[1]}
			}
			return postAndPrintOperation(cmd, out, wait, escapedPath("/clusters/%s/actions/update-component", args[0]), requestBody)
		},
	}
	addBodyFlags(cmd, &body)
	addWaitFlags(cmd, &wait)
	return cmd
}

// newClusterGPUNodePoolCommand adds, resizes, replaces or removes a cluster's optional GPU node pool. Values not given
// as flags keep those of the current pool.
func newClusterGPUNodePoolCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	wait := waitOptions{}
	var remove, yes bool
	specs := []jsonFlagSpec{
		stringJSONFlag("machine-type", "machineType", "Machine type that reports GPUs", false),
		intJSONFlag("min", "minNodeCount", "Minimum node count; 0 lets the pool scale to no nodes where the provider supports it", false),
		intJSONFlag("max", "maxNodeCount", "Maximum node count", false),
		intJSONFlag("disk-size", "nodeDiskSize", "Node disk size in GB, at least 20; required when the provider's node disk size is configurable", false),
	}
	cmd := &cobra.Command{
		Use:   "gpu-node-pool CLUSTER_ID",
		Short: "Add, resize, replace or remove the cluster's GPU node pool",
		Long: "Add, resize, replace or remove the cluster's optional GPU node pool. Pods that request GPUs run on this " +
			"pool. Values not given as flags keep those of the current pool. Changing the machine type creates the new " +
			"pool before the old one is deleted. --remove deletes the pool; it is refused while pods request GPUs.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if remove && (hasBody || hasChangedFlags(cmd.Flags(), "machine-type", "min", "max", "disk-size")) {
				return errors.New("--remove cannot be combined with pool values or --data/--file")
			}
			switch {
			case remove:
				if err := confirm(cmd, yes, "Remove the cluster's GPU node pool?"); err != nil {
					return err
				}
				requestBody = map[string]interface{}{"gpuNodePool": nil}
			case !hasBody:
				if !hasChangedFlags(cmd.Flags(), "machine-type", "min", "max", "disk-size") {
					return errors.New("pass pool values, --remove or --data/--file")
				}
				values, err := bodyFromJSONFlagSpecs(cmd, specs, false)
				if err != nil {
					return err
				}
				client, err := newRESTClient()
				if err != nil {
					return err
				}
				if err := fillClusterGPUNodePoolDefaults(cmd, client, args[0], values); err != nil {
					return err
				}
				requestBody = map[string]interface{}{"gpuNodePool": values}
			}
			return postAndPrintOperation(cmd, out, wait, escapedPath("/clusters/%s/actions/gpu-node-pool", args[0]), requestBody)
		},
	}
	addBodyFlags(cmd, &body)
	addJSONFlagSpecs(cmd, specs)
	addWaitFlags(cmd, &wait)
	cmd.Flags().BoolVar(&remove, "remove", false, "Remove the GPU node pool")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm --remove without prompting")
	return cmd
}

// fillClusterGPUNodePoolDefaults copies the cluster's current GPU node pool values into values for flags left out,
// and reports the flags a new pool still needs.
func fillClusterGPUNodePoolDefaults(cmd *cobra.Command, client *rest.Client, clusterID string, values map[string]interface{}) error {
	var cluster map[string]interface{}
	if err := client.Get(cmd.Context(), escapedPath("/clusters/%s", clusterID), nil, &cluster); err != nil {
		return err
	}
	current, _ := cluster["gpuNodePool"].(map[string]interface{})
	for _, key := range []string{"machineType", "minNodeCount", "maxNodeCount", "nodeDiskSize"} {
		if _, given := values[key]; !given && current[key] != nil {
			values[key] = current[key]
		}
	}
	for _, required := range []struct{ key, flag string }{
		{"machineType", "--machine-type"},
		{"minNodeCount", "--min"},
		{"maxNodeCount", "--max"},
	} {
		if _, ok := values[required.key]; !ok {
			return errors.Errorf("%s is required because the cluster has no GPU node pool yet", required.flag)
		}
	}
	return nil
}
