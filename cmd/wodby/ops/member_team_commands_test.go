package ops

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// newTestMemberCommand wraps set-teams in a member command with an --output flag, as the CLI registers it.
func newTestMemberCommand() *cobra.Command {
	out := outputOptions{}
	cmd := &cobra.Command{Use: "member"}
	addOutputFlag(cmd, &out)
	cmd.AddCommand(newMemberTeamsCommand())
	return cmd
}

func TestMemberSetTeams(t *testing.T) {
	membership := map[string]interface{}{"id": 5, "email": "ann@example.com", "role": "member", "status": "active",
		"teamMemberships": []interface{}{map[string]interface{}{"id": 1, "teamId": 2, "role": "leader"}}}
	runRESTRouteCases(t, []restRouteCase{
		{name: "teams", command: newTestMemberCommand, args: []string{"set-teams", "5", "--team", "2=leader", "--team", "8=member"},
			method: http.MethodPut, path: "/v1/org-memberships/5/teams",
			body: map[string]interface{}{"teams": []interface{}{
				map[string]interface{}{"teamId": float64(2), "role": "leader"},
				map[string]interface{}{"teamId": float64(8), "role": "member"},
			}},
			response: membership, wantInOut: "ann@example.com"},
		{name: "no teams", command: newTestMemberCommand, args: []string{"set-teams", "5", "--none", "-o", "json"},
			method: http.MethodPut, path: "/v1/org-memberships/5/teams", body: map[string]interface{}{"teams": []interface{}{}}, response: membership},
		{name: "raw body", command: newTestMemberCommand, args: []string{"set-teams", "5", "--data", `{"teams":[{"teamId":2,"role":"member"}]}`},
			method: http.MethodPut, path: "/v1/org-memberships/5/teams",
			body:     map[string]interface{}{"teams": []interface{}{map[string]interface{}{"teamId": float64(2), "role": "member"}}},
			response: membership},
	})
}

func TestMemberSetTeamsRejectsBadInput(t *testing.T) {
	configureTestAPI(t, "http://127.0.0.1:1/v1")
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"set-teams", "5"}, want: "--none"},
		{args: []string{"set-teams", "5", "--team", "2=leader", "--none"}, want: "not both"},
		{args: []string{"set-teams", "5", "--team", "2=owner"}, want: "TEAM_ID=member"},
		{args: []string{"set-teams", "5", "--team", "design=member"}, want: "TEAM_ID=member"},
	} {
		var out bytes.Buffer
		cmd := newTestMemberCommand()
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(test.args)
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("Execute(%v) error = %v, want %q", test.args, err, test.want)
		}
	}
}
