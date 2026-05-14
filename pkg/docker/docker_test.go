package docker

import (
	"io"
	"reflect"
	"testing"
)

func TestLoginCommandUsesPasswordStdin(t *testing.T) {
	password := "p@ss with spaces; $HOME `cmd`"
	cmd := loginCommand("registry.example.com", "user;name", password)

	wantArgs := []string{
		"docker",
		"login",
		"-u",
		"user;name",
		"--password-stdin",
		"registry.example.com",
	}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("login command args = %v, want %v", cmd.Args, wantArgs)
	}

	gotPassword, err := io.ReadAll(cmd.Stdin)
	if err != nil {
		t.Fatalf("ReadAll(stdin) error = %v", err)
	}
	if string(gotPassword) != password {
		t.Fatalf("login command stdin = %q, want %q", gotPassword, password)
	}
}

func TestChownSpec(t *testing.T) {
	tests := []struct {
		user string
		want string
	}{
		{user: "", want: "root:root"},
		{user: "root", want: "root:root"},
		{user: "wodby", want: "wodby:wodby"},
		{user: "1000", want: "1000:1000"},
		{user: "1000:1000", want: "1000:1000"},
		{user: "wodby:www-data", want: "wodby:www-data"},
	}

	for _, tt := range tests {
		t.Run(tt.user, func(t *testing.T) {
			if got := ChownSpec(tt.user); got != tt.want {
				t.Fatalf("ChownSpec(%q) = %q, want %q", tt.user, got, tt.want)
			}
		})
	}
}
