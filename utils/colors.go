package utils

import (
	"fmt"
	"regexp"
)

var (
	Good     = green
	Warn     = yellow
	Err      = red
	NoColors = false
)

var (
	// black   = Color("\033[1;30m%s\033[0m")
	red    = Color("\033[1;31m%s\033[0m")
	green  = Color("\033[1;32m%s\033[0m")
	yellow = Color("\033[1;33m%s\033[0m")
	// purple  = Color("\033[1;34m%s\033[0m")
	// magenta = Color("\033[1;35m%s\033[0m")
	// teal    = Color("\033[1;36m%s\033[0m")
	// white   = Color("\033[1;37m%s\033[0m")
)

func Color(colorString string) func(...interface{}) string {
	sprint := func(args ...interface{}) string {
		if NoColors {
			return fmt.Sprint(args...)
		}

		return fmt.Sprintf(colorString,
			fmt.Sprint(args...))
	}

	return sprint
}

// ansiRe matches CSI escape sequences such as color codes (e.g. "\x1b[1;32m", "\x1b[38;5;123m").
var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")

// StripANSI removes ANSI CSI escape sequences (colors, resets) from s.
func StripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}
