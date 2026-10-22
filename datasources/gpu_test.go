package datasources

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dkaser/unraid-motd/utils"
)

const (
	testIDA = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testIDB = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
)

func gv(val int) gpuValue {
	return gpuValue{Val: val, OK: true}
}

type parseGPUCase struct {
	name string
	in   string
	want []gpuInfo
}

var parseGPUCases = []parseGPUCase{
	{
		name: "normal line",
		in:   "0, GPU-aaa, NVIDIA GeForce RTX 3080, 45, 12, 1024, 10240\n",
		want: []gpuInfo{{"0", "GPU-aaa", "NVIDIA GeForce RTX 3080", gv(45), gv(12), gv(1024), gv(10240)}},
	},
	{
		name: "unknown values",
		in:   "1, GPU-bbb, Tesla T4, [N/A], [Not Supported], [N/A], 15360\n",
		want: []gpuInfo{{"1", "GPU-bbb", "Tesla T4", gpuValue{}, gpuValue{}, gpuValue{}, gv(15360)}},
	},
	{
		name: "blank lines and CRLF",
		in:   "\n0, GPU-aaa, Card, 40, 0, 1, 2\r\n\n  \n",
		want: []gpuInfo{{"0", "GPU-aaa", "Card", gv(40), gv(0), gv(1), gv(2)}},
	},
	{
		name: "name containing a comma",
		in:   "0, GPU-aaa, NVIDIA, Foo Edition, 50, 5, 6, 7\n",
		want: []gpuInfo{{"0", "GPU-aaa", "NVIDIA, Foo Edition", gv(50), gv(5), gv(6), gv(7)}},
	},
	{
		name: "malformed line is skipped",
		in:   "0, GPU-aaa, Card, 40\n1, GPU-bbb, Card2, 41, 1, 2, 3\n",
		want: []gpuInfo{{"1", "GPU-bbb", "Card2", gv(41), gv(1), gv(2), gv(3)}},
	},
	{name: "empty", in: "", want: nil},
}

func TestParseGPUCSV(t *testing.T) {
	t.Parallel()

	for _, tc := range parseGPUCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := parseGPUCSV(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseGPUCSV() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

type parseProcCase struct {
	name string
	in   string
	want []gpuProcess
}

var parseProcCases = []parseProcCase{
	{
		name: "path name",
		in:   "GPU-aaa, 1234, /usr/bin/python3, 512\n",
		want: []gpuProcess{{UUID: "GPU-aaa", PID: 1234, Name: "/usr/bin/python3", Mem: gv(512)}},
	},
	{
		name: "unknown memory",
		in:   "GPU-aaa, 77, ffmpeg, [N/A]\n\n",
		want: []gpuProcess{{UUID: "GPU-aaa", PID: 77, Name: "ffmpeg"}},
	},
	{
		name: "name containing a comma",
		in:   "GPU-aaa, 5, /opt/a, b/app, 10\n",
		want: []gpuProcess{{UUID: "GPU-aaa", PID: 5, Name: "/opt/a, b/app", Mem: gv(10)}},
	},
	{name: "malformed and non-numeric pid", in: "GPU-aaa, 1, x\nGPU-aaa, abc, x, 5\n", want: nil},
	{name: "empty", in: "", want: nil},
}

func TestParseProcessCSV(t *testing.T) {
	t.Parallel()

	for _, tc := range parseProcCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := parseProcessCSV(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseProcessCSV() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestContainerIDFromCgroup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"v2 plain", "0::/docker/" + testIDA + "\n", testIDA},
		{"v2 scope", "0::/system.slice/docker-" + testIDA + ".scope\n", testIDA},
		{
			"v1 multi-line",
			"12:cpu:/docker/" + testIDB + "\n11:memory:/docker/" + testIDB + "\n1:name=systemd:/docker/" + testIDB + "\n",
			testIDB,
		},
		{"root cgroup", "0::/\n", ""},
		{"service", "0::/system.slice/sshd.service\n", ""},
		{"empty", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := containerIDFromCgroup(tc.in); got != tc.want {
				t.Errorf("containerIDFromCgroup() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLabelProcesses(t *testing.T) {
	t.Parallel()

	procs := []gpuProcess{{PID: 1}, {PID: 2}, {PID: 3}}
	cgroups := map[int]string{
		1: "0::/docker/" + testIDA + "\n",
		2: "0::/\n",
	}
	names := map[string]string{testIDA: "plex"}

	labelProcesses(procs, func(pid int) string { return cgroups[pid] }, names)

	want := []string{"plex", "", ""}
	for i, p := range procs {
		if p.Label != want[i] {
			t.Errorf("pid %d label = %q, want %q", p.PID, p.Label, want[i])
		}
	}
}

func newTestGPUConf(warnOnly bool, showProcs bool) *ConfGPU {
	conf := &ConfGPU{}
	conf.Init()
	global := &Conf{}
	global.Init()
	conf.Load(global)
	conf.WarnOnly = &warnOnly
	conf.ShowProcesses = showProcs

	return conf
}

func testGPU(index string, temp int) gpuInfo {
	return gpuInfo{
		Index: index, UUID: "GPU-" + index, Name: "NVIDIA Card " + index,
		Temp: gv(temp), Util: gv(7), MemUsed: gv(1024), MemTotal: gv(2048),
	}
}

type formatGPUCase struct {
	name     string
	gpus     []gpuInfo
	warnOnly bool
	showProc bool
	contains []string
	absent   []string
}

var formatGPUProcs = []gpuProcess{
	{UUID: "GPU-0", PID: 1, Name: "/usr/bin/python3", Mem: gv(512)},
	{UUID: "GPU-0", PID: 2, Name: "/x/ffmpeg", Mem: gpuValue{}, Label: "jellyfin"},
}

var formatGPUCases = []formatGPUCase{
	{
		name: "warnings only, all cool", gpus: []gpuInfo{testGPU("0", 40)},
		warnOnly: true, showProc: true,
		contains: []string{"GPU: OK"}, absent: []string{"Card 0", "python3"},
	},
	{
		name: "warn", gpus: []gpuInfo{testGPU("0", 85), testGPU("1", 40)},
		warnOnly: true, showProc: true,
		contains: []string{"GPU: Warning", "GPU0 Card 0", "85°C, 7%, 1024.00 MB / 2.00 GB"},
		absent:   []string{"Card 1", "Critical"},
	},
	{
		name: "crit", gpus: []gpuInfo{testGPU("0", 95), testGPU("1", 85)},
		warnOnly: true, showProc: true,
		contains: []string{"GPU: Critical", "GPU0 Card 0", "GPU1 Card 1"},
	},
	{
		name: "warnings only off shows all", gpus: []gpuInfo{testGPU("0", 40), testGPU("1", 41)},
		warnOnly: false, showProc: true,
		contains: []string{"GPU: OK", "GPU0 Card 0", "GPU1 Card 1"},
	},
	{
		name: "show processes off", gpus: []gpuInfo{testGPU("0", 40)},
		warnOnly: false, showProc: false,
		contains: []string{"GPU0 Card 0"}, absent: []string{"python3", "jellyfin"},
	},
	{
		name: "container name and basename fallback", gpus: []gpuInfo{testGPU("0", 40)},
		warnOnly: false, showProc: true,
		contains: []string{"  python3", "512.00 MB", "  jellyfin", "n/a"}, absent: []string{"/usr/bin", "ffmpeg"},
	},
	{
		name:     "unknown values never warn",
		gpus:     []gpuInfo{{Index: "0", UUID: "GPU-0", Name: "Card 0"}},
		warnOnly: true, showProc: true,
		contains: []string{"GPU: OK"}, absent: []string{"Card 0"},
	},
	{
		name: "unknown values shown", gpus: []gpuInfo{{Index: "0", UUID: "GPU-0", Name: "Card 0"}},
		warnOnly: false, showProc: false,
		contains: []string{"GPU0 Card 0", "n/a / n/a"},
	},
	{name: "no gpu", gpus: nil, warnOnly: true, showProc: true, contains: []string{"GPU: No GPU found"}},
}

func TestFormatGPUs(t *testing.T) {
	utils.NoColors = true
	defer func() { utils.NoColors = false }()

	for _, testCase := range formatGPUCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := formatGPUs(testCase.gpus, formatGPUProcs, newTestGPUConf(testCase.warnOnly, testCase.showProc))

			for _, want := range testCase.contains {
				if !strings.Contains(got, want) {
					t.Errorf("output lacks %q:\n%s", want, got)
				}
			}

			for _, bad := range testCase.absent {
				if strings.Contains(got, bad) {
					t.Errorf("output unexpectedly contains %q:\n%s", bad, got)
				}
			}
		})
	}
}

func TestShortGPUName(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"NVIDIA GeForce RTX 3060": "RTX 3060",
		"NVIDIA RTX A2000":        "RTX A2000",
		"Tesla P4":                "Tesla P4",
	}

	for in, want := range tests {
		if got := shortGPUName(in); got != want {
			t.Errorf("shortGPUName(%q) = %q, want %q", in, got, want)
		}
	}
}
