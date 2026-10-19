package datasources

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const realisticVirshOutput = " Id   Name             State\n" +
	"----------------------------------\n" +
	" 1    Windows 11       running\n" +
	" -    Home Assistant   shut off\n" +
	" 3    ubuntu           paused\n" +
	" 4    vm-shutdown      in shutdown\n" +
	" 5    vm-suspended     pmsuspended\n" +
	" 6    vm-crashed       crashed\n" +
	" 7    vm-idle          idle\n" +
	" -    vm-nostate       no state\n" +
	"\n\n"

type parseVirshListCase struct {
	name string
	in   string
	want []containerStatus
}

var parseVirshListCases = []parseVirshListCase{
	{
		name: "realistic output with all known states",
		in:   realisticVirshOutput,
		want: []containerStatus{
			{"Windows 11", "running"},
			{"Home Assistant", "shut off"},
			{"ubuntu", "paused"},
			{"vm-shutdown", "in shutdown"},
			{"vm-suspended", "pmsuspended"},
			{"vm-crashed", "crashed"},
			{"vm-idle", "idle"},
			{"vm-nostate", "no state"},
		},
	},
	{
		name: "CRLF line endings",
		in:   " Id   Name   State\r\n-------------\r\n 1    Win 10   running\r\n -    other   shut off\r\n\r\n",
		want: []containerStatus{
			{"Win 10", "running"},
			{"other", "shut off"},
		},
	},
	{name: "empty string", in: "", want: nil},
	{name: "whitespace only", in: "\n  \n\n", want: nil},
	{name: "header only", in: " Id   Name   State\n--------------------\n\n", want: nil},
	{
		name: "unknown state uses last token",
		in:   " 2    some vm    weird\n",
		want: []containerStatus{{"some vm", "weird"}},
	},
	{
		name: "name starting with a state word",
		in:   " -    running-vm   shut off\n",
		want: []containerStatus{{"running-vm", "shut off"}},
	},
	{
		name: "name ending with a state word and real state",
		in:   " 1    my idle   running\n",
		want: []containerStatus{{"my idle", "running"}},
	},
	{
		name: "order preserved",
		in:   " 1 zeta running\n 2 alpha paused\n - mid shut off\n",
		want: []containerStatus{
			{"zeta", "running"},
			{"alpha", "paused"},
			{"mid", "shut off"},
		},
	},
}

func TestParseVirshList(t *testing.T) {
	for _, testCase := range parseVirshListCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := parseVirshList(testCase.in)
			if len(got) == 0 && len(testCase.want) == 0 {
				return
			}

			if !reflect.DeepEqual(got, testCase.want) {
				t.Errorf("parseVirshList() = %#v, want %#v", got, testCase.want)
			}
		})
	}
}

func TestParseVirshListEmptyIsLenZero(t *testing.T) {
	if got := parseVirshList(""); len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}

	if got := parseVirshList(" Id   Name   State\n------\n"); len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestVMServiceDisabled(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"disable", writeTempFile(t, dir, "disable.cfg", "SERVICE=\"disable\"\n"), true},
		{"disable uppercase", writeTempFile(t, dir, "disable_upper.cfg", "SERVICE=\"DISABLE\"\n"), true},
		{"enable", writeTempFile(t, dir, "enable.cfg", "SERVICE=\"enable\"\n"), false},
		{"no service key", writeTempFile(t, dir, "other.cfg", "FOO=\"bar\"\n"), false},
		{"missing file", filepath.Join(dir, "missing.cfg"), false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := vmServiceDisabled(testCase.path); got != testCase.want {
				t.Errorf("vmServiceDisabled() = %v, want %v", got, testCase.want)
			}
		})
	}
}
