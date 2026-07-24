package docker

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strconv"
	"testing"

	"github.com/wodby/wodby-cli/pkg/exec"
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

func TestRedactWriterRedactsSecretsAcrossWriteBoundaries(t *testing.T) {
	const secret = "supersecret"

	for split := 0; split <= len(secret); split++ {
		t.Run("split-"+strconv.Itoa(split), func(t *testing.T) {
			var output bytes.Buffer
			writer := newRedactWriter(&output, []string{secret})

			if _, err := writer.Write([]byte("before " + secret[:split])); err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Write([]byte(secret[split:] + " after")); err != nil {
				t.Fatal(err)
			}
			if err := writer.Flush(); err != nil {
				t.Fatal(err)
			}

			if got, want := output.String(), "before ***** after"; got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestRedactWriterRedactsShortAndOverlappingSecrets(t *testing.T) {
	var output bytes.Buffer
	writer := newRedactWriter(&output, []string{"1", "1234", "secret", "secret-value", "1234"})

	if _, err := writer.Write([]byte("1 1234 secret-value secret")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}

	if got, want := output.String(), "***** ***** ***** *****"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestRedactWriterFlushesSecretAtEndOfStream(t *testing.T) {
	var output bytes.Buffer
	writer := newRedactWriter(&output, []string{"tail-secret"})

	if _, err := writer.Write([]byte("output tail-secret")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}

	if got, want := output.String(), "output *****"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestRedactWriterPreservesOrdinaryOutputAndIgnoresEmptyValues(t *testing.T) {
	const input = "ordinary output\nwithout any secrets"
	var output bytes.Buffer
	writer := newRedactWriter(&output, []string{"", ""})

	if _, err := writer.Write([]byte(input)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}

	if got := output.String(); got != input {
		t.Fatalf("output = %q, want %q", got, input)
	}
}

func TestRedactWriterPropagatesOutputErrors(t *testing.T) {
	wantErr := errors.New("output unavailable")
	writer := newRedactWriter(errorWriter{err: wantErr}, []string{"secret"})

	if _, err := writer.Write([]byte("secret and enough trailing output")); !errors.Is(err, wantErr) {
		t.Fatalf("Write() error = %v, want %v", err, wantErr)
	}
}

func TestRedactWriterHandlesShortWrites(t *testing.T) {
	var output bytes.Buffer
	writer := newRedactWriter(&shortWriter{w: &output, max: 2}, []string{"secret"})

	if _, err := writer.Write([]byte("before secret after")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}

	if got, want := output.String(), "before ***** after"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestCmdStartVerboseRedactedFlushesBothStreams(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(
		"sh",
		"-c",
		"printf 'stdout tail-secret'; printf 'stderr tail-secret' >&2",
	)

	if err := cmdStartVerboseRedactedTo(cmd, []string{"tail-secret"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if got, want := stdout.String(), "stdout *****"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got, want := stderr.String(), "stderr *****"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

type shortWriter struct {
	w   io.Writer
	max int
}

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) > w.max {
		p = p[:w.max]
	}
	return w.w.Write(p)
}
