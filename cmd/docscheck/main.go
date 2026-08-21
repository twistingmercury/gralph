// Command docscheck verifies the checked CLI reference and local Markdown links.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	usageLine    = regexp.MustCompile(`(?m)^Usage of .+:$`)
	markdownLink = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)(?:\s+[^)]*)?\)`)
)

func main() {
	root := flag.String("root", ".", "repository root")
	helpFile := flag.String("help-file", "docs/cli-help.txt", "captured --help output")
	binary := flag.String("binary", "", "built gralph binary")
	flag.Parse()

	if *binary == "" {
		fail(errors.New("--binary is required"))
	}
	if err := checkHelp(*binary, filepath.Join(*root, *helpFile)); err != nil {
		fail(err)
	}
	if err := checkMarkdownLinks(*root); err != nil {
		fail(err)
	}
}

func checkHelp(binary, helpFile string) error {
	output, err := exec.Command(binary, "--help").CombinedOutput() // #nosec G204 -- binary is supplied by the Makefile.
	if err != nil {
		return fmt.Errorf("run %s --help: %w", binary, err)
	}
	actual := usageLine.ReplaceAllString(string(output), "Usage of gralph:")
	expected, err := os.ReadFile(helpFile)
	if err != nil {
		return fmt.Errorf("read captured help %q: %w", helpFile, err)
	}
	if strings.TrimSpace(actual) != strings.TrimSpace(string(expected)) {
		return fmt.Errorf("captured CLI help is stale; update %s\nexpected:\n%s\nactual:\n%s", helpFile, expected, actual)
	}
	return nil
}

func checkMarkdownLinks(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == ".bin") {
			return filepath.SkipDir
		}
		if entry.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range markdownLink.FindAllSubmatch(data, -1) {
			target := string(match[1])
			if skipLink(target) {
				continue
			}
			target = strings.SplitN(target, "#", 2)[0]
			resolved := filepath.Clean(filepath.Join(filepath.Dir(path), filepath.FromSlash(target)))
			info, err := os.Stat(resolved)
			if err != nil || info.IsDir() {
				return fmt.Errorf("%s: broken local Markdown link %q", path, target)
			}
		}
		return nil
	})
}

func skipLink(target string) bool {
	return target == "" || strings.HasPrefix(target, "#") || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:")
}

func fail(err error) {
	_, _ = fmt.Fprintln(os.Stderr, "docs-check:", err)
	os.Exit(1)
}
