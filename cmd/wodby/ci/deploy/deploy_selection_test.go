package deploy

import (
	"reflect"
	"strings"
	"testing"

	"github.com/wodby/wodby-cli/pkg/types"
)

func TestDeploymentServices(t *testing.T) {
	builtServices := []types.BuiltService{
		{Name: "php", Image: "registry.example.com/demo:php-1", Released: true},
		{Name: "nginx", Image: "registry.example.com/demo:nginx-1", Released: true},
		{Name: "node", Image: "registry.example.com/demo:node-1"},
	}

	t.Run("returns all released services by default", func(t *testing.T) {
		got, err := deploymentServices(builtServices, nil)
		if err != nil {
			t.Fatalf("deploymentServices() error = %v", err)
		}

		want := []*types.ServiceDeploymentInput{
			{Name: "php", Image: "registry.example.com/demo:php-1"},
			{Name: "nginx", Image: "registry.example.com/demo:nginx-1"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("deploymentServices() = %#v, want %#v", got, want)
		}
	})

	t.Run("rejects unreleased requested service", func(t *testing.T) {
		_, err := deploymentServices(builtServices, []string{"node"})
		if err == nil || !strings.Contains(err.Error(), "hasn't been released") {
			t.Fatalf("deploymentServices() error = %v, want unreleased error", err)
		}
	})

	t.Run("rejects missing requested service", func(t *testing.T) {
		_, err := deploymentServices(builtServices, []string{"typo"})
		if err == nil || !strings.Contains(err.Error(), "No built images found") {
			t.Fatalf("deploymentServices() error = %v, want missing service error", err)
		}
	})
}
