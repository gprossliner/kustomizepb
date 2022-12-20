package main

import (
	"io"
	"os/exec"
)

type Command struct {
	Name string
	Args []string

	Stdin []byte

	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

func NewCommand(name string, arg ...string) *Command {
	return &Command{
		Name: name,
		Args: arg,
	}
}

func (c *Command) Run() error {

	cmd := exec.Command(c.Name, c.Args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	err = cmd.Start()
	if err != nil {
		return err
	}

	if len(c.Stdin) > 0 {
		_, err = stdin.Write(c.Stdin)
		if err != nil {
			return err
		}
	}

	stdin.Close()

	stdoutBytes, err := io.ReadAll(stdout)
	if err != nil {
		return err
	}

	c.Stdout = stdoutBytes

	stderrBytes, err := io.ReadAll(stderr)
	if err != nil {
		return err
	}
	c.Stderr = stderrBytes
	cmd.Wait()

	c.ExitCode = cmd.ProcessState.ExitCode()

	return nil
}
