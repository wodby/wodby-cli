package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"github.com/wodby/wodby-cli/cmd/wodby/root"
)

func main() {
	outDir := flag.String("dir", "./out/docs/cli-reference", "Output directory")
	flag.Parse()

	if err := generate(*outDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(outDir string) error {
	cmd := root.NewCommand()
	cmd.InitDefaultCompletionCmd()
	prepareCommand(cmd)

	if err := os.RemoveAll(outDir); err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	if err := writeIndex(outDir); err != nil {
		return err
	}

	return generateCommandDocs(cmd, outDir)
}

func prepareCommand(cmd *cobra.Command) {
	cmd.DisableAutoGenTag = true
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	for _, child := range cmd.Commands() {
		prepareCommand(child)
	}
}

func generateCommandDocs(cmd *cobra.Command, outDir string) error {
	for _, child := range cmd.Commands() {
		if !child.IsAvailableCommand() || child.IsAdditionalHelpTopicCommand() {
			continue
		}
		if err := generateCommandDocs(child, outDir); err != nil {
			return err
		}
	}

	var buf bytes.Buffer
	if err := doc.GenMarkdownCustom(cmd, &buf, func(link string) string {
		return link
	}); err != nil {
		return err
	}

	content := promoteFirstHeading(buf.String(), cmd.CommandPath())
	filename := filepath.Join(outDir, commandFileName(cmd))
	return os.WriteFile(filename, []byte(content), 0644)
}

func commandFileName(cmd *cobra.Command) string {
	return strings.ReplaceAll(cmd.CommandPath(), " ", "_") + ".md"
}

func promoteFirstHeading(content string, commandPath string) string {
	from := "## " + commandPath + "\n\n"
	to := "# `" + commandPath + "`\n\n"
	return strings.Replace(content, from, to, 1)
}

func writeIndex(outDir string) error {
	content := `# CLI reference

This reference is generated from the Wodby CLI command tree.

- [wodby](wodby.md)
`
	return os.WriteFile(filepath.Join(outDir, "index.md"), []byte(content), 0644)
}
