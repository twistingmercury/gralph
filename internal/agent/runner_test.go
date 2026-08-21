package agent

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

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
