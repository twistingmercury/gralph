package looper

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// BypassArgs returns the claude flags for a session with no sandbox and no
// permission checks: what every session ran with before ADR-014.
func BypassArgs() []string {
	return []string{"--dangerously-skip-permissions"}
}

// SandboxArgs reads the Claude Code settings file at path and returns the
// claude flags for a sandboxed session. The settings travel inline, so there
// is no temporary file to clean up, and the file at path is never written.
func SandboxArgs(path string) ([]string, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("--sandbox-settings: file %q is not accessible: %w", path, err)
	}

	settings, err := forceSandbox(data)
	if err != nil {
		return nil, fmt.Errorf("--sandbox-settings: %w", err)
	}

	return []string{"--permission-mode", "acceptEdits", "--settings", settings}, nil
}

// forceSandbox returns the settings in data with the three sandbox keys a
// file must not be able to weaken set: the sandbox on, no way for the session
// to ask its way out, and no carrying on unsandboxed when it cannot start.
// Values are kept as raw JSON so everything else reaches Claude as written.
func forceSandbox(data []byte) (string, error) {
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		return "", fmt.Errorf("must be a JSON object: %w", err)
	}

	if settings == nil {
		return "", errors.New("must be a JSON object")
	}

	sandbox := map[string]json.RawMessage{}
	if raw, ok := settings["sandbox"]; ok {
		if err := json.Unmarshal(raw, &sandbox); err != nil || sandbox == nil {
			return "", errors.New("sandbox: must be a JSON object")
		}
	}

	sandbox["enabled"] = json.RawMessage("true")
	sandbox["allowUnsandboxedCommands"] = json.RawMessage("false")
	sandbox["failIfUnavailable"] = json.RawMessage("true")

	forced, err := json.Marshal(sandbox)
	if err != nil {
		return "", err
	}

	settings["sandbox"] = forced
	merged, err := json.Marshal(settings)
	if err != nil {
		return "", err
	}

	return string(merged), nil
}
