package datasources

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/SebastianOehm/unraid-motd/utils"
)

const (
	testOSVersion      = "7.2.0"
	testOSLatest       = "7.2.1"
	testLabelOS        = "Unraid OS"
	testLabelPlugins   = "Plugins"
	testLabelDocker    = "Docker"
	testPluginName     = "my.plugin"
	testPluginFile     = "my.plugin.plg"
	testImageSonarr    = "linuxserver/sonarr"
	testImageSonarrKey = "linuxserver/sonarr:latest"
	testDigestLocal    = "sha256:aaaa"
	testDigestRemote   = "sha256:bbbb"
	testStatusTrue     = "true"
	testStatusUndef    = "undef"
)

var errTestList = errors.New("docker not reachable")

func upd(name, installed, latest string) pendingUpdate {
	return pendingUpdate{Name: name, Installed: installed, Latest: latest}
}

// assertCategory compares a category with the expectation. nil and empty update
// slices are treated as equal.
func assertCategory(t *testing.T, got updateCategory, wantLabel string, wantChecked bool, want []pendingUpdate) {
	t.Helper()

	if got.Label != wantLabel {
		t.Errorf("Label = %q, want %q", got.Label, wantLabel)
	}

	if got.Checked != wantChecked {
		t.Errorf("Checked = %v, want %v", got.Checked, wantChecked)
	}

	if len(got.Updates) == 0 && len(want) == 0 {
		return
	}

	if !reflect.DeepEqual(got.Updates, want) {
		t.Errorf("Updates = %+v, want %+v", got.Updates, want)
	}
}

// useNoColors disables colors for the duration of the test so output can be
// asserted on plain strings.
func useNoColors(t *testing.T) {
	t.Helper()

	old := utils.NoColors
	utils.NoColors = true

	t.Cleanup(func() { utils.NoColors = old })
}

// plgDoc builds a minimal plg file; empty attributes are omitted.
func plgDoc(name, version, minVer, maxVer string) string {
	var attrs strings.Builder

	for _, kv := range [][2]string{{"name", name}, {"version", version}, {"min", minVer}, {"max", maxVer}} {
		if kv[1] != "" {
			attrs.WriteString(" " + kv[0] + `="` + kv[1] + `"`)
		}
	}

	return "<?xml version='1.0' standalone='yes'?>\n<PLUGIN" + attrs.String() + ">\n<CHANGES>\n- x\n</CHANGES>\n</PLUGIN>\n"
}

// ---------------------------------------------------------------- checkOSUpdate

type osUpdateCase struct {
	name        string
	result      string
	noResult    bool
	ignored     string
	noIgnored   bool
	wantChecked bool
	want        []pendingUpdate
}

var osUpdateCases = []osUpdateCase{
	{
		name:        "newer version available",
		result:      `{"version":"7.2.1","isNewer":true,"changelog":"x"}`,
		noIgnored:   true,
		wantChecked: true,
		want:        []pendingUpdate{upd(testLabelOS, testOSVersion, testOSLatest)},
	},
	{
		name:        "not newer",
		result:      `{"version":"7.2.1","isNewer":false}`,
		noIgnored:   true,
		wantChecked: true,
	},
	{
		name:        "isNewer missing counts as not newer",
		result:      `{"version":"7.2.1"}`,
		noIgnored:   true,
		wantChecked: true,
	},
	{
		name:        "version ignored",
		result:      `{"version":"7.2.1","isNewer":true}`,
		ignored:     `{"updateOsIgnoredReleases":["7.1.0","7.2.1"]}`,
		wantChecked: true,
	},
	{
		name:        "other version ignored",
		result:      `{"version":"7.2.1","isNewer":true}`,
		ignored:     `{"updateOsIgnoredReleases":["7.1.0"]}`,
		wantChecked: true,
		want:        []pendingUpdate{upd(testLabelOS, testOSVersion, testOSLatest)},
	},
	{
		name:        "invalid ignored file means nothing ignored",
		result:      `{"version":"7.2.1","isNewer":true}`,
		ignored:     `not json`,
		wantChecked: true,
		want:        []pendingUpdate{upd(testLabelOS, testOSVersion, testOSLatest)},
	},
	{
		name:     "missing result file",
		noResult: true,
		ignored:  `{"updateOsIgnoredReleases":[]}`,
	},
	{
		name:      "invalid result json",
		result:    `{"version":`,
		noIgnored: true,
	},
	{
		name:      "error field set",
		result:    `{"version":"7.2.1","isNewer":true,"error":"could not reach server"}`,
		noIgnored: true,
	},
}

func TestCheckOSUpdate(t *testing.T) {
	for _, testCase := range osUpdateCases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			resultPath := filepath.Join(dir, "result.json")
			ignoredPath := filepath.Join(dir, "ignored.json")

			if !testCase.noResult {
				resultPath = writeTempFile(t, dir, "result.json", testCase.result)
			}

			if !testCase.noIgnored {
				ignoredPath = writeTempFile(t, dir, "ignored.json", testCase.ignored)
			}

			got := checkOSUpdate(resultPath, ignoredPath, testOSVersion)
			assertCategory(t, got, testLabelOS, testCase.wantChecked, testCase.want)
		})
	}
}

// ---------------------------------------------------------------- readPlgInfo

const realisticPlg = `<?xml version='1.0' standalone='yes'?>
<!DOCTYPE PLUGIN [
<!ENTITY name      "unassigned.devices">
<!ENTITY author    "dlandon">
<!ENTITY version   "2025.01.15">
<!ENTITY launch    "Main/UnassignedDevices">
<!ENTITY pluginURL "https://raw.githubusercontent.com/&author;/&name;/master/&name;.plg">
]>
<PLUGIN name="&name;" author="&author;" version="&version;" launch="&launch;" pluginURL="&pluginURL;" min="6.12.0">
<CHANGES>
##&name;
###&version;
- Fix
</CHANGES>
<FILE Run="/bin/bash"><INLINE><![CDATA[ if [ 1 -lt 2 ] && true; then echo "<ok>"; fi ]]></INLINE></FILE>
</PLUGIN>
`

const nestedEntityPlg = `<?xml version='1.0' standalone='yes'?>
<!DOCTYPE PLUGIN [
<!ENTITY base    "unassigned">
<!ENTITY name    "&base;.devices">
<!ENTITY year    "2025">
<!ENTITY version "&year;.02.03">
<!ENTITY pluginURL "https://x/&name;.plg">
]>
<PLUGIN name="&name;" version="&version;" pluginURL="&pluginURL;" min="6.12.0" max="7.9.9">
</PLUGIN>
`

type plgInfoCase struct {
	name    string
	content string
	want    plgInfo
	wantErr bool
}

var plgInfoCases = []plgInfoCase{
	{
		name:    "realistic plg with DTD entities and CDATA body",
		content: realisticPlg,
		want:    plgInfo{Name: "unassigned.devices", Version: "2025.01.15", Min: "6.12.0"},
	},
	{
		name:    "realistic plg with CRLF line endings",
		content: strings.ReplaceAll(realisticPlg, "\n", "\r\n"),
		want:    plgInfo{Name: "unassigned.devices", Version: "2025.01.15", Min: "6.12.0"},
	},
	{
		name:    "nested entities",
		content: nestedEntityPlg,
		want:    plgInfo{Name: "unassigned.devices", Version: "2025.02.03", Min: "6.12.0", Max: "7.9.9"},
	},
	{
		name:    "literal attributes without DOCTYPE",
		content: `<?xml version='1.0'?><PLUGIN name="foo" version="1.2.3" min="6.0.0" max="7.0.0"></PLUGIN>`,
		want:    plgInfo{Name: "foo", Version: "1.2.3", Min: "6.0.0", Max: "7.0.0"},
	},
	{
		name:    "values are trimmed",
		content: `<PLUGIN name="  foo " version=" 1.0 " min=" 6.1.0" max="7.0.0 "/>`,
		want:    plgInfo{Name: "foo", Version: "1.0", Min: "6.1.0", Max: "7.0.0"},
	},
	{
		name:    "missing attributes stay empty",
		content: `<PLUGIN version="1"/>`,
		want:    plgInfo{Version: "1"},
	},
	{
		name:    "no PLUGIN element",
		content: `<?xml version='1.0'?><OTHER name="x" version="1"/>`,
		wantErr: true,
	},
	{
		name:    "empty file",
		content: "",
		wantErr: true,
	},
	{
		name:    "not xml at all",
		content: "just some text",
		wantErr: true,
	},
}

func TestReadPlgInfo(t *testing.T) {
	dir := t.TempDir()

	for _, testCase := range plgInfoCases {
		t.Run(testCase.name, func(t *testing.T) {
			path := writeTempFile(t, dir, "test.plg", testCase.content)

			got, err := readPlgInfo(path)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("readPlgInfo() = %+v, want error", got)
				}

				return
			}

			if err != nil {
				t.Fatalf("readPlgInfo() error = %v", err)
			}

			if got != testCase.want {
				t.Errorf("readPlgInfo() = %+v, want %+v", got, testCase.want)
			}
		})
	}
}

func TestReadPlgInfoMissingFile(t *testing.T) {
	if _, err := readPlgInfo(filepath.Join(t.TempDir(), "missing.plg")); err == nil {
		t.Error("readPlgInfo() on a missing file: want error, got nil")
	}
}

// ---------------------------------------------------------------- phpVersionCompare

type phpVersionCase struct {
	name string
	left string
	rite string
	want int
}

var phpVersionCases = []phpVersionCase{
	{"equal", "6.12.0", "6.12.0", 0},
	{"numeric not lexical", "6.12.10", "6.12.9", 1},
	{"major wins", "7.0.0", "6.12.15", 1},
	{"beta before release", "7.2.0-beta1", "7.2.0", -1},
	{"rc after beta with dots", "7.2.0-rc.1", "7.2.0-beta.2", 1},
	{"rc before release", "7.2.0-rc1", "7.2.0", -1},
	{"shorter before longer", "1.0", "1.0.1", -1},
	{"patch level after release", "1.0pl1", "1.0", 1},
	{"dev before alpha", "1.0-dev", "1.0-alpha", -1},
	{"a equals alpha", "1.0a", "1.0alpha", 0},
	{"extra numeric component", "7.1.4", "7.1.4.1", -1},
	{"both empty", "", "", 0},
	{"older minor", "6.11.5", "6.12.0", -1},
}

func TestPhpVersionCompare(t *testing.T) {
	for _, testCase := range phpVersionCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := phpVersionCompare(testCase.left, testCase.rite)
			if got != testCase.want {
				t.Errorf("phpVersionCompare(%q, %q) = %d, want %d", testCase.left, testCase.rite, got, testCase.want)
			}
		})
	}
}

func TestPhpVersionCompareAntisymmetric(t *testing.T) {
	for _, testCase := range phpVersionCases {
		t.Run(testCase.name, func(t *testing.T) {
			forward := phpVersionCompare(testCase.left, testCase.rite)
			backward := phpVersionCompare(testCase.rite, testCase.left)

			if forward != -backward {
				t.Errorf("compare(%q, %q) = %d but compare(%q, %q) = %d",
					testCase.left, testCase.rite, forward, testCase.rite, testCase.left, backward)
			}
		})
	}
}

// ---------------------------------------------------------------- checkPluginUpdates

// The plugin directories on Unraid contain symlinks. Plain files are used here
// because symlinks do not work reliably on Windows; the code only reads them.

type pluginUpdateCase struct {
	name        string
	installed   map[string]string
	latest      map[string]string
	osVersion   string
	wantChecked bool
	want        []pendingUpdate
}

var pluginUpdateCases = []pluginUpdateCase{
	{
		name:        "newer latest is an update",
		installed:   map[string]string{testPluginFile: plgDoc(testPluginName, "2025.01.01", "", "")},
		latest:      map[string]string{testPluginFile: plgDoc(testPluginName, "2025.02.01", "", "")},
		osVersion:   testOSVersion,
		wantChecked: true,
		want:        []pendingUpdate{upd(testPluginName, "2025.01.01", "2025.02.01")},
	},
	{
		name:        "equal versions",
		installed:   map[string]string{testPluginFile: plgDoc(testPluginName, "2025.01.01", "", "")},
		latest:      map[string]string{testPluginFile: plgDoc(testPluginName, "2025.01.01", "", "")},
		osVersion:   testOSVersion,
		wantChecked: true,
	},
	{
		name:        "older latest",
		installed:   map[string]string{testPluginFile: plgDoc(testPluginName, "2025.02.01", "", "")},
		latest:      map[string]string{testPluginFile: plgDoc(testPluginName, "2025.01.01", "", "")},
		osVersion:   testOSVersion,
		wantChecked: true,
	},
	{
		name:        "latest min above OS version is incompatible",
		installed:   map[string]string{testPluginFile: plgDoc(testPluginName, "1", "", "")},
		latest:      map[string]string{testPluginFile: plgDoc(testPluginName, "2", "7.3.0", "")},
		osVersion:   testOSVersion,
		wantChecked: true,
	},
	{
		name:        "latest max below OS version is incompatible",
		installed:   map[string]string{testPluginFile: plgDoc(testPluginName, "1", "", "")},
		latest:      map[string]string{testPluginFile: plgDoc(testPluginName, "2", "", "7.1.9")},
		osVersion:   testOSVersion,
		wantChecked: true,
	},
	{
		name:        "OS version equal to min and max is compatible",
		installed:   map[string]string{testPluginFile: plgDoc(testPluginName, "1", "", "")},
		latest:      map[string]string{testPluginFile: plgDoc(testPluginName, "2", testOSVersion, testOSVersion)},
		osVersion:   testOSVersion,
		wantChecked: true,
		want:        []pendingUpdate{upd(testPluginName, "1", "2")},
	},
	{
		name:        "OS version within min and max",
		installed:   map[string]string{testPluginFile: plgDoc(testPluginName, "1", "", "")},
		latest:      map[string]string{testPluginFile: plgDoc(testPluginName, "2", "6.12.0", "7.9.9")},
		osVersion:   testOSVersion,
		wantChecked: true,
		want:        []pendingUpdate{upd(testPluginName, "1", "2")},
	},
	{
		name:        "empty OS version skips the compatibility check",
		installed:   map[string]string{testPluginFile: plgDoc(testPluginName, "1", "", "")},
		latest:      map[string]string{testPluginFile: plgDoc(testPluginName, "2", "99.0.0", "0.1.0")},
		osVersion:   "",
		wantChecked: true,
		want:        []pendingUpdate{upd(testPluginName, "1", "2")},
	},
	{
		name:        "unRAIDServer.plg is skipped",
		installed:   map[string]string{"unRAIDServer.plg": plgDoc("Unraid", "7.2.0", "", "")},
		latest:      map[string]string{"unRAIDServer.plg": plgDoc("Unraid", "7.2.1", "", "")},
		osVersion:   testOSVersion,
		wantChecked: false,
	},
	{
		name: "unRAIDServer- plg is skipped but others are checked",
		installed: map[string]string{
			"unRAIDServer-.plg": plgDoc("Unraid", "7.2.0", "", ""),
			testPluginFile:      plgDoc(testPluginName, "1", "", ""),
		},
		latest: map[string]string{
			"unRAIDServer-.plg": plgDoc("Unraid", "7.2.1", "", ""),
			testPluginFile:      plgDoc(testPluginName, "2", "", ""),
		},
		osVersion:   testOSVersion,
		wantChecked: true,
		want:        []pendingUpdate{upd(testPluginName, "1", "2")},
	},
	{
		name: "installed plugin without latest file is skipped",
		installed: map[string]string{
			"orphan.plg":   plgDoc("orphan", "1", "", ""),
			testPluginFile: plgDoc(testPluginName, "1", "", ""),
		},
		latest:      map[string]string{testPluginFile: plgDoc(testPluginName, "2", "", "")},
		osVersion:   testOSVersion,
		wantChecked: true,
		want:        []pendingUpdate{upd(testPluginName, "1", "2")},
	},
	{
		name:        "plg without name attribute uses the file name",
		installed:   map[string]string{"fallback.name.plg": plgDoc("", "1", "", "")},
		latest:      map[string]string{"fallback.name.plg": plgDoc("", "2", "", "")},
		osVersion:   testOSVersion,
		wantChecked: true,
		want:        []pendingUpdate{upd("fallback.name", "1", "2")},
	},
	{
		name:        "byte-wise compare: 2025.10.10 is not newer than 2025.10.9",
		installed:   map[string]string{testPluginFile: plgDoc(testPluginName, "2025.10.9", "", "")},
		latest:      map[string]string{testPluginFile: plgDoc(testPluginName, "2025.10.10", "", "")},
		osVersion:   testOSVersion,
		wantChecked: true,
	},
	{
		name:        "byte-wise compare: 2025.10.9 counts as newer than 2025.10.10",
		installed:   map[string]string{testPluginFile: plgDoc(testPluginName, "2025.10.10", "", "")},
		latest:      map[string]string{testPluginFile: plgDoc(testPluginName, "2025.10.9", "", "")},
		osVersion:   testOSVersion,
		wantChecked: true,
		want:        []pendingUpdate{upd(testPluginName, "2025.10.10", "2025.10.9")},
	},
	{
		name: "updates are sorted by name",
		installed: map[string]string{
			"a.plg": plgDoc("Zebra", "1", "", ""),
			"b.plg": plgDoc("Alpha", "1", "", ""),
			"c.plg": plgDoc("Mid", "1", "", ""),
		},
		latest: map[string]string{
			"a.plg": plgDoc("Zebra", "2", "", ""),
			"b.plg": plgDoc("Alpha", "2", "", ""),
			"c.plg": plgDoc("Mid", "2", "", ""),
		},
		osVersion:   testOSVersion,
		wantChecked: true,
		want:        []pendingUpdate{upd("Alpha", "1", "2"), upd("Mid", "1", "2"), upd("Zebra", "1", "2")},
	},
	{
		name:        "unparsable latest file is skipped",
		installed:   map[string]string{testPluginFile: plgDoc(testPluginName, "1", "", ""), "bad.plg": plgDoc("bad", "1", "", "")},
		latest:      map[string]string{testPluginFile: plgDoc(testPluginName, "2", "", ""), "bad.plg": "garbage, not xml"},
		osVersion:   testOSVersion,
		wantChecked: true,
		want:        []pendingUpdate{upd(testPluginName, "1", "2")},
	},
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		writeTempFile(t, dir, name, content)
	}
}

func TestCheckPluginUpdates(t *testing.T) {
	for _, testCase := range pluginUpdateCases {
		t.Run(testCase.name, func(t *testing.T) {
			installedDir := t.TempDir()
			latestDir := t.TempDir()
			writeFiles(t, installedDir, testCase.installed)
			writeFiles(t, latestDir, testCase.latest)

			got := checkPluginUpdates(installedDir, latestDir, testCase.osVersion)
			assertCategory(t, got, testLabelPlugins, testCase.wantChecked, testCase.want)
		})
	}
}

func TestCheckPluginUpdatesNoLatestDir(t *testing.T) {
	installedDir := t.TempDir()
	writeTempFile(t, installedDir, testPluginFile, plgDoc(testPluginName, "1", "", ""))

	tests := []struct {
		name      string
		latestDir string
	}{
		{"empty path", ""},
		{"missing directory", filepath.Join(t.TempDir(), "does-not-exist")},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := checkPluginUpdates(installedDir, testCase.latestDir, testOSVersion)
			assertCategory(t, got, testLabelPlugins, false, nil)
		})
	}
}

// ---------------------------------------------------------------- ensureImageTag

func TestEnsureImageTag(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"plex", "library/plex:latest"},
		{"plex:1.2", "library/plex:1.2"},
		{"linuxserver/plex", "linuxserver/plex:latest"},
		{"lscr.io/linuxserver/plex:1.2", "lscr.io/linuxserver/plex:1.2"},
		{"registry:5000/foo/bar", "registry:5000/foo/bar:latest"},
		{"ghcr.io/a/b:v1.0", "ghcr.io/a/b:v1.0"},
		{"sha256:0123456789abcdef0123", "0123456789ab:latest"},
	}

	for _, testCase := range tests {
		t.Run(testCase.in, func(t *testing.T) {
			if got := ensureImageTag(testCase.in); got != testCase.want {
				t.Errorf("ensureImageTag(%q) = %q, want %q", testCase.in, got, testCase.want)
			}
		})
	}
}

// ---------------------------------------------------------------- readDockerUpdateStatus

func TestReadDockerUpdateStatus(t *testing.T) {
	content := `{
		"lscr.io/linuxserver/plex:latest": {"local": "sha256:aaaa", "remote": "sha256:bbbb", "status": "true"},
		"library/redis:latest": {"local": null, "remote": null, "status": "undef"},
		"broken": 5
	}`
	path := writeTempFile(t, t.TempDir(), "update-status.json", content)

	got, err := readDockerUpdateStatus(path)
	if err != nil {
		t.Fatalf("readDockerUpdateStatus() error = %v", err)
	}

	want := map[string]dockerUpdateEntry{
		"lscr.io/linuxserver/plex:latest": {Local: testDigestLocal, Remote: testDigestRemote, Status: testStatusTrue},
		"library/redis:latest":            {Local: "", Remote: "", Status: testStatusUndef},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("readDockerUpdateStatus() = %+v, want %+v", got, want)
	}
}

func TestReadDockerUpdateStatusErrors(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name string
		path string
	}{
		{"missing file", filepath.Join(dir, "missing.json")},
		{"top level array", writeTempFile(t, dir, "array.json", `[]`)},
		{"invalid json", writeTempFile(t, dir, "invalid.json", `{"a":`)},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got, err := readDockerUpdateStatus(testCase.path); err == nil {
				t.Errorf("readDockerUpdateStatus() = %+v, want error", got)
			}
		})
	}
}

// ---------------------------------------------------------------- dockerUpdates

const (
	testRunDigest   = "sha256:cccc"
	testDigestOther = "sha256:dddd"
)

type dockerUpdatesCase struct {
	name       string
	containers []containerImage
	status     map[string]dockerUpdateEntry
	want       []string
}

func dockerStatus(key, local, remote, status string) map[string]dockerUpdateEntry {
	return map[string]dockerUpdateEntry{key: {Local: local, Remote: remote, Status: status}}
}

func sonarrWith(digests ...string) []containerImage {
	return []containerImage{{Name: "sonarr", Image: testImageSonarr, Digests: digests}}
}

var dockerUpdatesCases = []dockerUpdatesCase{
	{
		name:       "local differs from remote and running image matches neither",
		containers: sonarrWith(testRunDigest),
		status:     dockerStatus(testImageSonarrKey, testDigestLocal, testDigestRemote, testStatusTrue),
		want:       []string{"sonarr"},
	},
	{
		name:       "local differs from remote but running image already is remote (stale local after compose pull)",
		containers: sonarrWith(testDigestRemote),
		status:     dockerStatus(testImageSonarrKey, testDigestLocal, testDigestRemote, testStatusTrue),
	},
	{
		name:       "local equals remote while running image is older (cached entry is stale)",
		containers: sonarrWith("sha256:5c02", "sha256:97ec"),
		status:     dockerStatus(testImageSonarrKey, "sha256:8d51", "sha256:8d51", testStatusTrue),
	},
	{
		name:       "local differs from remote and container has no digests",
		containers: sonarrWith(),
		status:     dockerStatus(testImageSonarrKey, testDigestLocal, testDigestRemote, testStatusTrue),
		want:       []string{"sonarr"},
	},
	{
		name:       "local differs from remote, several digests and none is remote",
		containers: sonarrWith(testRunDigest, testDigestOther),
		status:     dockerStatus(testImageSonarrKey, testDigestLocal, testDigestRemote, testStatusTrue),
		want:       []string{"sonarr"},
	},
	{
		name:       "remote matches the second of several digests",
		containers: sonarrWith(testRunDigest, testDigestRemote, testDigestOther),
		status:     dockerStatus(testImageSonarrKey, testDigestLocal, testDigestRemote, testStatusTrue),
	},
	{
		name:       "case and whitespace differences between local and remote are ignored",
		containers: sonarrWith(testRunDigest),
		status:     dockerStatus(testImageSonarrKey, " SHA256:AAA ", "sha256:aaa", testStatusTrue),
	},
	{
		name:       "case and whitespace differences in digests are ignored",
		containers: sonarrWith(" SHA256:ABC "),
		status:     dockerStatus(testImageSonarrKey, testDigestLocal, "sha256:abc", testStatusTrue),
	},
	{
		name:       "remote with surrounding whitespace and capitals matches a digest",
		containers: sonarrWith("sha256:abc"),
		status:     dockerStatus(testImageSonarrKey, testDigestLocal, "  Sha256:ABC\n", testStatusTrue),
	},
	{
		name:       "local undef",
		containers: sonarrWith(testRunDigest),
		status:     dockerStatus(testImageSonarrKey, testStatusUndef, testDigestRemote, testStatusTrue),
	},
	{
		name:       "local UNDEF in capitals",
		containers: sonarrWith(testRunDigest),
		status:     dockerStatus(testImageSonarrKey, "UNDEF", testDigestRemote, testStatusTrue),
	},
	{
		name:       "remote undef",
		containers: sonarrWith(testRunDigest),
		status:     dockerStatus(testImageSonarrKey, testDigestLocal, testStatusUndef, testStatusTrue),
	},
	{
		name:       "empty local",
		containers: sonarrWith(testRunDigest),
		status:     dockerStatus(testImageSonarrKey, "", testDigestRemote, testStatusTrue),
	},
	{
		name:       "empty remote",
		containers: sonarrWith(testRunDigest),
		status:     dockerStatus(testImageSonarrKey, testDigestLocal, "", testStatusTrue),
	},
	{
		name:       "blank remote",
		containers: sonarrWith(testRunDigest),
		status:     dockerStatus(testImageSonarrKey, testDigestLocal, "  ", testStatusTrue),
	},
	{
		name:       "status undef",
		containers: sonarrWith(testRunDigest),
		status:     dockerStatus(testImageSonarrKey, testDigestLocal, testDigestRemote, testStatusUndef),
	},
	{
		name:       "no status entry for the image",
		containers: sonarrWith(testRunDigest),
		status:     dockerStatus("other/image:latest", testDigestLocal, testDigestRemote, testStatusTrue),
	},
	{
		name:       "nil status map",
		containers: sonarrWith(testRunDigest),
		status:     nil,
	},
	{
		name:       "no containers",
		containers: nil,
		status:     dockerStatus(testImageSonarrKey, testDigestLocal, testDigestRemote, testStatusTrue),
	},
	{
		name:       "explicit tag matches normalised key",
		containers: []containerImage{{Name: "plex", Image: "plex:1.2", Digests: []string{testRunDigest}}},
		status:     dockerStatus("library/plex:1.2", testDigestLocal, testDigestRemote, testStatusTrue),
		want:       []string{"plex"},
	},
	{
		name:       "library image without namespace matches normalised key",
		containers: []containerImage{{Name: "cache", Image: "redis", Digests: []string{testRunDigest}}},
		status:     dockerStatus("library/redis:latest", testDigestLocal, testDigestRemote, testStatusTrue),
		want:       []string{"cache"},
	},
	{
		name: "sorted by name and only updated containers",
		containers: []containerImage{
			{Name: "zeta", Image: "a/zeta", Digests: []string{testRunDigest}},
			{Name: "alpha", Image: "a/alpha", Digests: []string{testRunDigest}},
			{Name: "uptodate", Image: "a/uptodate", Digests: []string{testDigestRemote}},
		},
		status: map[string]dockerUpdateEntry{
			"a/zeta:latest":     {Local: testDigestLocal, Remote: testDigestRemote, Status: testStatusTrue},
			"a/alpha:latest":    {Local: testDigestLocal, Remote: testDigestRemote, Status: testStatusTrue},
			"a/uptodate:latest": {Local: testDigestLocal, Remote: testDigestRemote, Status: testStatusTrue},
		},
		want: []string{"alpha", "zeta"},
	},
}

func TestDockerUpdates(t *testing.T) {
	for _, testCase := range dockerUpdatesCases {
		t.Run(testCase.name, func(t *testing.T) {
			var names []string
			for _, update := range dockerUpdates(testCase.containers, testCase.status) {
				names = append(names, update.Name)
			}

			if !slices.Equal(names, testCase.want) {
				t.Errorf("dockerUpdates() names = %v, want %v", names, testCase.want)
			}
		})
	}
}

// ---------------------------------------------------------------- checkDockerUpdates

func TestCheckDockerUpdates(t *testing.T) {
	dir := t.TempDir()
	statusPath := writeTempFile(t, dir, "update-status.json",
		`{"`+testImageSonarrKey+`":{"local":"`+testDigestLocal+`","remote":"`+testDigestRemote+`","status":"true"}}`)
	missingPath := filepath.Join(dir, "missing.json")

	okList := func() ([]containerImage, error) {
		return []containerImage{
			{Name: "sonarr", Image: testImageSonarr, Digests: []string{testRunDigest}},
			{Name: "other", Image: "x/other", Digests: []string{testRunDigest}},
		}, nil
	}
	failList := func() ([]containerImage, error) {
		return nil, errTestList
	}

	// Compose Manager Plus says sonarr is current and other has an update
	composePath := writeTempFile(t, dir, "compose-status.json", `{"version":1,"stacks":{"media":{"containers":[
		{"name":"sonarr","hasUpdate":false,"updateStatus":"up-to-date"},
		{"name":"other","hasUpdate":true,"updateStatus":"update-available"}]}}}`)
	// The plugin could not check sonarr, so Unraid's status applies to it
	composeUnknownPath := writeTempFile(t, dir, "compose-unknown.json",
		`{"stacks":{"media":{"containers":[{"name":"sonarr","hasUpdate":false,"updateStatus":"unknown"}]}}}`)

	tests := []struct {
		name        string
		path        string
		composePath string
		list        func() ([]containerImage, error)
		wantChecked bool
		wantNames   []string
	}{
		{"list error", statusPath, composePath, failList, false, nil},
		{"both status files missing", missingPath, missingPath, okList, false, nil},
		{"unraid status only", statusPath, missingPath, okList, true, []string{"sonarr"}},
		{"compose status only", missingPath, composePath, okList, true, []string{"other"}},
		{"compose result wins over unraid status", statusPath, composePath, okList, true, []string{"other"}},
		{"compose unknown falls back to unraid status", statusPath, composeUnknownPath, okList, true, []string{"sonarr"}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := checkDockerUpdates(testCase.path, testCase.composePath, testCase.list)

			if got.Label != testLabelDocker {
				t.Errorf("Label = %q, want %q", got.Label, testLabelDocker)
			}

			if got.Checked != testCase.wantChecked {
				t.Errorf("Checked = %v, want %v", got.Checked, testCase.wantChecked)
			}

			var names []string
			for _, update := range got.Updates {
				names = append(names, update.Name)
			}

			if !slices.Equal(names, testCase.wantNames) {
				t.Errorf("update names = %v, want %v", names, testCase.wantNames)
			}
		})
	}
}

// ---------------------------------------------------------------- readComposeUpdateStatus

func TestReadComposeUpdateStatus(t *testing.T) {
	dir := t.TempDir()
	// Shape of the file written by Compose Manager Plus, trimmed to a few fields
	path := writeTempFile(t, dir, "update-status.json", `{
    "version": 1,
    "stacks": {
        "empty": {"projectName": "empty", "containers": [], "hasUpdate": false, "lastChecked": 1795394152},
        "fdroid": {
            "projectName": "fdroid",
            "containers": [
                {"name": "fdroid-web", "image": "nginx:stable-alpine", "hasUpdate": false, "updateStatus": "up-to-date"},
                {"name": "fdroid-upload", "image": "python:3.13-alpine", "hasUpdate": true, "updateStatus": "update-available"},
                {"name": "fdroid-build", "image": "local/build", "hasUpdate": false, "updateStatus": "unknown"}
            ]
        },
        "tasks": {"containers": [{"name": "vikunja", "hasUpdate": true, "updateStatus": "update-available"}]}
    }
}`)

	got, err := readComposeUpdateStatus(path)
	if err != nil {
		t.Fatalf("readComposeUpdateStatus() error = %v", err)
	}

	want := map[string]bool{"fdroid-web": false, "fdroid-upload": true, "vikunja": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("readComposeUpdateStatus() = %v, want %v", got, want)
	}
}

func TestReadComposeUpdateStatusErrors(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name string
		path string
	}{
		{"missing file", filepath.Join(dir, "missing.json")},
		{"invalid JSON", writeTempFile(t, dir, "invalid.json", "{not json")},
		{"unexpected shape", writeTempFile(t, dir, "shape.json", `{"stacks":[]}`)},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := readComposeUpdateStatus(testCase.path); err == nil {
				t.Error("readComposeUpdateStatus() error = nil, want an error")
			}
		})
	}
}

// ---------------------------------------------------------------- renderUpdates

func category(label string, checked bool, updates ...pendingUpdate) updateCategory {
	return updateCategory{Label: label, Checked: checked, Updates: updates}
}

type renderUpdatesCase struct {
	name       string
	categories []updateCategory
	ignore     []string
	warnOnly   bool
	contains   []string
	absent     []string
}

var renderUpdatesCases = []renderUpdatesCase{
	{
		name:       "one update shows installed to latest",
		categories: []updateCategory{category(testLabelOS, true, upd(testLabelOS, "7.2.0", testOSLatest))},
		warnOnly:   true,
		contains:   []string{"Updates: Warning", testLabelOS, "7.2.0 → 7.2.1"},
		absent:     []string{"Updates: OK"},
	},
	{
		name:       "unknown installed version shows latest available",
		categories: []updateCategory{category(testLabelPlugins, true, upd(testPluginName, "", testOSLatest))},
		warnOnly:   true,
		contains:   []string{"Updates: Warning", testPluginName, "7.2.1 available"},
	},
	{
		name:       "container update",
		categories: []updateCategory{category(testLabelDocker, true, upd("sonarr", "", ""))},
		warnOnly:   true,
		contains:   []string{"Updates: Warning", "sonarr", "update available"},
	},
	{
		name: "all up to date with warnings only",
		categories: []updateCategory{
			category(testLabelOS, true),
			category(testLabelPlugins, true),
			category(testLabelDocker, true),
		},
		warnOnly: true,
		contains: []string{"Updates: OK"},
		absent:   []string{"up to date", "Warning"},
	},
	{
		name: "all up to date shows rows when not warnings only",
		categories: []updateCategory{
			category(testLabelOS, true),
			category(testLabelPlugins, true),
		},
		warnOnly: false,
		contains: []string{"Updates: OK", testLabelOS, testLabelPlugins, "up to date"},
	},
	{
		name:       "ignore list removes the update and turns the title OK",
		categories: []updateCategory{category(testLabelDocker, true, upd("sonarr", "", ""))},
		ignore:     []string{"sonarr"},
		warnOnly:   true,
		contains:   []string{"Updates: OK"},
		absent:     []string{"sonarr", "Warning", "update available"},
	},
	{
		name: "ignore only hides the named update",
		categories: []updateCategory{category(testLabelDocker, true,
			upd("sonarr", "", ""), upd("radarr", "", ""))},
		ignore:   []string{"sonarr"},
		warnOnly: true,
		contains: []string{"Updates: Warning", "radarr"},
		absent:   []string{"sonarr"},
	},
	{
		name: "unchecked categories add no rows",
		categories: []updateCategory{
			category(testLabelOS, true),
			category(testLabelDocker, false),
		},
		warnOnly: false,
		contains: []string{"Updates: OK", testLabelOS},
		absent:   []string{testLabelDocker},
	},
}

func TestRenderUpdates(t *testing.T) {
	useNoColors(t)

	for _, testCase := range renderUpdatesCases {
		t.Run(testCase.name, func(t *testing.T) {
			content, anyChecked := renderUpdates(testCase.categories, testCase.ignore, testCase.warnOnly, testTableConf())

			if !anyChecked {
				t.Error("anyChecked = false, want true")
			}

			for _, want := range testCase.contains {
				if !strings.Contains(content, want) {
					t.Errorf("content missing %q:\n%s", want, content)
				}
			}

			for _, unwanted := range testCase.absent {
				if strings.Contains(content, unwanted) {
					t.Errorf("content unexpectedly contains %q:\n%s", unwanted, content)
				}
			}
		})
	}
}

func TestRenderUpdatesNothingChecked(t *testing.T) {
	useNoColors(t)

	categories := []updateCategory{
		category(testLabelOS, false),
		category(testLabelPlugins, false),
		category(testLabelDocker, false),
	}

	for _, warnOnly := range []bool{true, false} {
		content, anyChecked := renderUpdates(categories, nil, warnOnly, testTableConf())
		if content != "" || anyChecked {
			t.Errorf("renderUpdates(warnOnly=%v) = (%q, %v), want (\"\", false)", warnOnly, content, anyChecked)
		}
	}

	content, anyChecked := renderUpdates(nil, nil, true, testTableConf())
	if content != "" || anyChecked {
		t.Errorf("renderUpdates(nil) = (%q, %v), want (\"\", false)", content, anyChecked)
	}
}

// ---------------------------------------------------------------- config

func TestConfUpdatesInit(t *testing.T) {
	var conf ConfUpdates
	conf.Init()

	if !conf.CheckOS || !conf.CheckPlugins || !conf.CheckDocker {
		t.Errorf("checks = os:%v plugins:%v docker:%v, want all true", conf.CheckOS, conf.CheckPlugins, conf.CheckDocker)
	}

	if conf.Ignore == nil || len(conf.Ignore) != 0 {
		t.Errorf("Ignore = %#v, want empty non-nil slice", conf.Ignore)
	}
}

func TestConfInitSetsUpdates(t *testing.T) {
	var conf Conf
	conf.Init()

	if !conf.Updates.CheckOS || !conf.Updates.CheckPlugins || !conf.Updates.CheckDocker {
		t.Errorf("Updates checks = os:%v plugins:%v docker:%v, want all true",
			conf.Updates.CheckOS, conf.Updates.CheckPlugins, conf.Updates.CheckDocker)
	}

	for _, column := range conf.ColDef {
		if slices.Contains(column, "updates") {
			t.Errorf("default ColDef %v contains \"updates\", want it opt-in", conf.ColDef)
		}
	}
}

// ---------------------------------------------------------------- readUnraidVersion

func TestReadUnraidVersion(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
		wantErr bool
	}{
		{name: "quoted version", content: "version=\"7.2.0\"\n", want: "7.2.0"},
		{name: "unquoted version", content: "version=6.12.13\n", want: "6.12.13"},
		{name: "version after other lines", content: "# comment\nbuild=2025\nversion=\"7.0.1\"\n", want: "7.0.1"},
		{name: "no version line", content: "build=2025\nname=unraid\n", wantErr: true},
		{name: "empty quoted version", content: "version=\"\"\n", wantErr: true},
		{name: "empty file", content: "", wantErr: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			path := writeTempFile(t, t.TempDir(), "unraid-version", testCase.content)

			got, err := readUnraidVersion(path)
			if testCase.wantErr {
				if err == nil {
					t.Errorf("readUnraidVersion() = %q, want error", got)
				}

				return
			}

			if err != nil {
				t.Fatalf("readUnraidVersion() error = %v", err)
			}

			if got != testCase.want {
				t.Errorf("readUnraidVersion() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestReadUnraidVersionMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")

	if got, err := readUnraidVersion(path); err == nil {
		t.Errorf("readUnraidVersion() = %q, want error", got)
	}

	if _, err := os.Stat(path); err == nil {
		t.Fatal("file unexpectedly exists")
	}
}
