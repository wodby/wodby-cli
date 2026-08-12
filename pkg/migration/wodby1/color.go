package wodby1

import (
	"io"
	"os"
	"strings"
)

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiCyan   = "\x1b[36m"
	ansiGray   = "\x1b[90m"
	ansiOrange = "\x1b[38;5;208m"
)

func migrationColorEnabled(w io.Writer) bool {
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

func migrationColor(w io.Writer, code, value string) string {
	if !migrationColorEnabled(w) || value == "" {
		return value
	}
	return code + value + ansiReset
}
