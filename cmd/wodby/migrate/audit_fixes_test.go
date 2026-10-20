package migrate

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

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
