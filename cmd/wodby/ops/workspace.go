package ops

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newWorkspaceCommand uses the public control plane; it never writes SSH config
// or moves an existing local coding-agent session into a remote workspace.
func newWorkspaceCommand(out outputOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "workspace", Short: "Inspect and operate development workspaces"}
	cmd.AddCommand(newGetCommand("get ENVIRONMENT_ID", "Get workspace environment and preparation status", "/app-environments/", append(append([]string{}, instanceGetColumns...), "executionMode", "workspace"), out))
	connection := &cobra.Command{Use: "connection ENVIRONMENT_ID", Short: "Get SSH connection details and host fingerprint", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newRESTClient()
		if err != nil {
			return err
		}
		var result interface{}
		if err = client.Get(cmd.Context(), escapedPath("/workspaces/%s/connection", args[0]), nil, &result); err != nil {
			return err
		}
		return printClientResult(cmd, client, out, result, []string{"ready", "reason", "host", "port", "username", "workingDirectory", "hostKeyFingerprint"})
	}}
	connection.Long = "Return SSH connection details without changing local files. Add your public key to your Wodby account, verify the host fingerprint, and connect with ssh -p PORT USER@HOST. Use the returned workingDirectory for your remote agent."
	cmd.AddCommand(connection, newRawBodyPostCommand("eligibility", "Check workspace support (JSON body: stackRevId, disabledServiceIds, optional clusterId)", "/workspace-eligibility", []string{"eligible", "reasons", "sourceStackServiceId", "consumerStackServiceIds"}, out))
	for _, operation := range []string{"prepare", "restart", "pause", "resume"} {
		cmd.AddCommand(newClusterActionCommand(operation+" ENVIRONMENT_ID", fmt.Sprintf("%s workspace (restart affects SSH sessions)", operation), "/workspaces/%s/actions/"+operation, out))
	}
	return cmd
}
