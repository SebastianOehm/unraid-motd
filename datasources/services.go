package datasources

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dkaser/unraid-motd/utils"
	"gopkg.in/ini.v1"
)

const (
	servicesCfgDir = "/boot/config"
	servicesRcDir  = "/etc/rc.d"
)

// serviceNameUnsafe matches every character that is not allowed in a service name
var serviceNameUnsafe = regexp.MustCompile(`[^a-zA-Z0-9\-_]+`)

type ConfServices struct {
	ConfBase `yaml:",inline"`
	Services []string `yaml:"monitor,flow"`
}

// Init is mandatory
func (c *ConfServices) Init() {
	// Base init must be called
	c.ConfBase.Init()
	c.Services = []string{
		"nginx",
		"samba",
		"tailscale",
		"nfsd",
		"sshd",
		"docker",
	}
}

func GetServices(channel chan<- SourceReturn, conf *Conf) {
	sourceConf := conf.Services
	sourceConf.Load(conf)

	returnData := NewSourceReturn(conf.debug)
	defer func() {
		channel <- returnData.Return()
	}()
	returnData.Content = getServiceStatus(&sourceConf, servicesCfgDir, servicesRcDir)
}

// serviceDisabled reports whether the key in the cfg file is set to "no".
// A cfg file that cannot be loaded counts as disabled.
func serviceDisabled(path, key string) bool {
	cfg, err := ini.Load(path)
	if err != nil {
		return true
	}

	return strings.ToLower(cfg.Section("").Key(key).String()) == "no"
}

// disabledServices returns for the services that can be switched off in the
// Unraid settings whether they are disabled
func disabledServices(cfgDir string) map[string]bool {
	shareCfg := filepath.Join(cfgDir, "share.cfg")

	return map[string]bool{
		"sshd":   serviceDisabled(filepath.Join(cfgDir, "ident.cfg"), "USE_SSH"),
		"samba":  serviceDisabled(shareCfg, "shareSMBEnabled"),
		"nfsd":   serviceDisabled(shareCfg, "shareNFSEnabled"),
		"docker": serviceDisabled(filepath.Join(cfgDir, "docker.cfg"), "DOCKER_ENABLED"),
	}
}

// sanitizeServiceName removes everything but letters, digits, "-" and "_"
func sanitizeServiceName(name string) string {
	return serviceNameUnsafe.ReplaceAllString(name, "")
}

func getServiceStatus(sourceConf *ConfServices, cfgDir, rcDir string) (content string) {
	outputTable := GetTableWriter(sourceConf)

	overall := utils.Good("OK")
	disabled := disabledServices(cfgDir)

	for _, service := range sourceConf.Services {
		service = sanitizeServiceName(service)

		if disabled[service] {
			continue
		}

		cmd := exec.Command(filepath.Join(rcDir, "rc."+service), "status") // #nosec G204
		err := cmd.Run()

		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			overall = utils.Err("Critical")
			outputTable.AppendRow([]interface{}{service, utils.Err("Not running")})
		} else if !*sourceConf.WarnOnly {
			outputTable.AppendRow([]interface{}{service, utils.Good("Running")})
		}
	}

	content = RenderTable(outputTable, "Services: "+overall)

	return
}
