package claudecode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// Runner runs the binary with args in dir. It returns an error wrapping
// ErrNotStarted when nothing ran, or ErrTimeout when the context ended
// first; a non-zero exit is a Result, not an error.
type Runner interface {
	Run(ctx context.Context, dir string, args ...string) (Result, error)
}

type Result struct {
	Stdout, Stderr []byte
	Code           int
}

var (
	ErrNotStarted = errors.New("claude process did not start")
	ErrTimeout    = errors.New("claude process timed out")
)

type execRunner struct{ bin string }

func (r execRunner) Run(ctx context.Context, dir string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, r.bin, args...)
	cmd.Dir = dir
	cmd.WaitDelay = 5 * time.Second
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	res := Result{Stdout: out.Bytes(), Stderr: errb.Bytes(), Code: cmd.ProcessState.ExitCode()}
	if cmd.ProcessState == nil {
		return res, fmt.Errorf("%w: %v", ErrNotStarted, err)
	}
	if ctx.Err() != nil {
		return res, fmt.Errorf("%w: %v", ErrTimeout, ctx.Err())
	}
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) && !errors.Is(err, exec.ErrWaitDelay) {
		return res, err
	}
	return res, nil
}
