package agent

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type commandRunnerInvocation struct {
	Args  []string `json:"args"`
	Stdin string   `json:"stdin"`
}

func TestCommandRunnerStdin(t *testing.T) {
	t.Parallel()

	t.Run("passes exact arguments and stdin without shell interpretation", func(t *testing.T) {
		t.Parallel()

		testExecutable, err := os.Executable()
		if !assert.NoError(t, err) {
			return
		}

		tempDir := t.TempDir()
		recordPath := filepath.Join(tempDir, "invocation with spaces.json")
		shellMarkerPath := filepath.Join(tempDir, "shell-marker")
		args := []string{
			"-test.run=^TestCommandRunnerStdinHelper$",
			"--",
			"--command-runner-stdin-helper",
			recordPath,
			"argument with spaces",
			"$(touch " + shellMarkerPath + ")",
			"semi;colon & ampersand | pipe",
		}
		prompt := "exact prompt with spaces\npath: /tmp/a path/with spaces\nmetacharacters: $(echo no) ; & | `no`\n"

		runner, err := NewCommandRunner(AgentCommand{
			Executable: testExecutable,
			Args:       args,
			PromptMode: PromptModeStdin,
		})
		if !assert.NoError(t, err) {
			return
		}

		outputPath, err := runner.Run(context.Background(), prompt)
		if !assert.NoError(t, err) {
			return
		}
		t.Cleanup(func() { _ = os.Remove(outputPath) })

		data, err := os.ReadFile(recordPath)
		if !assert.NoError(t, err) {
			return
		}
		var got commandRunnerInvocation
		if !assert.NoError(t, json.Unmarshal(data, &got)) {
			return
		}
		assert.Equal(t, args, got.Args)
		assert.Equal(t, prompt, got.Stdin)
		assert.NoFileExists(t, shellMarkerPath, "shell metacharacters must remain literal")
	})

	t.Run("validates the command before execution", func(t *testing.T) {
		t.Parallel()

		_, err := NewCommandRunner(AgentCommand{PromptMode: PromptModeStdin})
		assert.Error(t, err)
	})
}

func TestCommandRunnerArg(t *testing.T) {
	t.Parallel()

	testExecutable, err := os.Executable()
	if !assert.NoError(t, err) {
		return
	}

	tempDir := t.TempDir()
	recordPath := filepath.Join(tempDir, "arg invocation with spaces.json")
	shellMarkerPath := filepath.Join(tempDir, "arg-shell-marker")
	prompt := "exact prompt with spaces\nmetacharacters: $(touch " + shellMarkerPath + ") ; & | `no`\n"
	configuredArgs := []string{
		"-test.run=^TestCommandRunnerStdinHelper$",
		"--",
		"--command-runner-stdin-helper",
		recordPath,
		"argument before prompt",
		promptPlaceholder,
		"argument after prompt; still literal",
	}
	expectedArgs := append([]string(nil), configuredArgs...)
	expectedArgs[5] = prompt

	runner, err := NewCommandRunner(AgentCommand{
		Executable: testExecutable,
		Args:       configuredArgs,
		PromptMode: PromptModeArg,
	})
	if !assert.NoError(t, err) {
		return
	}

	outputPath, err := runner.Run(context.Background(), prompt)
	if !assert.NoError(t, err) {
		return
	}
	t.Cleanup(func() { _ = os.Remove(outputPath) })

	data, err := os.ReadFile(recordPath)
	if !assert.NoError(t, err) {
		return
	}
	var got commandRunnerInvocation
	if !assert.NoError(t, json.Unmarshal(data, &got)) {
		return
	}
	assert.Equal(t, expectedArgs, got.Args)
	assert.Empty(t, got.Stdin)
	assert.Equal(t, promptPlaceholder, configuredArgs[5], "configured arguments must not be mutated")
	assert.NoFileExists(t, shellMarkerPath, "shell metacharacters must remain literal")
}

func TestCommandRunnerErrors(t *testing.T) {
	t.Run("invalid configuration", func(t *testing.T) {
		_, err := NewCommandRunner(AgentCommand{PromptMode: PromptModeStdin})
		assert.ErrorIs(t, err, ErrInvalidConfiguration)
	})

	t.Run("setup failure", func(t *testing.T) {
		invalidTempDir := filepath.Join(t.TempDir(), "missing")
		t.Setenv("TMPDIR", invalidTempDir)
		t.Setenv("TEMP", invalidTempDir)
		t.Setenv("TMP", invalidTempDir)

		runner, err := NewCommandRunner(AgentCommand{
			Executable: os.Args[0],
			PromptMode: PromptModeStdin,
		})
		if !assert.NoError(t, err) {
			return
		}

		_, err = runner.Run(context.Background(), "prompt")
		assert.ErrorIs(t, err, ErrSetup)
		var pathErr *os.PathError
		assert.ErrorAs(t, err, &pathErr)
	})

	t.Run("missing executable", func(t *testing.T) {
		runner, err := NewCommandRunner(AgentCommand{
			Executable: "gralph-command-runner-definitely-missing-agent",
			PromptMode: PromptModeStdin,
		})
		if !assert.NoError(t, err) {
			return
		}

		outputPath, err := runner.Run(context.Background(), "prompt")
		t.Cleanup(func() { _ = os.Remove(outputPath) })
		assert.ErrorIs(t, err, ErrProcessStart)
		assert.NotErrorIs(t, err, ErrNonZeroExit)
		assert.ErrorIs(t, err, exec.ErrNotFound)
		var execErr *exec.Error
		assert.ErrorAs(t, err, &execErr)
	})

	t.Run("permission denial", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("executable permission bits are not supported on Windows")
		}

		executablePath := filepath.Join(t.TempDir(), "non-executable-agent")
		if !assert.NoError(t, os.WriteFile(executablePath, []byte("not executable"), 0o600)) {
			return
		}
		runner, err := NewCommandRunner(AgentCommand{
			Executable: executablePath,
			PromptMode: PromptModeStdin,
		})
		if !assert.NoError(t, err) {
			return
		}

		outputPath, err := runner.Run(context.Background(), "prompt")
		t.Cleanup(func() { _ = os.Remove(outputPath) })
		assert.ErrorIs(t, err, ErrProcessStart)
		assert.NotErrorIs(t, err, ErrNonZeroExit)
		assert.ErrorIs(t, err, os.ErrPermission)
		var pathErr *os.PathError
		assert.ErrorAs(t, err, &pathErr)
	})

	t.Run("cancellation", func(t *testing.T) {
		testExecutable, err := os.Executable()
		if !assert.NoError(t, err) {
			return
		}

		startedPath := filepath.Join(t.TempDir(), "started")
		runner, err := NewCommandRunner(AgentCommand{
			Executable: testExecutable,
			Args: []string{
				"-test.run=^TestCommandRunnerErrorsHelper$",
				"--",
				"--command-runner-errors-helper",
				"block",
				startedPath,
			},
			PromptMode: PromptModeStdin,
		})
		if !assert.NoError(t, err) {
			return
		}

		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		type runResult struct {
			outputPath string
			err        error
		}
		resultCh := make(chan runResult, 1)
		go func() {
			outputPath, runErr := runner.Run(ctx, "prompt")
			resultCh <- runResult{outputPath: outputPath, err: runErr}
		}()

		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, statErr := os.Stat(startedPath); statErr == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("timed out waiting for helper process to start")
			}
			time.Sleep(10 * time.Millisecond)
		}
		cancel()

		result := <-resultCh
		t.Cleanup(func() { _ = os.Remove(result.outputPath) })
		assert.ErrorIs(t, result.err, ErrCanceled)
		assert.ErrorIs(t, result.err, context.Canceled)
		var exitErr *exec.ExitError
		assert.ErrorAs(t, result.err, &exitErr)
	})

	t.Run("non-zero exit", func(t *testing.T) {
		testExecutable, err := os.Executable()
		if !assert.NoError(t, err) {
			return
		}

		runner, err := NewCommandRunner(AgentCommand{
			Executable: testExecutable,
			Args: []string{
				"-test.run=^TestCommandRunnerErrorsHelper$",
				"--",
				"--command-runner-errors-helper",
				"exit-one",
			},
			PromptMode: PromptModeStdin,
		})
		if !assert.NoError(t, err) {
			return
		}

		outputPath, err := runner.Run(context.Background(), "prompt")
		t.Cleanup(func() { _ = os.Remove(outputPath) })
		assert.ErrorIs(t, err, ErrNonZeroExit)
		assert.NotErrorIs(t, err, ErrProcessStart)
		var exitErr *exec.ExitError
		if assert.ErrorAs(t, err, &exitErr) {
			assert.Equal(t, 1, exitErr.ExitCode())
		}
	})
}

func TestCommandRunnerStdinHelper(t *testing.T) {
	const marker = "--command-runner-stdin-helper"

	markerIndex := -1
	for i, arg := range os.Args {
		if arg == marker {
			markerIndex = i
			break
		}
	}
	if markerIndex == -1 {
		return
	}
	if markerIndex+1 >= len(os.Args) {
		os.Exit(2)
	}

	stdin, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(3)
	}
	record := commandRunnerInvocation{
		Args:  append([]string(nil), os.Args[1:]...),
		Stdin: string(stdin),
	}
	data, err := json.Marshal(record)
	if err != nil {
		os.Exit(4)
	}
	if err := os.WriteFile(os.Args[markerIndex+1], data, 0o600); err != nil {
		os.Exit(5)
	}
	os.Exit(0)
}

func TestCommandRunnerErrorsHelper(t *testing.T) {
	const marker = "--command-runner-errors-helper"

	markerIndex := -1
	for i, arg := range os.Args {
		if arg == marker {
			markerIndex = i
			break
		}
	}
	if markerIndex == -1 {
		return
	}
	if markerIndex+1 >= len(os.Args) {
		os.Exit(2)
	}

	switch os.Args[markerIndex+1] {
	case "exit-one":
		os.Exit(1)
	case "block":
		if markerIndex+2 >= len(os.Args) {
			os.Exit(3)
		}
		if err := os.WriteFile(os.Args[markerIndex+2], nil, 0o600); err != nil {
			os.Exit(4)
		}
		time.Sleep(24 * time.Hour)
	default:
		os.Exit(5)
	}
}
