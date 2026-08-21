package looper

import (
	"fmt"
	"strings"

	"github.com/twistingmercury/gralph/internal/agent"
)

// Config contains the files, retry policy, and agent command for a loop run.
type Config struct {
	PromptPath   string
	PRDPath      string
	ProgressPath string
	MaxAttempts  int
	AgentCommand agent.AgentCommand
}

// Validate checks the configuration values that do not require filesystem I/O.
func (c Config) Validate() error {
	if strings.TrimSpace(c.PromptPath) == "" {
		return fmt.Errorf("prompt path must not be blank")
	}
	if strings.TrimSpace(c.PRDPath) == "" {
		return fmt.Errorf("PRD path must not be blank")
	}
	if c.MaxAttempts < 1 {
		return fmt.Errorf("maximum attempts must be at least 1")
	}
	if err := c.AgentCommand.Validate(); err != nil {
		return fmt.Errorf("agent command: %w: %w", agent.ErrInvalidConfiguration, err)
	}

	return nil
}
