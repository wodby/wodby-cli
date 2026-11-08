package ops

import (
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var memberTeamColumns = []string{"id", "member", "email", "role", "status", "teamMemberships"}

// newMemberTeamsCommand replaces the teams an organization member belongs to. It is meant for the member command,
// whose --output flag it uses.
func newMemberTeamsCommand() *cobra.Command {
	out := outputOptions{output: outputTable}
	body := bodyOptions{}
	var teams []string
	var none bool
	cmd := &cobra.Command{
		Use:   "set-teams MEMBERSHIP_ID",
		Short: "Replace the teams an organization member belongs to; teams left out lose the member",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestBody, hasBody, err := readBody(body)
			if err != nil {
				return err
			}
			if !hasBody {
				if none && len(teams) > 0 {
					return errors.New("use either --team or --none, not both")
				}
				if !none && len(teams) == 0 {
					return errors.New("pass --team for each team, or --none to remove the member from every team")
				}
				items, err := memberTeamItems(teams)
				if err != nil {
					return err
				}
				requestBody = map[string]interface{}{"teams": items}
			}
			client, err := newRESTClient()
			if err != nil {
				return err
			}
			var result interface{}
			if err := client.Put(cmd.Context(), escapedPath("/org-memberships/%s/teams", args[0]), nil, requestBody, &result); err != nil {
				return err
			}
			return printClientGetResult(cmd, client, out, result, memberTeamColumns)
		},
	}
	addBodyFlags(cmd, &body)
	cmd.Flags().StringArrayVar(&teams, "team", nil, "Team as TEAM_ID=member or TEAM_ID=leader; repeat for each team")
	cmd.Flags().BoolVar(&none, "none", false, "Remove the member from every team")
	return cmd
}

// memberTeamItems parses TEAM_ID=ROLE flags into team request items.
func memberTeamItems(teams []string) ([]interface{}, error) {
	items := make([]interface{}, 0, len(teams))
	for _, team := range teams {
		id, role, ok := strings.Cut(team, "=")
		number, err := strconv.Atoi(strings.TrimSpace(id))
		role = strings.TrimSpace(role)
		if !ok || err != nil || (role != "member" && role != "leader") {
			return nil, errors.Errorf("invalid --team %q: use TEAM_ID=member or TEAM_ID=leader", team)
		}
		items = append(items, map[string]interface{}{"teamId": number, "role": role})
	}
	return items, nil
}
