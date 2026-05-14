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
