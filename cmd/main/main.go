package main

import (
	"github/twistingmercury/gralph/internal/version"
	"os"

	"github.com/spf13/pflag"
)

var (
	helpFlag       = pflag.Bool("help", false, "Provides help instructions for gralph")
	versioinFlag   = pflag.Bool("version", false, "Show the current versoin of gralph")
	prdFlag        = pflag.String("prd-md", "", "The path to the 'PRD.md'file")
	promptFlag     = pflag.String("prompt-md", "", "The path to the 'PROMPT.md' file")
	progressFlag   = pflag.String("progress-file", "", "The path to the 'PROMPT.md' file")
	iterationsFlag = pflag.IntP("iterations", "i", 10, "The number of attempts to work on a task before abandoning it")
)

func main() {
	pflag.Parse()
	CheckVersion()

	println("I'm in danger!")
}

func CheckVersion() {
	if !*versioinFlag {
		return
	}

	version.Print()
	os.Exit(0)
}
