package wodby1

import (
	"context"
	"strings"
	"testing"
)

// A repository matched only by name must come from the Git host the Wodby 2
// integration connects to; naming it explicitly confirms another host.
func TestPreflightRepositoryHostMustMatchTheIntegration(t *testing.T) {
	for _, tc := range []struct {
		name, url, provider string
		explicit            bool
		blocked             bool
		want                string
	}{
		{name: "GitLab.com through GitLab", url: "git@gitlab.com:acme/example.git", provider: "gitlab"},
		{name: "Bitbucket over ssh", url: "ssh://git@bitbucket.org/acme/example.git", provider: "bitbucket"},
		{name: "GitHub https", url: "https://github.com/acme/example.git", provider: "github"},
		{name: "GitLab.com repository through GitHub", url: "git@gitlab.com:acme/example.git", provider: "github", blocked: true, want: "on github.com"},
		{name: "self-hosted GitLab", url: "ssh://git@git.example.org:2222/acme/example.git", provider: "gitlab", blocked: true, want: "self-hosted GitLab"},
		{name: "self-hosted GitLab confirmed by name", url: "ssh://git@git.example.org:2222/acme/example.git", provider: "gitlab", explicit: true},
		{name: "moved to GitHub and confirmed by name", url: "git@bitbucket.org:acme/example.git", provider: "github", explicit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			export := preflightFixtureExport(true)
			export.Apps[0].App.Repository.URL = tc.url
			export.Apps[0].Instances[0].Services = []Service{{Name: "php", Enabled: true}}
			catalog := preflightSingleBuildCatalog("php", false)
			catalog.gitProvider = tc.provider
			api := newPreflightTargetAPI(t, catalog)
			options := preflightOwnerPlanOptions()
			options.Repository = RepositoryTargetPlan{GitIntegrationID: 44}
			if tc.explicit {
				options.Repository.RepositoryName = "acme/example"
			}
			plan := preflightBuildPlan(t, export, options)
			prepared, err := api.client.PreflightTarget(context.Background(), export, &plan, TargetPreflightOptions{SkipData: true})
			if err != nil {
				t.Fatal(err)
			}
			blockers := []string{}
			for _, item := range plan.Review {
				if item.Severity == SeverityBlocking && item.Subject == "repository" {
					blockers = append(blockers, item.Message)
				}
			}
			if tc.blocked {
				if len(blockers) != 1 || !strings.Contains(blockers[0], tc.want) || !strings.Contains(blockers[0], "--target-repository-name acme/example") {
					t.Fatalf("blockers = %q", blockers)
				}
				if plan.Apps[0].Repository.RemoteGitRepoID != "" {
					t.Fatal("an unconfirmed repository must not be linked")
				}
				return
			}
			if len(blockers) != 0 {
				t.Fatalf("unexpected blockers: %q", blockers)
			}
			if build := prepared.Instances[0].BuildSource; build == nil || build.Input.RemoteGitRepoID == nil || *build.Input.RemoteGitRepoID != "remote-repo-17" {
				t.Fatalf("the repository must be linked: %#v", build)
			}
		})
	}
}

func TestRepositoryHost(t *testing.T) {
	for input, want := range map[string]string{
		"git@gitlab.com:acme/example.git":                  "gitlab.com",
		"GIT@GitHub.com:acme/example.git":                  "github.com",
		"ssh://git@git.example.org:2222/group/example.git": "git.example.org",
		"https://user@bitbucket.org/acme/example.git":      "bitbucket.org",
		"/srv/git/example.git":                             "",
		"":                                                 "",
	} {
		if got := repositoryHost(input); got != want {
			t.Errorf("repositoryHost(%q) = %q, want %q", input, got, want)
		}
	}
}
