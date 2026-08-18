package wodby1

import (
	"strings"
	"testing"

	"github.com/pkg/errors"
)

func TestExternalActionNextStepsExplainThePipelineWork(t *testing.T) {
	blocked := &ExternalActionRequiredError{
		Instance:         "prod",
		TargetInstanceID: 4100,
		ServiceName:      "php",
		TargetServiceID:  4200,
		ProviderLabel:    "GitHub Actions",
		ExampleURL:       "https://github.com/wodby/wodby-ci/blob/2.0/drupal/github-actions/wodby.yml",
	}
	steps := blocked.NextSteps()
	for _, want := range []string{
		"prod",
		"GitHub Actions pipeline to Wodby CLI 2.x",
		"https://github.com/wodby/wodby-ci/blob/2.0/drupal/github-actions/wodby.yml",
		"All examples: https://github.com/wodby/wodby-ci/tree/2.0",
		"WODBY_API_KEY",
		"WODBY_APP_SERVICE_ID  4200",
		"wodby ci deploy",
		"Rerun the same --apply command",
		"4100",
	} {
		if !strings.Contains(steps, want) {
			t.Fatalf("next steps missing %q:\n%s", want, steps)
		}
	}
	// The old message told operators to inspect a target that is fine.
	if strings.Contains(strings.ToLower(steps), "inspect the target") {
		t.Fatalf("next steps still ask for a target inspection:\n%s", steps)
	}
	// Nothing pins a ref unless the reviewed build source has one.
	if strings.Contains(steps, "reviewed Git ref") {
		t.Fatalf("next steps mention a ref that is not matched on:\n%s", steps)
	}
}

func TestExternalActionNextStepsFallBackToTheExamplesRepository(t *testing.T) {
	blocked := &ExternalActionRequiredError{
		Instance: "prod", ServiceName: "php", TargetServiceID: 42,
	}
	steps := blocked.NextSteps()
	if !strings.Contains(steps, "Update your CI pipeline to Wodby CLI 2.x") {
		t.Fatalf("unknown provider should stay generic:\n%s", steps)
	}
	if strings.Count(steps, wodbyCIRepositoryURL) < 2 {
		t.Fatalf("next steps must still link the examples repository:\n%s", steps)
	}
}

func TestExternalActionNextStepsNameThePinnedRef(t *testing.T) {
	blocked := &ExternalActionRequiredError{
		Instance: "prod", ServiceName: "php", TargetServiceID: 42, GitRef: "main",
	}
	if !strings.Contains(blocked.NextSteps(), `Build the reviewed Git ref "main"`) {
		t.Fatalf("pinned ref is not reported:\n%s", blocked.NextSteps())
	}
}

func TestPausedMigrationIsRecognizedThroughWrapping(t *testing.T) {
	blocked := &ExternalActionRequiredError{Instance: "prod", ServiceName: "php", TargetServiceID: 42}
	wrapped := errors.Wrap(&MigrationPausedError{Actions: []*ExternalActionRequiredError{blocked}}, "migration apply")

	paused, ok := AsMigrationPaused(wrapped)
	if !ok || len(paused.Actions) != 1 || paused.Actions[0] != blocked {
		t.Fatalf("AsMigrationPaused() = %#v, %v", paused, ok)
	}
	if _, ok := AsExternalActionRequired(wrapped); !ok {
		t.Fatal("the detail type must stay reachable through the aggregate")
	}
	if _, ok := AsMigrationPaused(errors.New("target rejected the deployment")); ok {
		t.Fatal("a real failure must not be reported as paused")
	}
}

func TestPausedMigrationRendersEveryPendingInstance(t *testing.T) {
	paused := &MigrationPausedError{Actions: []*ExternalActionRequiredError{
		{Instance: "prod", ServiceName: "php", TargetServiceID: 1},
		{Instance: "stage", ServiceName: "php", TargetServiceID: 2},
	}}
	steps := paused.NextSteps()
	if !strings.Contains(steps, "WODBY_APP_SERVICE_ID  1") || !strings.Contains(steps, "WODBY_APP_SERVICE_ID  2") {
		t.Fatalf("every paused instance needs its own app service ID:\n%s", steps)
	}
	if !strings.Contains(paused.Error(), "2 instances") {
		t.Fatalf("aggregate summary = %q", paused.Error())
	}
}

func TestWodbyCIExampleURLPrefersTheMostSpecificPage(t *testing.T) {
	tests := []struct {
		stack, path, want string
	}{
		{"drupal", "github-actions/wodby.yml", "https://github.com/wodby/wodby-ci/blob/2.0/drupal/github-actions/wodby.yml"},
		{"drupal", "", "https://github.com/wodby/wodby-ci/tree/2.0/drupal"},
		{"", "github-actions/wodby.yml", wodbyCIRepositoryURL},
	}
	for _, test := range tests {
		if got := wodbyCIExampleURL(test.stack, test.path); got != test.want {
			t.Fatalf("wodbyCIExampleURL(%q, %q) = %q, want %q", test.stack, test.path, got, test.want)
		}
	}
}
