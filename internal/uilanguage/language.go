// Package uilanguage resolves the local interface language from the user's
// macOS language preference and an optional Team Cross override.
package uilanguage

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	Auto    = "auto"
	Chinese = "zh-CN"
	English = "en"
)

func Valid(mode string) bool {
	return mode == Auto || mode == Chinese || mode == English
}

func Mode(saved string) string {
	if Valid(saved) {
		return saved
	}
	return Auto
}

func FromLanguage(language string) string {
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "zh" || strings.HasPrefix(language, "zh-") || strings.HasPrefix(language, "zh_") {
		return Chinese
	}
	return English
}

func firstAppleLanguage(output string) string {
	for _, line := range strings.Split(output, "\n") {
		value := strings.Trim(strings.TrimSpace(line), "\"', ()\t\r")
		if value != "" {
			return value
		}
	}
	return ""
}

func System() string {
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	if output, err := exec.CommandContext(ctx, "/usr/bin/defaults", "read", "-g", "AppleLanguages").Output(); err == nil {
		if language := firstAppleLanguage(string(output)); language != "" {
			return FromLanguage(language)
		}
	}
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if value := os.Getenv(name); value != "" {
			return FromLanguage(strings.Split(value, ".")[0])
		}
	}
	return English
}

func Resolve(mode string) string {
	if mode == Chinese || mode == English {
		return mode
	}
	return System()
}
