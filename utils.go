package main

import (
	"errors"
	"regexp"
	"strconv"
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
