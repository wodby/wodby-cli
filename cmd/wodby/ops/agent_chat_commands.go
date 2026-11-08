package ops

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/wodby/wodby-cli/pkg/api/rest"
)

var (
	agentChatColumns       = []string{"id", "title", "status", "approvalMode", "contextTokens", "updatedAt"}
	agentChatDetailColumns = []string{"id", "title", "status", "error", "approvalMode", "contextTokens", "contextLimit", "createdAt", "updatedAt"}
)

// agentChatPollInterval is how often --wait reads a chat while the agent works.
var agentChatPollInterval = 2 * time.Second

// agentChatToolContentLimit is how many characters of a tool result the table output shows; -o json shows all.
const agentChatToolContentLimit = 400

// agentChatWriteSpec describes a command that writes to an existing chat at /agent-chats/{id}{pathSuffix}.
type agentChatWriteSpec struct {
	use        string
	short      string
	method     string
	pathSuffix string
	args       cobra.PositionalArgs
	waitable   bool
	// flags adds the command's own flags.
	flags func(cmd *cobra.Command)
	// body builds the request body from flags and arguments; nil sends no body and adds no --data/--file flags.
	body func(cmd *cobra.Command, args []string) (interface{}, error)
}

// newAgentChatCommand manages the current user's chats with Wodby Agent in an organization.
func newAgentChatCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{
		Use:     "agent-chat",
		Aliases: []string{"agent-chats"},
		Short:   "Chat with Wodby Agent",
		Long: "Chat with Wodby Agent in an organization. A message starts a run that continues after the command returns; " +
			"use --wait, or read the chat with get until its status is idle, or waiting for an approval or an answer. " +
			"Each run uses the organization's AI credits.",
	}
	addOutputFlag(cmd, &out)
	list := newOrgListCommand("list", "List your agent chats, the most recently active first", "/agent-chats", agentChatColumns, out)
	defaultToList(cmd, list)
	cmd.AddCommand(
		list,
		newAgentChatStartCommand(out),
		newAgentChatGetCommand(out),
		newAgentChatWriteCommand(agentChatWriteSpec{
			use:        "send ID MESSAGE",
			short:      "Send a message to an idle chat and start the agent's run; MESSAGE - reads standard input",
			method:     http.MethodPost,
			pathSuffix: "/messages",
			args:       cobra.RangeArgs(1, 2),
			waitable:   true,
			body: func(cmd *cobra.Command, args []string) (interface{}, error) {
				message, err := readAgentChatMessage(cmd, args, 1)
				if err != nil {
					return nil, err
				}
				return map[string]interface{}{"message": message}, nil
			},
		}, out),
		newAgentChatWriteCommand(agentChatWriteSpec{
			use:        "approve ID",
			short:      "Approve or decline a change that waits for approval",
			method:     http.MethodPost,
			pathSuffix: "/approvals",
			args:       cobra.ExactArgs(1),
			waitable:   true,
			flags: func(cmd *cobra.Command) {
				cmd.Flags().Int("message-id", 0, "ID of the tool message that waits for approval, as get shows it")
				cmd.Flags().Bool("deny", false, "Decline the change instead of approving it")
			},
			body: func(cmd *cobra.Command, args []string) (interface{}, error) {
				messageID, _ := cmd.Flags().GetInt("message-id")
				if err := requireIntFlag(messageID, "--message-id"); err != nil {
					return nil, err
				}
				deny, _ := cmd.Flags().GetBool("deny")
				return map[string]interface{}{"messageId": messageID, "approve": !deny}, nil
			},
		}, out),
		newAgentChatWriteCommand(agentChatWriteSpec{
			use:        "answer ID",
			short:      "Answer a question the agent asked",
			method:     http.MethodPost,
			pathSuffix: "/answers",
			args:       cobra.ExactArgs(1),
			waitable:   true,
			flags: func(cmd *cobra.Command) {
				cmd.Flags().Int("message-id", 0, "ID of the ask_user message that waits for an answer, as get shows it")
				cmd.Flags().StringArray("answer", nil, "An option label from the question, or other text; repeat to choose several")
			},
			body: func(cmd *cobra.Command, args []string) (interface{}, error) {
				messageID, _ := cmd.Flags().GetInt("message-id")
				if err := requireIntFlag(messageID, "--message-id"); err != nil {
					return nil, err
				}
				answers, _ := cmd.Flags().GetStringArray("answer")
				if len(answers) == 0 {
					return nil, errors.New("--answer is required")
				}
				return map[string]interface{}{"messageId": messageID, "answers": answers}, nil
			},
		}, out),
		newAgentChatWriteCommand(agentChatWriteSpec{
			use:        "approval-mode ID MODE",
			short:      "Set whether the chat asks before a change: ask, or never_ask to run every change at once",
			method:     http.MethodPut,
			pathSuffix: "/approval-mode",
			args:       cobra.RangeArgs(1, 2),
			body: func(cmd *cobra.Command, args []string) (interface{}, error) {
				if len(args) < 2 {
					return nil, errors.New("MODE is required: ask or never_ask")
				}
				mode, err := agentChatApprovalMode(args[1], "MODE")
				if err != nil {
					return nil, err
				}
				return map[string]interface{}{"approvalMode": mode}, nil
			},
		}, out),
		newAgentChatWriteCommand(agentChatWriteSpec{
			use:        "stop ID",
			short:      "Stop the chat's run and decline the changes that wait for approval",
			method:     http.MethodPost,
			pathSuffix: "/stop",
			args:       cobra.ExactArgs(1),
		}, out),
		newAgentChatDeleteCommand(out),
	)
	return cmd
}

// newAgentChatStartCommand starts a chat with its first message.
func newAgentChatStartCommand(out outputOptions) *cobra.Command {
	body := bodyOptions{}
	wait := waitOptions{}
	var orgID, approvalMode string
	cmd := &cobra.Command{
		Use:   "start MESSAGE",
		Short: "Start a chat with Wodby Agent; MESSAGE - reads standard input",
		Args:  cobra.RangeArgs(0, 1),
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
				message, err := readAgentChatMessage(cmd, args, 0)
				if err != nil {
					return err
				}
				values := map[string]interface{}{"message": message}
				if cmd.Flags().Changed("approval-mode") {
					mode, err := agentChatApprovalMode(approvalMode, "--approval-mode")
					if err != nil {
						return err
					}
					values["approvalMode"] = mode
				}
				resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
				if err != nil {
					return err
				}
				if err := addOptionalInt(values, "orgId", resolvedOrgID, "--org"); err != nil {
					return err
				}
				requestBody = values
			}
			var result interface{}
			if err := client.Post(cmd.Context(), "/agent-chats", nil, requestBody, &result); err != nil {
				return err
			}
			chat := objectItem(result)
			return printAgentChatWriteResult(cmd, client, out, wait, formatValue(chat["orgId"]), formatValue(chat["id"]), result)
		},
	}
	addBodyFlags(cmd, &body)
	addAgentChatWaitFlags(cmd, &wait)
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	cmd.Flags().StringVar(&approvalMode, "approval-mode", "", "ask (the default) waits for approval of every change; never_ask runs every change at once, including destructive ones")
	return cmd
}

// newAgentChatGetCommand shows a chat with its messages.
func newAgentChatGetCommand(out outputOptions) *cobra.Command {
	var orgID string
	cmd := &cobra.Command{
		Use:   "get ID",
		Short: "Show an agent chat with its messages",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
			if err != nil {
				return err
			}
			var detail map[string]interface{}
			if err := client.Get(cmd.Context(), escapedPath("/agent-chats/%s", args[0]), url.Values{"orgId": []string{resolvedOrgID}}, &detail); err != nil {
				return err
			}
			return printAgentChatDetail(cmd, out, detail)
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	return cmd
}

// newAgentChatWriteCommand builds a command that writes to an existing chat, from flags or --data/--file, and prints
// the chat, or with --wait the chat and its messages once the agent stops working.
func newAgentChatWriteCommand(spec agentChatWriteSpec, out outputOptions) *cobra.Command {
	body := bodyOptions{}
	wait := waitOptions{}
	var orgID string
	cmd := &cobra.Command{
		Use:   spec.use,
		Short: spec.short,
		Args:  spec.args,
		RunE: func(cmd *cobra.Command, args []string) error {
			var requestBody interface{}
			if spec.body != nil {
				raw, hasBody, err := readBody(body)
				if err != nil {
					return err
				}
				requestBody = raw
				if !hasBody {
					if requestBody, err = spec.body(cmd, args); err != nil {
						return err
					}
				}
			}
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
			if err := client.Do(cmd.Context(), spec.method, escapedPath("/agent-chats/%s", args[0])+spec.pathSuffix, query, requestBody, &result); err != nil {
				return err
			}
			return printAgentChatWriteResult(cmd, client, out, wait, resolvedOrgID, args[0], result)
		},
	}
	if spec.body != nil {
		addBodyFlags(cmd, &body)
	}
	if spec.waitable {
		addAgentChatWaitFlags(cmd, &wait)
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	if spec.flags != nil {
		spec.flags(cmd)
	}
	return cmd
}

// newAgentChatDeleteCommand deletes a chat and its messages after confirmation.
func newAgentChatDeleteCommand(out outputOptions) *cobra.Command {
	var orgID string
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete ID",
		Short: "Delete an agent chat and its messages; what the agent did stays",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := confirm(cmd, yes, "Delete agent chat?"); err != nil {
				return err
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			resolvedOrgID, err := inferOrgID(cmd.Context(), client, orgID)
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Delete(cmd.Context(), escapedPath("/agent-chats/%s", args[0]), url.Values{"orgId": []string{resolvedOrgID}}, &result); err != nil {
				return err
			}
			return printClientResult(cmd, client, out, result, operationColumns)
		},
	}
	cmd.Flags().StringVar(&orgID, "org", "", "Organization ID; inferred when current credentials expose one org")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm without prompting")
	return cmd
}

// addAgentChatWaitFlags adds --wait and --timeout for commands that start or resume the agent's run.
func addAgentChatWaitFlags(cmd *cobra.Command, opts *waitOptions) {
	cmd.Flags().BoolVar(&opts.wait, "wait", false, "Wait until the agent stops working, then show the chat with its messages")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", 10*time.Minute, "Maximum time to wait")
}

// readAgentChatMessage returns the message in args[index], read from standard input when it is "-".
func readAgentChatMessage(cmd *cobra.Command, args []string, index int) (string, error) {
	if len(args) <= index {
		return "", errors.New("MESSAGE is required")
	}
	message := args[index]
	if message == "-" {
		content, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", errors.WithStack(err)
		}
		message = strings.TrimSpace(string(content))
	}
	if strings.TrimSpace(message) == "" {
		return "", errors.New("MESSAGE is empty")
	}
	return message, nil
}

// agentChatApprovalMode checks an approval mode and returns it as the API names it.
func agentChatApprovalMode(value string, name string) (string, error) {
	mode := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "-", "_")
	if mode != "ask" && mode != "never_ask" {
		return "", errors.Errorf("invalid %s %q: use ask or never_ask", name, value)
	}
	return mode, nil
}

// printAgentChatWriteResult prints the chat a write returned, or with --wait the chat and its messages once the
// agent stops working. A run that stopped with an error makes the command fail after printing.
func printAgentChatWriteResult(cmd *cobra.Command, client *rest.Client, out outputOptions, wait waitOptions, orgID string, chatID string, result interface{}) error {
	if !wait.wait {
		return printClientResult(cmd, client, out, result, agentChatColumns)
	}
	if chatID == "" {
		return errors.New("the API returned no chat ID to wait for")
	}
	detail, err := waitForAgentChat(cmd.Context(), client, orgID, chatID, wait.timeout)
	if err != nil {
		return err
	}
	if err := printAgentChatDetail(cmd, out, detail); err != nil {
		return err
	}
	if message := formatValue(valueAtPath(detail, "chat.error")); message != "" {
		return errors.Errorf("the agent stopped: %s", message)
	}
	return nil
}

// waitForAgentChat reads the chat until its status is idle, or waiting for an approval or an answer, and returns it
// with its messages.
func waitForAgentChat(ctx context.Context, client *rest.Client, orgID string, chatID string, timeout time.Duration) (map[string]interface{}, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	query := url.Values{}
	addQuery(query, "orgId", orgID)
	for {
		var detail map[string]interface{}
		if err := client.Get(ctx, escapedPath("/agent-chats/%s", chatID), query, &detail); err != nil {
			if ctx.Err() != nil {
				return nil, errors.Errorf("timed out waiting for agent chat %s", chatID)
			}
			return nil, err
		}
		status := formatValue(valueAtPath(detail, "chat.status"))
		switch status {
		case "idle", "waiting":
			return detail, nil
		case "":
			return detail, errors.New("agent chat response did not include a status")
		}
		select {
		case <-ctx.Done():
			return detail, errors.Errorf("timed out waiting for agent chat %s", chatID)
		case <-time.After(agentChatPollInterval):
		}
	}
}

// printAgentChatDetail prints a chat and its messages: JSON as the API returned it, otherwise the chat's fields and a
// transcript of its messages.
func printAgentChatDetail(cmd *cobra.Command, out outputOptions, detail map[string]interface{}) error {
	output := outputFormat(cmd, out)
	switch output {
	case outputJSON:
		return printJSON(cmd, detail)
	case outputTable, outputVertical:
		chat, _ := detail["chat"].(map[string]interface{})
		printVerticalTable(cmd, chat, agentChatDetailColumns, false)
		chatCommand := cmd.CommandPath()
		if cmd.HasParent() {
			chatCommand = cmd.Parent().CommandPath()
		}
		printAgentChatMessages(cmd, chatCommand, formatValue(chat["id"]), asRows(detail["messages"]))
		return nil
	default:
		return errors.Errorf("unsupported output format %q", output)
	}
}

// printAgentChatMessages prints messages as a transcript and says which chatCommand subcommand answers the calls that
// wait.
func printAgentChatMessages(cmd *cobra.Command, chatCommand string, chatID string, messages []map[string]interface{}) {
	w := cmd.OutOrStdout()
	if len(messages) == 0 {
		return
	}
	fmt.Fprintln(w)
	var pendingApprovals, pendingQuestions []string
	for _, message := range messages {
		id := formatValue(message["id"])
		role := formatValue(message["role"])
		tool := formatValue(message["tool"])
		approval := formatValue(message["approval"])

		header := []string{fmt.Sprintf("[%s] %s", id, role)}
		if tool != "" {
			header[0] += " " + tool
		}
		if approval == "pending" {
			if tool == "ask_user" {
				header = append(header, "waits for an answer")
				pendingQuestions = append(pendingQuestions, id)
			} else {
				header = append(header, "waits for approval")
				pendingApprovals = append(pendingApprovals, id)
			}
		} else if approval != "" {
			header = append(header, approval)
		}
		if truthyPath(message, "toolDestructive") {
			header = append(header, "destructive")
		}
		if truthyPath(message, "isError") {
			header = append(header, "error")
		}
		if created := formatDisplayTime(message["createdAt"]); created != "" {
			header = append(header, created)
		}
		fmt.Fprintln(w, strings.Join(header, " · "))

		if role == "tool" {
			for _, target := range asRows(message["toolTargets"]) {
				fmt.Fprintf(w, "  Target: %s %q (%s)\n", formatValue(target["kind"]), formatValue(target["name"]), formatValue(target["argument"]))
			}
			if arguments := message["toolArguments"]; arguments != nil {
				fmt.Fprintf(w, "  Arguments: %s\n", formatValue(arguments))
			}
		}
		content := strings.TrimSpace(formatValue(message["content"]))
		if role == "tool" {
			content = truncateAgentChatContent(content, agentChatToolContentLimit)
		}
		for _, line := range strings.Split(content, "\n") {
			if line != "" {
				fmt.Fprintln(w, "  "+line)
			}
		}
	}
	for _, id := range pendingApprovals {
		fmt.Fprintf(w, "\nMessage %s waits for approval: %s approve %s --message-id %s, with --deny to decline.\n", id, chatCommand, chatID, id)
	}
	for _, id := range pendingQuestions {
		fmt.Fprintf(w, "\nMessage %s waits for an answer: %s answer %s --message-id %s --answer TEXT.\n", id, chatCommand, chatID, id)
	}
}

// truncateAgentChatContent shortens content to limit characters and says how much was left out.
func truncateAgentChatContent(content string, limit int) string {
	runes := []rune(content)
	if len(runes) <= limit {
		return content
	}
	return string(runes[:limit]) + fmt.Sprintf("… (%d more characters; use -o json to see all)", len(runes)-limit)
}
