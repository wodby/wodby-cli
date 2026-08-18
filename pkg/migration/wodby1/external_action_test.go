package wodby1

import (
	"strings"
	"testing"

	"github.com/pkg/errors"
)

func TestExternalActionNextStepsExplainThePipelineWork(t *testing.T) {
	blocked := &ExternalActionRequiredError{
		Instance:          "prod",
		TargetInstanceID:  4100,
		ServiceName:       "php",
		TargetServiceID:   4200,
		ProviderKey:       "github",
		ProviderLabel:     "GitHub Actions",
		ProviderSupported: true,
		ExampleURL:        "https://github.com/wodby/wodby-ci/blob/2.0/drupal/github-actions/wodby.yml",
	}
	steps := blocked.NextSteps()
	for _, want := range []string{
		"prod",
		"GitHub Actions configuration to Wodby CLI 2.x",
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

// Wodby 2 has CI providers for GitHub, GitLab, and CircleCI only. Bitbucket
// Pipelines, Travis CI, and Jenkins are recognized on the Wodby 1 side but
// migrate as Custom CI, so the steps must not imply otherwise.
func TestExternalActionAdmitsWhenWodby2DoesNotSupportTheProvider(t *testing.T) {
	for _, test := range []struct{ key, label string }{
		{"travisci", "Travis CI"},
		{"bitbucket-pipelines", "Bitbucket Pipelines"},
		{"jenkins", "Jenkins"},
	} {
		t.Run(test.key, func(t *testing.T) {
			steps := (&ExternalActionRequiredError{
				Instance: "prod", ServiceName: "php", TargetServiceID: 42,
				ProviderKey: test.key, ProviderLabel: test.label,
				ExampleURL: "https://github.com/wodby/wodby-ci/tree/2.0/drupal",
			}).NextSteps()

			if !strings.Contains(steps, "Wodby 2 does not support "+test.label) ||
				!strings.Contains(steps, wodby2CIProviderLabels) {
				t.Fatalf("unsupported provider must be stated plainly:\n%s", steps)
			}
			// Without the override the build is recorded as "unknown".
			if !strings.Contains(steps, "wodby ci init --provider "+test.key) {
				t.Fatalf("unsupported provider needs the --provider hint:\n%s", steps)
			}
		})
	}
}

// The Wodby 1 CLI writes exactly these values to a build's provider field; see
// pkg/types/types.go on the Wodby 1 branch. Every one must map, or the
// migration silently degrades to generic naming and a root link.
func TestEveryWodby1AutodetectedProviderIsRecognized(t *testing.T) {
	for _, test := range []struct {
		stored, provider, label string
		wantExample             bool // Wodby 2 has this provider
	}{
		{"github", "github", "GitHub Actions", true},
		{"gitlab", "gitlab", "GitLab CI", true},
		{"circleci", "circleci", "CircleCI", true},
		{"travisci", "travisci", "Travis CI", false},
		{"bitbucket-pipelines", "bitbucket-pipelines", "Bitbucket Pipelines", false},
		{"jenkins", "jenkins", "Jenkins", false},
	} {
		t.Run(test.stored, func(t *testing.T) {
			provider, label, examplePath := normalizedWodby1CIProvider(test.stored)
			if provider != test.provider || label != test.label {
				t.Fatalf("normalizedWodby1CIProvider(%q) = %q, %q", test.stored, provider, label)
			}
			if (examplePath != "") != test.wantExample {
				t.Fatalf("%q example path = %q, want present = %v", test.stored, examplePath, test.wantExample)
			}
			// Example coverage and Wodby 2 provider support are the same set.
			if wodby2SupportsCIProvider(provider) != test.wantExample {
				t.Fatalf("%q support = %v, want %v", test.stored, !test.wantExample, test.wantExample)
			}
		})
	}
}

// Wodby 1 stores the literal "Unknown" when it detected no CI environment, and
// `wodby ci init --provider` takes free text.
func TestUnrecognizedWodby1ProvidersStayGeneric(t *testing.T) {
	for _, value := range []string{"Unknown", "", "  ", "teamcity", "some-internal-runner"} {
		if provider, label, path := normalizedWodby1CIProvider(value); provider != "" || label != "" || path != "" {
			t.Fatalf("normalizedWodby1CIProvider(%q) = %q, %q, %q", value, provider, label, path)
		}
	}
	// Spellings a person may type into --provider still resolve.
	for _, value := range []string{"GitHub Actions", "gitlab ci", "Circle CI", "Bitbucket Pipelines", "TRAVIS-CI"} {
		if provider, _, _ := normalizedWodby1CIProvider(value); provider == "" {
			t.Fatalf("normalizedWodby1CIProvider(%q) was not recognized", value)
		}
	}
}
