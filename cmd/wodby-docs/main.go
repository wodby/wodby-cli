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
	return writeReference(cmd, outDir)
}

func prepareCommand(cmd *cobra.Command) {
	cmd.DisableAutoGenTag = true
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	for _, child := range cmd.Commands() {
		prepareCommand(child)
	}
}

func writeReference(cmd *cobra.Command, outDir string) error {
	var buf bytes.Buffer
	buf.WriteString(`# CLI reference

This reference is generated from the Wodby CLI command tree.

`)

	if err := appendCommandDocs(&buf, cmd); err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(outDir, "index.md"), buf.Bytes(), 0644)
}

func appendCommandDocs(buf *bytes.Buffer, cmd *cobra.Command) error {
	var cmdBuf bytes.Buffer
	if err := doc.GenMarkdownCustom(cmd, &cmdBuf, singlePageLink); err != nil {
		return err
	}

	buf.WriteString(commandSection(cmdBuf.String(), cmd))
	buf.WriteString("\n")

	for _, child := range cmd.Commands() {
		if !child.IsAvailableCommand() || child.IsAdditionalHelpTopicCommand() {
			continue
		}
		if err := appendCommandDocs(buf, child); err != nil {
			return err
		}
	}

	return nil
}

func commandFileName(cmd *cobra.Command) string {
	return strings.ReplaceAll(cmd.CommandPath(), " ", "_") + ".md"
}

func commandSection(content string, cmd *cobra.Command) string {
	anchor := strings.TrimSuffix(commandFileName(cmd), ".md")
	return fmt.Sprintf(`<a id="%s"></a>

%s`, anchor, promoteFirstHeading(content, cmd.CommandPath()))
}

func promoteFirstHeading(content string, commandPath string) string {
	from := "## " + commandPath + "\n\n"
	to := "## `" + commandPath + "`\n\n"
	return strings.Replace(content, from, to, 1)
}

func singlePageLink(link string) string {
	return "#" + strings.TrimSuffix(link, ".md")
}
