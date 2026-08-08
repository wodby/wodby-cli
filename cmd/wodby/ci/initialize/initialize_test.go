package initialize

import "testing"

func TestProviderFlagHasShorthandAndNoDefault(t *testing.T) {
	flag := Cmd.Flags().Lookup("provider")
	if flag == nil {
		t.Fatal("provider flag was not registered")
	}
	if flag.Shorthand != "p" {
		t.Fatalf("provider shorthand = %q, want %q", flag.Shorthand, "p")
	}
	if flag.DefValue != "" {
		t.Fatalf("provider default = %q, want empty string", flag.DefValue)
	}
}
