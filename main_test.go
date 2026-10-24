package main

import "testing"

func TestRenderWidth(t *testing.T) {
	tests := []struct {
		name           string
		forceFlagWidth bool
		allowNoTTY     bool
		termWidth      int
		termOK         bool
		flagWidth      int
		wantWidth      int
		wantOK         bool
	}{
		// --preview / --dump-config: always the flag width, terminal or not
		{name: "preview without terminal", forceFlagWidth: true, flagWidth: 120, wantWidth: 120, wantOK: true},
		{name: "preview in terminal ignores terminal width", forceFlagWidth: true, termWidth: 200, termOK: true, flagWidth: 120, wantWidth: 120, wantOK: true},
		{name: "preview default width", forceFlagWidth: true, wantWidth: 80, wantOK: true},
		{name: "preview negative width", forceFlagWidth: true, flagWidth: -5, wantWidth: 80, wantOK: true},
		{name: "preview in terminal default width", forceFlagWidth: true, termWidth: 200, termOK: true, wantWidth: 80, wantOK: true},
		{name: "preview with no-color", forceFlagWidth: true, allowNoTTY: true, termWidth: 200, termOK: true, flagWidth: 100, wantWidth: 100, wantOK: true},

		// --no-color
		{name: "no-color in terminal uses terminal width", allowNoTTY: true, termWidth: 132, termOK: true, flagWidth: 120, wantWidth: 132, wantOK: true},
		{name: "no-color in terminal without flag width", allowNoTTY: true, termWidth: 132, termOK: true, wantWidth: 132, wantOK: true},
		{name: "no-color without terminal uses flag width", allowNoTTY: true, flagWidth: 120, wantWidth: 120, wantOK: true},
		{name: "no-color without terminal default width", allowNoTTY: true, wantWidth: 80, wantOK: true},
		{name: "no-color without terminal negative width", allowNoTTY: true, flagWidth: -1, wantWidth: 80, wantOK: true},
		{name: "no-color when terminal size unknown falls back to flag width", allowNoTTY: true, flagWidth: 120, wantWidth: 120, wantOK: true},

		// plain run (also NO_COLOR env only): terminal required
		{name: "plain in terminal", termWidth: 100, termOK: true, wantWidth: 100, wantOK: true},
		{name: "plain in terminal ignores flag width", termWidth: 100, termOK: true, flagWidth: 120, wantWidth: 100, wantOK: true},
		{name: "plain without terminal renders nothing", flagWidth: 120, wantOK: false},
		{name: "plain without terminal and no flag width renders nothing", wantOK: false},
		{name: "plain with unknown terminal size renders nothing", flagWidth: 0, wantOK: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			gotWidth, gotOK := renderWidth(testCase.forceFlagWidth, testCase.allowNoTTY, testCase.termWidth, testCase.termOK, testCase.flagWidth)
			if gotOK != testCase.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, testCase.wantOK)
			}
			if gotWidth != testCase.wantWidth {
				t.Errorf("width = %d, want %d", gotWidth, testCase.wantWidth)
			}
		})
	}
}
