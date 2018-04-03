package exec

import (
	"bytes"
	"errors"
	"os/exec"
)

// Cmd is command representation.
type Cmd struct {
	*exec.Cmd
}

// SeparateOutput returns StdOut and StdErr simultaneously.
func (c *Cmd) SeparateOutput() ([]byte, []byte, error) {
	if c.Stdout != nil {
		return nil, nil, errors.New("exec: StdOut already set")
	}
	if c.Stderr != nil {
		return nil, nil, errors.New("exec: StdErr already set")
	}

	stdOut := new(bytes.Buffer)
	stdErr := new(bytes.Buffer)

	c.Stdout = stdOut
	c.Stderr = stdErr

	err := c.Run()
	if err != nil {
		return nil, nil, err
	}

	return stdOut.Bytes(), stdErr.Bytes(), nil
}

// Command is new command constructor.
func Command(name string, args ...string) *Cmd {
	cmd := exec.Command(name, args...)

	return &Cmd{cmd}
}

// PipeCommands returns StdOut and StdErr simultaneously.
func PipeCommands(cmds ...*exec.Cmd) ([]byte, error) {
	for i, cmd := range cmds[:len(cmds)-1] {
		out, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		cmd.Start()
		cmds[i+1].Stdin = out
	}
	final, err := cmds[len(cmds)-1].Output()

	for _, cmd := range cmds[:len(cmds)-1] {
		cmd.Process.Kill()
	}

	if err != nil {
		return nil, err
	}

	return final, nil
}
