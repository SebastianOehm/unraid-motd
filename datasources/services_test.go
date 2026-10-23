package datasources

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

const (
	servicesIdentCfg  = "NAME=\"Tower\"\nUSE_SSH=\"yes\"\nPORTSSH=\"22\"\nUSE_TELNET=\"no\"\n"
	servicesShareCfg  = "shareDisk=\"yes\"\nshareSMBEnabled=\"yes\"\nshareNFSEnabled=\"no\"\nshareCacheEnabled=\"yes\"\n"
	servicesDockerCfg = "DOCKER_ENABLED=\"yes\"\nDOCKER_IMAGE_SIZE=\"20\"\nDOCKER_TIMEOUT=\"10\"\n"
)

// newServicesConf returns a ready to use services config, as GetServices would build it
func newServicesConf(services []string, warnOnly bool) *ConfServices {
	var conf Conf
	conf.Init()

	sourceConf := conf.Services
	sourceConf.Load(&conf)
	sourceConf.Services = services
	*sourceConf.WarnOnly = warnOnly

	return &sourceConf
}

// writeFakeRC writes an executable fake rc script that exits with the given code
func writeFakeRC(t *testing.T, dir, service, exitCode string) {
	t.Helper()

	script := "#!/bin/sh\nexit " + exitCode + "\n"
	// #nosec G306 -- fake rc script must be executable
	if err := os.WriteFile(filepath.Join(dir, "rc."+service), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestServiceDisabled(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"no", writeTempFile(t, dir, "no.cfg", "USE_SSH=\"no\"\n"), true},
		{"NO uppercase", writeTempFile(t, dir, "upper.cfg", "USE_SSH=\"NO\"\n"), true},
		{"yes", writeTempFile(t, dir, "yes.cfg", "USE_SSH=\"yes\"\n"), false},
		{"unquoted no", writeTempFile(t, dir, "unquoted.cfg", "USE_SSH=no\n"), true},
		{"key missing", writeTempFile(t, dir, "other.cfg", "NAME=\"Tower\"\n"), false},
		{"file missing", filepath.Join(dir, "missing.cfg"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := serviceDisabled(tt.path, "USE_SSH"); got != tt.want {
				t.Errorf("serviceDisabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDisabledServices(t *testing.T) {
	realistic := t.TempDir()
	writeTempFile(t, realistic, "ident.cfg", servicesIdentCfg)
	writeTempFile(t, realistic, "share.cfg", servicesShareCfg)
	writeTempFile(t, realistic, "docker.cfg", servicesDockerCfg)

	switchedOff := t.TempDir()
	writeTempFile(t, switchedOff, "ident.cfg", "USE_SSH=\"no\"\n")
	writeTempFile(t, switchedOff, "share.cfg", "shareSMBEnabled=\"no\"\nshareNFSEnabled=\"yes\"\n")
	writeTempFile(t, switchedOff, "docker.cfg", "DOCKER_ENABLED=\"no\"\n")

	tests := []struct {
		name string
		dir  string
		want map[string]bool
	}{
		{
			name: "realistic config",
			dir:  realistic,
			want: map[string]bool{"sshd": false, "samba": false, "nfsd": true, "docker": false},
		},
		{
			name: "switched off in the opposite way",
			dir:  switchedOff,
			want: map[string]bool{"sshd": true, "samba": true, "nfsd": false, "docker": true},
		},
		{
			name: "no config files",
			dir:  t.TempDir(),
			want: map[string]bool{"sshd": true, "samba": true, "nfsd": true, "docker": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := disabledServices(tt.dir); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("disabledServices() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSanitizeServiceName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "nginx", "nginx"},
		{"command injection", "rc.nginx;rm", "rcnginxrm"},
		{"path traversal", "../ssh d", "sshd"},
		{"dash and underscore", "php-fpm_2", "php-fpm_2"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeServiceName(tt.in); got != tt.want {
				t.Errorf("sanitizeServiceName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

type serviceStatusCase struct {
	name     string
	services []string
	scripts  map[string]string // service -> exit code of the fake rc script
	cfgs     map[string]string // file name -> content, in the cfg dir
	warnOnly bool
	want     []string
	notWant  []string
}

// serviceStatusCases returns the table for TestGetServiceStatus
func serviceStatusCases() []serviceStatusCase {
	return []serviceStatusCase{
		{
			name:     "running service",
			services: []string{"nginx"},
			scripts:  map[string]string{"nginx": "0"},
			want:     []string{"nginx", "Running", "OK"},
			notWant:  []string{"Not running", "Critical"},
		},
		{
			name:     "failing service",
			services: []string{"nginx"},
			scripts:  map[string]string{"nginx": "1"},
			want:     []string{"nginx", "Not running", "Critical"},
			notWant:  []string{"OK"},
		},
		{
			name:     "missing script is skipped",
			services: []string{"nginx"},
			want:     []string{"OK"},
			notWant:  []string{"nginx", "Running", "Critical"},
		},
		{
			name:     "disabled service is skipped even if its script fails",
			services: []string{"samba", "nginx"},
			scripts:  map[string]string{"samba": "1", "nginx": "0"},
			cfgs:     map[string]string{"share.cfg": "shareDisk=\"yes\"\nshareSMBEnabled=\"no\"\n"},
			want:     []string{"nginx", "Running", "OK"},
			notWant:  []string{"samba", "Not running", "Critical"},
		},
		{
			name:     "enabled service is checked",
			services: []string{"samba"},
			scripts:  map[string]string{"samba": "1"},
			cfgs:     map[string]string{"share.cfg": servicesShareCfg},
			want:     []string{"samba", "Not running", "Critical"},
		},
		{
			name:     "missing cfg file disables the service",
			services: []string{"docker"},
			scripts:  map[string]string{"docker": "1"},
			want:     []string{"OK"},
			notWant:  []string{"docker", "Critical"},
		},
		{
			name:     "warnings only hides running services",
			services: []string{"nginx", "tailscale"},
			scripts:  map[string]string{"nginx": "0", "tailscale": "1"},
			warnOnly: true,
			want:     []string{"tailscale", "Not running", "Critical"},
			notWant:  []string{"nginx", "Running"},
		},
		{
			name:     "sanitized name is used for the script",
			services: []string{"../nginx;"},
			scripts:  map[string]string{"nginx": "0"},
			want:     []string{"nginx", "Running", "OK"},
		},
	}
}

// checkServiceStatus runs getServiceStatus against fake cfg and rc dirs and checks the output
func checkServiceStatus(t *testing.T, test serviceStatusCase) {
	t.Helper()

	cfgDir := t.TempDir()
	rcDir := t.TempDir()

	for name, content := range test.cfgs {
		writeTempFile(t, cfgDir, name, content)
	}

	for service, exitCode := range test.scripts {
		writeFakeRC(t, rcDir, service, exitCode)
	}

	got := getServiceStatus(newServicesConf(test.services, test.warnOnly), cfgDir, rcDir)

	for _, s := range test.want {
		if !strings.Contains(got, s) {
			t.Errorf("output does not contain %q:\n%s", s, got)
		}
	}

	for _, s := range test.notWant {
		if strings.Contains(got, s) {
			t.Errorf("output unexpectedly contains %q:\n%s", s, got)
		}
	}
}

func TestGetServiceStatus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake rc scripts require a POSIX system")
	}

	for _, tt := range serviceStatusCases() {
		t.Run(tt.name, func(t *testing.T) {
			checkServiceStatus(t, tt)
		})
	}
}
