package ciuser

import (
	"testing"

	"github.com/pkg/errors"
)

func TestResolveBindUser(t *testing.T) {
	tests := []struct {
		name       string
		current    string
		currentErr error
		owner      string
		want       string
		wantErr    bool
	}{
		{name: "uses current non-root identity", current: "1001:121", want: "1001:121"},
		{name: "uses workspace owner for root cli", current: "0:0", owner: "1002:121", want: "1002:121"},
		{name: "keeps root for root-owned workspace", current: "0:0", owner: "0:0", want: "0:0"},
		{name: "returns current identity errors", currentErr: errors.New("boom"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveBindUser(
				"/workspace",
				func() (string, error) { return tt.current, tt.currentErr },
				func(string) (string, error) { return tt.owner, nil },
			)
			if tt.wantErr {
				if err == nil {
					t.Fatal("resolveBindUser() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("resolveBindUser() = %q, want %q", got, tt.want)
			}
		})
	}
}
