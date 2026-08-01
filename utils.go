package main

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

func stringToInt64(s string) int64 {
	i, _ := strconv.ParseInt(s, 10, 64)
	return i
}

// CustomError redacts the bot token from error messages before they are shown anywhere.
func CustomError(err error) error {
	if err == nil {
		return nil
	}

	tokenRegex := regexp.MustCompile(`\d{9}:[A-Za-z0-9_-]{35}`)
	return errors.New(tokenRegex.ReplaceAllString(err.Error(), "$TOKEN"))
}

var htmlReplacer = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// esc escapes user-provided text (names, cards, ...) for safe inclusion in
// HTML parse-mode messages.
func esc(s string) string { return htmlReplacer.Replace(s) }

// truncate shortens s to at most n runes, adding an ellipsis when it cuts.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// anyText matches any message carrying non-blank text (conversation input).
func anyText(msg *gotgbot.Message) bool {
	return strings.TrimSpace(msg.GetText()) != ""
}
