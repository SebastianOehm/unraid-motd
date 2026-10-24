package utils

import (
	"strings"
	"testing"
)

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "plain text", in: "hello world", want: "hello world"},
		{name: "single color", in: "\x1b[1;32mOK\x1b[0m", want: "OK"},
		{name: "reset only", in: "a\x1b[0mb", want: "ab"},
		{name: "multiple sequences", in: "\x1b[1;31mred\x1b[0m and \x1b[1;33myellow\x1b[0m", want: "red and yellow"},
		{name: "256 color", in: "\x1b[38;5;123mx\x1b[0m", want: "x"},
		{name: "truecolor", in: "\x1b[38;2;1;2;3mx\x1b[0m", want: "x"},
		{name: "box drawing kept", in: "\x1b[1;32m╭─ ✔ ─╮\x1b[0m", want: "╭─ ✔ ─╮"},
		{name: "newlines kept", in: "\x1b[32ma\x1b[0m\nb\n", want: "a\nb\n"},
		{name: "lone escape kept", in: "a\x1bb", want: "a\x1bb"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := StripANSI(testCase.in); got != testCase.want {
				t.Errorf("StripANSI(%q) = %q, want %q", testCase.in, got, testCase.want)
			}
		})
	}
}

func TestColorHonorsNoColors(t *testing.T) {
	orig := NoColors
	defer func() { NoColors = orig }()

	tests := []struct {
		name     string
		noColors bool
		wantANSI bool
	}{
		{name: "colors enabled", noColors: false, wantANSI: true},
		{name: "colors disabled", noColors: true, wantANSI: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			NoColors = testCase.noColors
			funcs := map[string]func(...interface{}) string{"Good": Good, "Warn": Warn, "Err": Err}
			for name, fn := range funcs {
				got := fn("x")
				if hasANSI := strings.Contains(got, "\x1b"); hasANSI != testCase.wantANSI {
					t.Errorf("%s(\"x\") = %q, contains ESC = %v, want %v", name, got, hasANSI, testCase.wantANSI)
				}
				if StripANSI(got) != "x" {
					t.Errorf("StripANSI(%s(\"x\")) = %q, want \"x\"", name, StripANSI(got))
				}
				if testCase.noColors && got != "x" {
					t.Errorf("%s(\"x\") = %q, want \"x\"", name, got)
				}
			}
		})
	}
}
