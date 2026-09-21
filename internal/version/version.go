package version

import (
	"fmt"
)

var (
	version   = "dev"     // set via -ldflags: -X github.com/twistingmercury/gralph/internal/version.version=<value>
	buildDate = "unknown" // set via -ldflags: -X github.com/twistingmercury/gralph/internal/version.buildDate=<value>
	gitCommit = "unknown" // set via -ldflags: -X github.com/twistingmercury/gralph/internal/version.gitCommit=<value>
)

func Print() {
	fmt.Printf("gralph version: %s\ndate: %s\ncommit: %s\n", version, buildDate, gitCommit)
}
