package runtime

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// ─── DockerRunner ─────────────────────────────────────────────────────────────

// DockerRunner is the concrete implementation of Runner that executes jobs
// inside isolated Docker containers using the `docker` CLI via os/exec.
//
// Each job runs in a fresh container that is force-removed on completion,
// timeout, or cancellation. Timeout is enforced via context.WithTimeout; when
// the deadline fires the container is hard-killed before the call returns.
//
// Every request is validated by the embedded WorkspaceGuard before Docker is
// involved; this blocks path traversal and credential access at the host layer.
//
// Usage:
//
//	guard, _ := runtime.NewWorkspaceGuard("/var/vclaw/workspaces")
//	runner := runtime.NewDockerRunner(runtime.DockerRunnerConfig{Guard: guard})
//	result, err := runner.RunPython(ctx, req)
type DockerRunner struct {
	cfg   DockerRunnerConfig
	guard *WorkspaceGuard
}

// DockerRunnerConfig holds tunable parameters for the DockerRunner.
type DockerRunnerConfig struct {
	// Image is the sandbox Docker image to use.
	// Defaults to "vclaw-sandbox:latest".
	Image string

	// MaxOutputBytes caps the combined bytes kept from stdout and stderr.
	// Defaults to runtime.MaxOutputBytes (128 KB).
	MaxOutputBytes int

	// StopTimeoutSec is the number of seconds given to `docker stop` before
	// the runner escalates to `docker kill`. Defaults to 3.
	StopTimeoutSec int

	// Guard enforces workspace directory restrictions. When non-nil, every
	// RunPython and RunShell call validates the workspace path through the
	// guard before dispatching to Docker. Strongly recommended in production.
	Guard *WorkspaceGuard
}

// NewDockerRunner creates a DockerRunner with the given config.
// Zero-value fields are replaced with sensible defaults.
func NewDockerRunner(cfg DockerRunnerConfig) *DockerRunner {
	if cfg.Image == "" {
		cfg.Image = "vclaw-sandbox:latest"
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = MaxOutputBytes
	}
	if cfg.StopTimeoutSec <= 0 {
		cfg.StopTimeoutSec = 3
	}
	return &DockerRunner{cfg: cfg, guard: cfg.Guard}
}

// ─── Runner interface ─────────────────────────────────────────────────────────

// RunPython implements Runner. It executes Python code or a script file
// in an isolated container with a per-job timeout.
func (r *DockerRunner) RunPython(ctx context.Context, req *RunPythonRequest) (*JobResult, error) {
	if err := ValidateRunPythonRequest(req); err != nil {
		return nil, err
	}

	// ── Workspace guard ──────────────────────────────────────────────────
	if r.guard != nil {
		if err := r.guard.ValidateWorkspaceDir(req.WorkspaceDir); err != nil {
			return nil, fmt.Errorf("run_python: %w", err)
		}
	}
	if strings.TrimSpace(req.ScriptPath) != "" {
		if err := ValidateScriptPath(req.ScriptPath); err != nil {
			return nil, fmt.Errorf("run_python: %w", err)
		}
	}

	timeout := EffectivePythonTimeout(req)
	jobID := newJobID()

	// Inline code → write a temp script into the workspace, run it, clean up.
	var scriptFile string
	if strings.TrimSpace(req.Code) != "" {
		fname := fmt.Sprintf("vclaw_job_%s.py", jobID)
		hostPath := filepath.Join(req.WorkspaceDir, fname)
		if err := os.WriteFile(hostPath, []byte(req.Code), 0644); err != nil {
			return nil, fmt.Errorf("run_python: failed to write code to workspace: %w", err)
		}
		defer os.Remove(hostPath)
		scriptFile = fname
	} else {
		scriptFile = req.ScriptPath
	}

	cmd := []string{"python", "/workspace/" + scriptFile}
	return r.dispatch(ctx, jobID, req.RequestID, req.WorkspaceDir, cmd, timeout)
}

// RunShell implements Runner. It executes a shell command in an isolated
// container via `sh -c` with a per-job timeout.
func (r *DockerRunner) RunShell(ctx context.Context, req *RunShellRequest) (*JobResult, error) {
	if err := ValidateRunShellRequest(req); err != nil {
		return nil, err
	}

	// ── Workspace guard ──────────────────────────────────────────────────
	if r.guard != nil {
		if err := r.guard.ValidateWorkspaceDir(req.WorkspaceDir); err != nil {
			return nil, fmt.Errorf("run_shell: %w", err)
		}
	}
	if err := ValidateShellCommand(req.Command); err != nil {
		return nil, fmt.Errorf("run_shell: %w", err)
	}

	timeout := EffectiveShellTimeout(req)
	jobID := newJobID()

	cmd := []string{"sh", "-c", req.Command}
	return r.dispatch(ctx, jobID, req.RequestID, req.WorkspaceDir, cmd, timeout)
}

// ─── Core dispatch ────────────────────────────────────────────────────────────

// dispatch runs cmd inside a Docker container with a timeout deadline.
// It handles:
//  1. Applying the timeout via context.WithTimeout.
//  2. Starting the container with a deterministic name for force-kill on timeout.
//  3. Streaming stdout/stderr into bounded buffers.
//  4. On timeout: hard-killing the container and returning JobTimeout.
//  5. On success/failure: returning the exit code and captured output.
func (r *DockerRunner) dispatch(
	ctx context.Context,
	jobID, requestID, workspaceDir string,
	cmd []string,
	timeout time.Duration,
) (*JobResult, error) {
	jobCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	containerName := "vclaw-job-" + jobID

	// Build the full docker run invocation.
	dockerArgs := r.buildDockerArgs(containerName, workspaceDir, cmd)
	dockerCmd := exec.Command("docker", dockerArgs...) //nolint:gosec

	var stdoutBuf, stderrBuf bytes.Buffer
	dockerCmd.Stdout = &stdoutBuf
	dockerCmd.Stderr = &stderrBuf

	start := time.Now()

	if err := dockerCmd.Start(); err != nil {
		return nil, fmt.Errorf("docker run start failed: %w", err)
	}

	// Wait for process completion in a goroutine so we can race against the
	// timeout context.
	waitResult := make(chan error, 1)
	go func() { waitResult <- dockerCmd.Wait() }()

	select {
	case waitErr := <-waitResult:
		// Job completed (success or failure) within the allowed time.
		elapsed := time.Since(start)
		return r.buildResult(requestID, jobID, waitErr, elapsed, &stdoutBuf, &stderrBuf), nil

	case <-jobCtx.Done():
		// Deadline exceeded or parent context cancelled.
		// Force-kill the container, then wait for the docker process to exit.
		elapsed := time.Since(start)
		r.killContainer(containerName)
		<-waitResult // drain to avoid goroutine leak

		reason := jobCtx.Err()
		status := JobTimeout
		if ctx.Err() != nil {
			// Parent context was cancelled, not our timeout.
			status = JobFailed
		}

		stdout, outTrunc := TruncateOutput(stdoutBuf.String())
		stderr, _ := TruncateOutput(stderrBuf.String())
		return &JobResult{
			RequestID:       requestID,
			JobID:           jobID,
			Status:          status,
			ExitCode:        -1,
			Stdout:          stdout,
			Stderr:          stderr + fmt.Sprintf("\n[sandbox] job killed: %v (limit %s)", reason, timeout),
			DurationMs:      elapsed.Milliseconds(),
			Artifacts:       []string{},
			OutputTruncated: outTrunc,
		}, nil
	}
}

// buildResult converts a docker Wait() outcome into a JobResult.
func (r *DockerRunner) buildResult(
	requestID, jobID string,
	waitErr error,
	elapsed time.Duration,
	stdoutBuf, stderrBuf *bytes.Buffer,
) *JobResult {
	stdout, outTrunc := TruncateOutput(stdoutBuf.String())
	stderr, errTrunc := TruncateOutput(stderrBuf.String())

	exitCode := 0
	status := JobSuccess
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
		status = JobFailed
	}

	return &JobResult{
		RequestID:       requestID,
		JobID:           jobID,
		Status:          status,
		ExitCode:        exitCode,
		Stdout:          stdout,
		Stderr:          stderr,
		DurationMs:      elapsed.Milliseconds(),
		Artifacts:       []string{},
		OutputTruncated: outTrunc || errTrunc,
	}
}

// buildDockerArgs constructs the full argument list for `docker run`.
// Security flags mirror those validated in smoke tests.
func (r *DockerRunner) buildDockerArgs(containerName, workspaceDir string, cmd []string) []string {
	stopTimeout := fmt.Sprintf("%d", r.cfg.StopTimeoutSec)
	args := []string{
		"run",
		"--rm",
		"--name", containerName,

		// ── Resource limits ────────────────────────────────────────────────
		"--memory", "256m",
		"--cpus", "0.5",
		"--pids-limit", "64",

		// ── Security ───────────────────────────────────────────────────────
		"--network", "none",
		"--read-only",
		"--security-opt", "no-new-privileges:true",
		"--cap-drop", "ALL",
		"--stop-timeout", stopTimeout,

		// ── Filesystem ─────────────────────────────────────────────────────
		"--tmpfs", "/tmp:size=64m",
		"--volume", workspaceDir + ":/workspace:rw",

		// ── Image ──────────────────────────────────────────────────────────
		r.cfg.Image,
	}
	return append(args, cmd...)
}

// killContainer sends `docker kill` to the named container.
// It is best-effort: errors are silently discarded because the container
// may have already exited on its own.
func (r *DockerRunner) killContainer(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, "docker", "kill", name).Run()
}

// ─── Job ID generator ─────────────────────────────────────────────────────────

var jobSeq uint64

// newJobID returns a short unique identifier for a sandbox job.
// It combines a nanosecond timestamp with an atomic counter so IDs are
// unique even when multiple jobs are dispatched in the same millisecond.
func newJobID() string {
	seq := atomic.AddUint64(&jobSeq, 1)
	ts := time.Now().UnixNano() / int64(time.Millisecond)
	return fmt.Sprintf("%d-%04d", ts, seq%10000)
}
