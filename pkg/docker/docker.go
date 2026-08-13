package docker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/pkg/errors"
	"github.com/wodby/wodby-cli/pkg/exec"
)

// Client is docker client representation.
type Client struct{}

type RunConfig struct {
	Image           string
	Volumes         []string
	VolumesFrom     []string
	Env             []string
	EnvFile         string
	User            string
	WorkDir         string
	Entrypoint      string
	ClearEntrypoint bool
}

type ImageConfig struct {
	User       string            `json:"User"`
	WorkingDir string            `json:"WorkingDir"`
	Labels     map[string]string `json:"Labels"`
}

// Login authorizes in the registry.
func (c *Client) Login(host string, username string, password string) error {
	return cmdStartVerbose(loginCommand(host, username, password))
}

func loginCommand(host string, username string, password string) *exec.Cmd {
	cmd := exec.Command("docker", "login", "-u", username, "--password-stdin", host)
	cmd.Stdin = strings.NewReader(password)

	return cmd
}

// Build builds docker image.
func (c *Client) Build(dockerfile string, tags []string, context string, buildArgs map[string]string) error {
	return c.BuildWithRedactions(dockerfile, tags, context, buildArgs, nil)
}

// BuildWithRedactions builds a Docker image while removing sensitive values
// from both the displayed command and streamed Docker output.
func (c *Client) BuildWithRedactions(dockerfile string, tags []string, context string, buildArgs map[string]string, redactions []string) error {
	cmd := buildCommand(dockerfile, tags, context, buildArgs)
	fmt.Printf("Building:\n %s\n", redactString(strings.Join(cmd.Args, " "), redactions))

	return cmdStartVerboseRedacted(cmd, redactions)
}

// buildCommand explicitly uses Buildx and loads the result into the Docker
// image store because the release command pushes the locally built tags.
func buildCommand(dockerfile string, tags []string, context string, buildArgs map[string]string) *exec.Cmd {
	args := []string{"buildx", "build", "--load"}

	for _, tag := range tags {
		args = append(args, "-t", tag)
	}

	args = append(args, buildArgOptions(buildArgs)...)
	args = append(args, "-f", "-", context)

	cmd := exec.Command("docker", args...)
	cmd.Stdin = strings.NewReader(dockerfile)

	return cmd
}

func buildArgOptions(buildArgs map[string]string) []string {
	names := make([]string, 0, len(buildArgs))
	for name := range buildArgs {
		names = append(names, name)
	}
	sort.Strings(names)

	options := make([]string, 0, len(names)*2)
	for _, name := range names {
		value := buildArgs[name]
		if value == "" {
			options = append(options, "--build-arg", name)
		} else {
			options = append(options, "--build-arg", fmt.Sprintf("%s=%s", name, value))
		}
	}
	return options
}

func (c *Client) Push(image string) error {
	fmt.Printf("Pushing:\n docker push %s\n", image)
	cmd := exec.Command("docker", "push", image)

	return cmdStartVerbose(cmd)
}

func (c *Client) Pull(image string) error {
	fmt.Printf("Pulling:\n docker pull %s\n", image)
	cmd := exec.Command("docker", "pull", "-q", image)

	return cmdStartVerbose(cmd)
}

func (c *Client) Tag(image string, tag string) error {
	fmt.Printf("Tagging:\n docker tag %s %s\n", image, tag)
	cmd := exec.Command("docker", "tag", image, tag)

	return cmdStartVerbose(cmd)
}

func (c *Client) GetImageDefaultUser(image string) (string, error) {
	config, err := c.GetImageConfig(image)
	if err != nil {
		return "", err
	}

	return config.User, nil
}

func (c *Client) GetImageWorkingDir(image string) (string, error) {
	config, err := c.GetImageConfig(image)
	if err != nil {
		return "", err
	}

	return config.WorkingDir, nil
}

func (c *Client) GetImageConfig(image string) (ImageConfig, error) {
	config := ImageConfig{}

	if err := c.Pull(image); err != nil {
		return config, err
	}

	out, err := exec.Command("docker", "image", "inspect", image, "-f", "{{json .Config}}").CombinedOutput()
	if err != nil {
		return config, errors.Wrap(err, string(out))
	}

	if err := json.Unmarshal(bytes.TrimSpace(out), &config); err != nil {
		return config, errors.Wrap(err, "failed to decode image configuration")
	}

	if config.User == "" {
		config.User = "root"
	}
	if config.WorkingDir == "" {
		config.WorkingDir = "/"
	}
	if config.Labels == nil {
		config.Labels = map[string]string{}
	}

	return config, nil
}

// ChownSpec preserves explicit user:group image ownership and supplies the
// historical same-name group only when the image specifies a user alone.
func ChownSpec(user string) string {
	if user == "" {
		return "root:root"
	}
	if strings.Contains(user, ":") {
		return user
	}

	return fmt.Sprintf("%s:%s", user, user)
}

// Run runs docker container.
func (c *Client) Run(args []string, config RunConfig) error {
	command := []string{"run", "--rm"}
	for _, volumesFrom := range config.VolumesFrom {
		command = append(command, fmt.Sprintf("--volumes-from=%s", volumesFrom))
	}
	for _, volume := range config.Volumes {
		command = append(command, fmt.Sprintf("--volume=%s", volume))
	}
	for _, env := range config.Env {
		command = append(command, fmt.Sprintf("--env=%s", env))
	}
	if config.EnvFile != "" {
		command = append(command, fmt.Sprintf("--env-file=%s", config.EnvFile))
	}
	if config.User != "" {
		command = append(command, fmt.Sprintf("--user=%s", config.User))
	}
	if config.WorkDir != "" {
		command = append(command, fmt.Sprintf("--workdir=%s", config.WorkDir))
	}
	if config.ClearEntrypoint {
		command = append(command, "--entrypoint=")
	} else if config.Entrypoint != "" {
		command = append(command, fmt.Sprintf("--entrypoint=%s", config.Entrypoint))
	}
	command = append(append(command, config.Image), args...)

	fmt.Printf("Running:\n docker %s\n", strings.Join(command, " "))

	// Show run command progress.
	cmd := exec.Command("docker", command...)

	return cmdStartVerbose(cmd)
}

// NewClient creates new docker client.
func NewClient() *Client {
	return &Client{}
}

func cmdStartVerbose(cmd *exec.Cmd) error {
	return cmdStartVerboseRedacted(cmd, nil)
}

func cmdStartVerboseRedacted(cmd *exec.Cmd, redactions []string) error {
	return cmdStartVerboseRedactedTo(cmd, redactions, os.Stdout, os.Stderr)
}

func cmdStartVerboseRedactedTo(cmd *exec.Cmd, redactions []string, stdoutOutput io.Writer, stderrOutput io.Writer) error {
	stdoutIn, err := cmd.StdoutPipe()
	if err != nil {
		return errors.WithStack(err)
	}
	stderrIn, err := cmd.StderrPipe()
	if err != nil {
		return errors.WithStack(err)
	}

	var errStdout, errStderr error
	stdout := newRedactWriter(stdoutOutput, redactions)
	stderr := newRedactWriter(stderrOutput, redactions)

	if err := cmd.Start(); err != nil {
		return errors.WithStack(err)
	}

	var copies sync.WaitGroup
	copies.Add(2)
	go func() {
		defer copies.Done()
		_, errStdout = io.Copy(stdout, stdoutIn)
	}()
	go func() {
		defer copies.Done()
		_, errStderr = io.Copy(stderr, stderrIn)
	}()
	copies.Wait()

	errFlushStdout := stdout.Flush()
	errFlushStderr := stderr.Flush()

	if err := cmd.Wait(); err != nil {
		return errors.WithStack(err)
	}
	if errStdout != nil {
		return errors.Wrap(errStdout, "failed to copy command stdout")
	}
	if errStderr != nil {
		return errors.Wrap(errStderr, "failed to copy command stderr")
	}
	if errFlushStdout != nil {
		return errors.Wrap(errFlushStdout, "failed to flush command stdout")
	}
	if errFlushStderr != nil {
		return errors.Wrap(errFlushStderr, "failed to flush command stderr")
	}

	return nil
}

func redactString(value string, redactions []string) string {
	var output bytes.Buffer
	writer := newRedactWriter(&output, redactions)
	_, _ = writer.Write([]byte(value))
	_ = writer.Flush()
	return output.String()
}

type redactWriter struct {
	w                  io.Writer
	redactions         [][]byte
	maxRedactionLength int
	pending            []byte
}

func newRedactWriter(w io.Writer, redactions []string) *redactWriter {
	unique := make(map[string]struct{}, len(redactions))
	values := make([][]byte, 0, len(redactions))
	for _, value := range redactions {
		if value == "" {
			continue
		}
		if _, ok := unique[value]; ok {
			continue
		}
		unique[value] = struct{}{}
		values = append(values, []byte(value))
	}
	sort.Slice(values, func(i, j int) bool {
		return len(values[i]) > len(values[j])
	})

	maxLength := 0
	if len(values) > 0 {
		maxLength = len(values[0])
	}

	return &redactWriter{
		w:                  w,
		redactions:         values,
		maxRedactionLength: maxLength,
	}
}

func (w *redactWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	w.pending = append(w.pending, p...)
	if err := w.drain(false); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *redactWriter) Flush() error {
	return w.drain(true)
}

func (w *redactWriter) drain(final bool) error {
	if len(w.pending) == 0 {
		return nil
	}
	if len(w.redactions) == 0 {
		if err := writeAll(w.w, w.pending); err != nil {
			return err
		}
		w.clearPending(len(w.pending))
		return nil
	}

	limit := len(w.pending)
	if !final {
		limit -= w.maxRedactionLength - 1
		if limit <= 0 {
			return nil
		}
	}

	var output bytes.Buffer
	consumed := 0
	for consumed < limit {
		matchLength := w.matchLength(w.pending[consumed:])
		if matchLength > 0 {
			output.WriteString("*****")
			consumed += matchLength
			continue
		}
		output.WriteByte(w.pending[consumed])
		consumed++
	}

	if err := writeAll(w.w, output.Bytes()); err != nil {
		return err
	}
	w.clearPending(consumed)
	return nil
}

func (w *redactWriter) matchLength(p []byte) int {
	for _, value := range w.redactions {
		if bytes.HasPrefix(p, value) {
			return len(value)
		}
	}
	return 0
}

func (w *redactWriter) clearPending(consumed int) {
	for i := 0; i < consumed; i++ {
		w.pending[i] = 0
	}
	if consumed == len(w.pending) {
		w.pending = nil
		return
	}
	w.pending = w.pending[consumed:]
}

func writeAll(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if n > 0 {
			p = p[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
