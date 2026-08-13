package run

import (
	"testing"
)

func TestRunUserOverride(t *testing.T) {
	if got := runUserOverride(""); got != "" {
		t.Fatalf("runUserOverride(\"\") = %q, want image default", got)
	}
	if got := runUserOverride("1001:1002"); got != "1001:1002" {
		t.Fatalf("runUserOverride() = %q, want explicit identity", got)
	}
}

func TestShouldClearImageEntrypoint(t *testing.T) {
	tests := []struct {
		name               string
		explicitEntrypoint string
		user               string
		want               bool
	}{
		{name: "numeric uid gid", user: "1001:1001", want: true},
		{name: "numeric uid named group", user: "1001:www-data", want: true},
		{name: "named user", user: "wodby", want: false},
		{name: "explicit entrypoint", explicitEntrypoint: "composer", user: "1001:1001", want: false},
		{name: "no user", user: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldClearImageEntrypoint(tt.explicitEntrypoint, tt.user)
			if got != tt.want {
				t.Fatalf("shouldClearImageEntrypoint(%q, %q) = %t, want %t", tt.explicitEntrypoint, tt.user, got, tt.want)
			}
		})
	}
}
