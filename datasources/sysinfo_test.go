package datasources

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dkaser/unraid-motd/utils"
)

const meminfoNoSwap = "MemTotal:       32840228 kB\n" +
	"MemFree:         1203456 kB\n" +
	"MemAvailable:   24531200 kB\n" +
	"Buffers:            4096 kB\n" +
	"Cached:         20115480 kB\n" +
	"SwapCached:            0 kB\n" +
	"SwapTotal:             0 kB\n" +
	"SwapFree:              0 kB\n" +
	"Dirty:               128 kB\n"

const meminfoWithSwap = "MemTotal:       16384000 kB\n" +
	"MemFree:          512000 kB\n" +
	"MemAvailable:    8192000 kB\n" +
	"SwapTotal:       4194304 kB\n" +
	"SwapFree:        3145728 kB\n"

func TestParseMeminfo(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    meminfo
		wantErr bool
	}{
		{
			name: "without swap",
			in:   meminfoNoSwap,
			want: meminfo{MemTotal: 32840228, MemAvailable: 24531200},
		},
		{
			name: "with swap",
			in:   meminfoWithSwap,
			want: meminfo{MemTotal: 16384000, MemAvailable: 8192000, SwapTotal: 4194304, SwapFree: 3145728},
		},
		{
			name:    "missing MemAvailable",
			in:      "MemTotal: 1000 kB\nSwapTotal: 0 kB\n",
			wantErr: true,
		},
		{
			name:    "missing MemTotal",
			in:      "MemAvailable: 1000 kB\n",
			wantErr: true,
		},
		{
			name:    "non numeric value",
			in:      "MemTotal: lots kB\nMemAvailable: 1000 kB\n",
			wantErr: true,
		},
		{
			name:    "empty",
			in:      "",
			wantErr: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := parseMeminfo(strings.NewReader(testCase.in))
			if (err != nil) != testCase.wantErr {
				t.Fatalf("parseMeminfo() error = %v, wantErr %v", err, testCase.wantErr)
			}
			if got != testCase.want {
				t.Errorf("parseMeminfo() = %+v, want %+v", got, testCase.want)
			}
		})
	}
}

func TestParseLoadavg(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantStrs [loadFields]string
		wantVals [loadFields]float64
		wantErr  bool
	}{
		{
			name:     "normal",
			in:       "0.52 0.48 0.40 1/234 5678\n",
			wantStrs: [loadFields]string{"0.52", "0.48", "0.40"},
			wantVals: [loadFields]float64{0.52, 0.48, 0.40},
		},
		{name: "empty", in: "", wantErr: true},
		{name: "two fields", in: "0.52 0.48\n", wantErr: true},
		{name: "non numeric", in: "abc 0.48 0.40 1/234 5678\n", wantErr: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			gotStrs, gotVals, err := parseLoadavg(testCase.in)
			if (err != nil) != testCase.wantErr {
				t.Fatalf("parseLoadavg() error = %v, wantErr %v", err, testCase.wantErr)
			}
			if err != nil {
				if !errors.Is(err, errLoadavgMalformed) {
					t.Errorf("parseLoadavg() error = %v, want errLoadavgMalformed", err)
				}

				return
			}
			if gotStrs != testCase.wantStrs {
				t.Errorf("parseLoadavg() strings = %v, want %v", gotStrs, testCase.wantStrs)
			}
			if gotVals != testCase.wantVals {
				t.Errorf("parseLoadavg() values = %v, want %v", gotVals, testCase.wantVals)
			}
		})
	}
}

func TestThresholdLevel(t *testing.T) {
	tests := []struct {
		name  string
		value int
		warn  int
		crit  int
		want  sysLevel
	}{
		{"below warn", 89, 90, 95, sysOK},
		{"at warn", 90, 90, 95, sysWarning},
		{"between warn and crit", 94, 90, 95, sysWarning},
		{"at crit", 95, 90, 95, sysCritical},
		{"above crit", 100, 90, 95, sysCritical},
		{"warn disabled", 92, 0, 95, sysOK},
		{"warn disabled, crit hit", 96, 0, 95, sysCritical},
		{"crit disabled", 99, 90, 0, sysWarning},
		{"both disabled", 100, 0, 0, sysOK},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := thresholdLevel(testCase.value, testCase.warn, testCase.crit); got != testCase.want {
				t.Errorf("thresholdLevel(%d, %d, %d) = %v, want %v", testCase.value, testCase.warn, testCase.crit, got, testCase.want)
			}
		})
	}
}

func TestFormatLoad(t *testing.T) {
	strs := [loadFields]string{"4.00", "8.00", "1.00"}
	vals := [loadFields]float64{4, 8, 1}

	tests := []struct {
		name string
		cpus int
		warn int
		crit int
		want string
	}{
		{
			name: "mixed levels on 4 cpus",
			cpus: 4,
			warn: 100,
			crit: 200,
			want: utils.Warn("4.00") + " [1m], " + utils.Err("8.00") + " [5m], 1.00 [15m]",
		},
		{
			name: "all below on 16 cpus",
			cpus: 16,
			warn: 100,
			crit: 200,
			want: "4.00 [1m], 8.00 [5m], 1.00 [15m]",
		},
		{
			name: "thresholds disabled",
			cpus: 4,
			warn: 0,
			crit: 0,
			want: "4.00 [1m], 8.00 [5m], 1.00 [15m]",
		},
		{
			name: "zero cpus treated as one",
			cpus: 0,
			warn: 100,
			crit: 200,
			want: utils.Err("4.00") + " [1m], " + utils.Err("8.00") + " [5m], " + utils.Warn("1.00") + " [15m]",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := formatLoad(strs, vals, testCase.cpus, testCase.warn, testCase.crit); got != testCase.want {
				t.Errorf("formatLoad() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestFormatMemory(t *testing.T) {
	const total = 2097152 // 2 GiB in kB

	tests := []struct {
		name string
		used float64
		warn int
		crit int
		text string
		lvl  sysLevel
	}{
		{"plain", total / 2, 90, 95, "50% used (1024.00 MB of 2.00 GB)", sysOK},
		{"warn", total * 0.92, 90, 95, "92% used (1.84 GB of 2.00 GB)", sysWarning},
		{"crit", total * 0.96, 90, 95, "96% used (1.92 GB of 2.00 GB)", sysCritical},
		{"disabled", total * 0.99, 0, 0, "99% used (1.98 GB of 2.00 GB)", sysOK},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			want := testCase.text
			switch testCase.lvl {
			case sysWarning:
				want = utils.Warn(testCase.text)
			case sysCritical:
				want = utils.Err(testCase.text)
			case sysOK:
			}
			if got := formatMemory(testCase.used, total, testCase.warn, testCase.crit); got != want {
				t.Errorf("formatMemory() = %q, want %q", got, want)
			}
		})
	}
}

func TestSysInfoConfDefaults(t *testing.T) {
	var conf Conf
	conf.Init()

	tests := []struct {
		name string
		got  int
		want int
	}{
		{"ram_warn", conf.SysInfo.RAMWarn, 90},
		{"ram_crit", conf.SysInfo.RAMCrit, 95},
		{"swap_warn", conf.SysInfo.SwapWarn, 50},
		{"swap_crit", conf.SysInfo.SwapCrit, 80},
		{"load_warn", conf.SysInfo.LoadWarn, 100},
		{"load_crit", conf.SysInfo.LoadCrit, 200},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.got != testCase.want {
				t.Errorf("%s = %d, want %d", testCase.name, testCase.got, testCase.want)
			}
		})
	}
}

func TestReadDistroName(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "quoted version as written by Unraid",
			content: "version=\"7.1.4\"\n",
			want:    "7.1.4",
		},
		{
			name:    "unquoted version",
			content: "version=6.12.13\n",
			want:    "6.12.13",
		},
		{
			name:    "version line after other lines",
			content: "# comment\nbuild=2025\nversion=\"7.0.1\"\n",
			want:    "7.0.1",
		},
		{
			// Current behavior: no version= line yields an empty string, not a warning.
			name:    "no version line",
			content: "build=2025\nname=unraid\n",
			want:    "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			path := writeTempFile(t, t.TempDir(), "unraid-version", testCase.content)

			if got := readDistroName(path); got != testCase.want {
				t.Errorf("readDistroName() = %q, want %q", got, testCase.want)
			}
		})
	}

	t.Run("missing file", func(t *testing.T) {
		got := readDistroName(filepath.Join(t.TempDir(), "does-not-exist"))
		if !strings.Contains(got, "unavailable") {
			t.Errorf("readDistroName() = %q, want it to contain %q", got, "unavailable")
		}
	})
}
