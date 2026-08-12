package migrate

import (
	"io"
	"os"
	"strings"
)

const (
	cliColorReset  = "\x1b[0m"
	cliColorBold   = "\x1b[1m"
	cliColorRed    = "\x1b[31m"
	cliColorGreen  = "\x1b[32m"
	cliColorCyan   = "\x1b[36m"
	cliColorOrange = "\x1b[38;5;208m"
)

func cliColorEnabled(w io.Writer) bool {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		return false
	}
	if force := strings.TrimSpace(os.Getenv("CLICOLOR_FORCE")); force != "" && force != "0" {
		return true
	}
	file, ok := w.(*os.File)
	if !ok || strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func cliColor(w io.Writer, code, value string) string {
	if !cliColorEnabled(w) || value == "" {
		return value
	}
	return code + value + cliColorReset
}

func progressMessageColor(message string) string {
	lower := strings.ToLower(message)
	for _, value := range []string{
		" completed", " complete", " created", " configured", " passed", " is ok", " now uses",
		"already uses", "already enabled", "already disabled", "prepared", "succeeded", "verified",
	} {
		if strings.Contains(lower, value) {
			return cliColorGreen
		}
	}
	for _, value := range []string{"warning", "--force", "skipping", "skipped", "deferred", "bypass"} {
		if strings.Contains(lower, value) {
			return cliColorOrange
		}
	}
	return ""
}
