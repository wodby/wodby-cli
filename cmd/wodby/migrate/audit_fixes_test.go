package migrate

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/wodby/wodby-cli/pkg/migration/wodby1"
)

// The plan pins a snapshot for every instance with imports, while the option
// lists only the instances the operator overrode. Rerunning the same command
// must be accepted.
func TestSameSourceBackupSelectionAcceptsPartialRequest(t *testing.T) {
	pinned := wodby1.SourceBackupSelection{
		"inst-prod":    {"db": "backup-1", "files": "backup-1"},
		"inst-staging": {"db": "backup-9", "files": "backup-9"},
	}
	if !sameSourceBackupSelection(wodby1.SourceBackupSelection{"inst-staging": {"*": "backup-9"}}, pinned) {
		t.Fatal("the original partial selection must match the pinned snapshots")
	}
	if sameSourceBackupSelection(wodby1.SourceBackupSelection{"inst-staging": {"*": "backup-2"}}, pinned) {
		t.Fatal("another snapshot must not match")
	}
	if sameSourceBackupSelection(wodby1.SourceBackupSelection{"inst-dev": {"*": "backup-9"}}, pinned) {
		t.Fatal("an instance the plan did not pin must not match")
	}
}

// Cobra merges the root command's flags into the subcommand's, so the
// suggestion would otherwise repeat the Wodby 2 credentials on screen.
func TestRestartCommandSuggestionOmitsCredentials(t *testing.T) {
	root := &cobra.Command{Use: "wodby"}
	root.PersistentFlags().String("api-key", "", "")
	root.PersistentFlags().String("access-token", "", "")
	app := newWodby1AppCommand()
	root.AddCommand(app)
	if err := app.ParseFlags([]string{"--api-key", "wodby2-secret", "--access-token", "token-secret", "--source-token", "wodby1-secret", "--target-cluster", "production"}); err != nil {
		t.Fatal(err)
	}
	suggestion := restartCommandSuggestion(app, "app", "app-uuid")
	for _, secret := range []string{"wodby2-secret", "token-secret", "wodby1-secret"} {
		if strings.Contains(suggestion, secret) {
			t.Fatalf("suggestion repeats a credential: %s", suggestion)
		}
	}
	if !strings.Contains(suggestion, "--target-cluster production") || !strings.HasSuffix(suggestion, "--apply --restart") {
		t.Fatalf("suggestion = %s", suggestion)
	}
}

func TestConfirmRollbackRefusesAnEmptyName(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("\n"))
	if err := confirmRollback(cmd, false, ""); err == nil {
		t.Fatal("an empty name must not let Enter approve a rollback")
	}
}
