package config

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/spf13/viper"
	"github.com/wodby/wodby-cli/pkg/api"
	"github.com/wodby/wodby-cli/pkg/types"
)

func TestViperConfigRoundTripAfterDependencyUpgrade(t *testing.T) {
	want := &Config{
		UUID:       "instance-uuid",
		WorkingDir: "/var/www/html",
		Context:    "/workspace",
		API: &api.Config{
			Key:    "api-key",
			Scheme: "https",
			Host:   "api.wodby.com",
			Prefix: "api/v2",
		},
		BuildConfig: &types.BuildConfig{
			Services: map[string]types.Service{
				"php": {Name: "php", Image: "wodby/php", Slug: "registry.example/php"},
			},
			Title:   "Example",
			Default: "php",
		},
		Metadata: &types.BuildMetadata{
			Provider: types.GitHubActions,
			Number:   "42",
			Branch:   "master",
			Commit:   "deadbeef",
		},
	}

	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}

	v := viper.New()
	v.SetConfigType("json")
	if err := v.ReadConfig(bytes.NewReader(encoded)); err != nil {
		t.Fatal(err)
	}

	got := new(Config)
	if err := v.Unmarshal(got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.Marshal(got)
		t.Fatalf("round-trip config = %s, want %s", gotJSON, encoded)
	}
}
