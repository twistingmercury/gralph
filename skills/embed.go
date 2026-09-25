// Package skills embeds the Claude Code skills shipped with gralph.
package skills

import "embed"

// FS holds the gralph-docs-writer skill folder, SKILL.md and templates/.
//
//go:embed gralph-docs-writer
var FS embed.FS
