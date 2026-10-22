package datasources

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dkaser/unraid-motd/utils"
)

const realisticUpscOutput = "battery.charge: 100\n" +
	"battery.charge.low: 10\n" +
	"battery.runtime: 3900\n" +
	"battery.type: PbAc\n" +
	"device.mfr: American Power Conversion\n" +
	"device.model: Back-UPS ES 700G\n" +
	"device.type: ups\n" +
	"driver.name: usbhid-ups\n" +
	"input.voltage: 230.0\n" +
	"ups.load: 23\n" +
	"ups.mfr: American Power Conversion\n" +
	"ups.model: Back-UPS ES 700G\n" +
	"ups.status: OL\n" +
	"ups.timer.delay: 0\n"

// newUPSConf returns a ready to use UPS config, as GetUPS would build it
func newUPSConf(warnOnly bool) *ConfUPS {
	var conf Conf
	conf.Init()

	sourceConf := conf.UPS
	sourceConf.Load(&conf)
	*sourceConf.WarnOnly = warnOnly

	return &sourceConf
}

func TestParseUPSC(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want map[string]string
	}{
		{
			name: "realistic output",
			in:   realisticUpscOutput,
			want: map[string]string{
				"battery.charge":     "100",
				"battery.charge.low": "10",
				"battery.runtime":    "3900",
				"battery.type":       "PbAc",
				"device.mfr":         "American Power Conversion",
				"device.model":       "Back-UPS ES 700G",
				"device.type":        "ups",
				"driver.name":        "usbhid-ups",
				"input.voltage":      "230.0",
				"ups.load":           "23",
				"ups.mfr":            "American Power Conversion",
				"ups.model":          "Back-UPS ES 700G",
				"ups.status":         "OL",
				"ups.timer.delay":    "0",
			},
		},
		{
			name: "CRLF line endings",
			in:   "ups.status: OL CHRG\r\nups.load: 5\r\n",
			want: map[string]string{"ups.status": "OL CHRG", "ups.load": "5"},
		},
		{
			name: "garbage and blank lines",
			in:   "\nInit SSL without certificate database\n  \nno separator\n: no key\nups.status: OB\n",
			want: map[string]string{"ups.status": "OB"},
		},
		{
			name: "value containing a separator",
			in:   "ups.id: a: b\n",
			want: map[string]string{"ups.id": "a: b"},
		},
		{
			name: "empty value is dropped",
			in:   "ups.status: \nups.load: 5\n",
			want: map[string]string{"ups.load": "5"},
		},
		{name: "empty string", in: "", want: map[string]string{}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := parseUPSC(testCase.in)
			if !reflect.DeepEqual(got, testCase.want) {
				t.Errorf("parseUPSC() = %#v, want %#v", got, testCase.want)
			}
		})
	}
}

func TestUPSStatusLevel(t *testing.T) {
	tests := []struct {
		status    string
		wantLevel upsLevel
		wantText  string
	}{
		{"OL", upsOK, "Online"},
		{"OL CHRG", upsOK, "Online, Charging"},
		{"OB DISCHRG", upsWarning, "On battery, Discharging"},
		{"OB LB", upsCritical, "On battery, Low battery"},
		{"OL RB", upsWarning, "Online, Replace battery"},
		{"FSD OB LB", upsCritical, "Forced shutdown, On battery, Low battery"},
		{"XYZ", upsWarning, "XYZ"},
	}

	for _, testCase := range tests {
		t.Run(testCase.status, func(t *testing.T) {
			level, text := upsStatusLevel(testCase.status)
			if level != testCase.wantLevel || text != testCase.wantText {
				t.Errorf("upsStatusLevel(%q) = (%d, %q), want (%d, %q)",
					testCase.status, level, text, testCase.wantLevel, testCase.wantText)
			}
		})
	}
}

func TestUPSBatteryLevel(t *testing.T) {
	conf := newUPSConf(false)

	tests := []struct {
		charge float64
		want   upsLevel
	}{
		{100, upsOK},
		{51, upsOK},
		{50, upsWarning},
		{21, upsWarning},
		{20, upsCritical},
		{0, upsCritical},
	}

	for _, testCase := range tests {
		if got := upsBatteryLevel(testCase.charge, conf); got != testCase.want {
			t.Errorf("upsBatteryLevel(%v) = %d, want %d", testCase.charge, got, testCase.want)
		}
	}
}

func TestUPSRowsUnparsableBattery(t *testing.T) {
	rows := upsRows(map[string]string{"ups.status": "OL", "battery.charge": "n/a"}, newUPSConf(false))
	if len(rows) != 1 || rows[0].label != "Status" {
		t.Errorf("upsRows() = %#v, want only the Status row", rows)
	}
}

func TestFormatRuntime(t *testing.T) {
	tests := []struct {
		seconds int
		want    string
	}{
		{0, "0m"},
		{59, "0m"},
		{60, "1m"},
		{725, "12m"},
		{3600, "1h 00m"},
		{3900, "1h 05m"},
	}

	for _, testCase := range tests {
		if got := formatRuntime(testCase.seconds); got != testCase.want {
			t.Errorf("formatRuntime(%d) = %q, want %q", testCase.seconds, got, testCase.want)
		}
	}
}

var (
	upsOnBattery = map[string]string{
		"device.model": "Back-UPS", "ups.status": "OB DISCHRG",
		"battery.charge": "40", "battery.runtime": "725", "ups.load": "30",
	}
	upsLowBattery = map[string]string{"device.model": "Back-UPS", "ups.status": "OB LB", "battery.charge": "10"}
	upsNoModel    = map[string]string{"ups.status": "OL", "battery.charge": "100"}
)

func TestFormatUPS(t *testing.T) {
	utils.NoColors = true

	healthy := parseUPSC(realisticUpscOutput)

	tests := []struct {
		name     string
		vars     map[string]string
		warnOnly bool
		want     []string
		notWant  []string
	}{
		{
			name: "all rows", vars: healthy, warnOnly: false,
			want:    []string{"UPS (Back-UPS ES 700G): OK", "Status", "Online", "Battery", "100%", "Runtime", "1h 05m", "Load", "23%"},
			notWant: []string{"Warning", "Critical"},
		},
		{
			name: "warnings only while healthy", vars: healthy, warnOnly: true,
			want:    []string{"UPS (Back-UPS ES 700G): OK"},
			notWant: []string{"Status", "Battery", "Runtime", "Load"},
		},
		{
			name: "warnings only on battery", vars: upsOnBattery, warnOnly: true,
			want:    []string{"UPS (Back-UPS): Warning", "Status", "On battery, Discharging", "Battery", "40%"},
			notWant: []string{"Runtime", "Load"},
		},
		{
			name: "warnings only low battery", vars: upsLowBattery, warnOnly: true,
			want: []string{"UPS (Back-UPS): Critical", "Low battery", "10%"},
		},
		{
			name: "model falls back to ups.model", vars: map[string]string{"ups.model": "Other", "ups.status": "OL"}, warnOnly: false,
			want: []string{"UPS (Other): OK"},
		},
		{
			name: "missing model", vars: upsNoModel, warnOnly: false,
			want:    []string{"UPS: OK"},
			notWant: []string{"UPS ("},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := formatUPS(testCase.vars, newUPSConf(testCase.warnOnly))

			for _, want := range testCase.want {
				if !strings.Contains(got, want) {
					t.Errorf("formatUPS() missing %q in:\n%s", want, got)
				}
			}

			for _, notWant := range testCase.notWant {
				if strings.Contains(got, notWant) {
					t.Errorf("formatUPS() unexpectedly contains %q in:\n%s", notWant, got)
				}
			}
		})
	}
}
