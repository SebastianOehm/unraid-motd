package datasources

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"gopkg.in/ini.v1"

	"github.com/dkaser/unraid-motd/utils"
)

// virshTimeout bounds how long a single virsh invocation may run, so a hung
// libvirt daemon cannot stall the whole MOTD.
const virshTimeout = 5 * time.Second

// vmDomainCfg is the Unraid VM manager settings file. Its SERVICE key is
// "disable" when the VM service has been turned off.
const vmDomainCfg = "/boot/config/domain.cfg"

// vmStoppedStatus is the virsh state meaning a VM was shut down (the
// equivalent of an exited docker container).
const vmStoppedStatus = "shut off"

// ErrVMServiceDisabled is returned (wrapped in ModuleNotAvailable) when the
// Unraid VM service is turned off.
var ErrVMServiceDisabled = errors.New("VM service is disabled")

// vmStates are the domain states virsh can print, longest first so that
// multi-word states win over their single-word suffixes.
var vmStates = []string{
	"in shutdown",
	"pmsuspended",
	"shut off",
	"no state",
	"running",
	"crashed",
	"paused",
	"idle",
}

// ConfVMs extends ConfBase with virtual machine settings
type ConfVMs struct {
	ConfBase `yaml:",inline"`
	// List of VM names to ignore
	Ignore []string `yaml:"ignore"`
	// List of VM names for which being shut off is not a warning
	IgnoreStopped []string `yaml:"ignore_stopped"`
}

// Init sets default values
func (c *ConfVMs) Init() {
	c.ConfBase.Init()
	c.Ignore = []string{}
	c.IgnoreStopped = []string{}
}

// GetVMs reports the state of libvirt virtual machines, as listed by virsh.
func GetVMs(channel chan<- SourceReturn, conf *Conf) {
	sourceConf := conf.VMs
	sourceConf.Load(conf)

	returnData := NewSourceReturn(conf.debug)
	defer func() {
		channel <- returnData.Return()
	}()

	if vmServiceDisabled(vmDomainCfg) {
		returnData.Error = &ModuleNotAvailable{"vms", ErrVMServiceDisabled}

		return
	}

	path, err := exec.LookPath("virsh")
	if err != nil {
		returnData.Error = &ModuleNotAvailable{"vms", err}

		return
	}

	vms, err := getVMs(path)
	if err != nil {
		log.Debugf("Could not list VMs: %v", err)
		t := GetTableWriter(sourceConf)
		returnData.Content = RenderTable(t, "VMs: "+utils.Warn("Unavailable"))

		return
	}

	cl := containerList{Runtime: "VMs", StoppedStatus: vmStoppedStatus, Containers: vms}
	returnData.Content = cl.getContent(sourceConf.Ignore, sourceConf.IgnoreStopped, *sourceConf.WarnOnly, sourceConf)
}

// vmServiceDisabled reports whether the Unraid domain config explicitly disables
// the VM service. A missing or unreadable file counts as not disabled.
func vmServiceDisabled(path string) bool {
	cfg, err := ini.Load(path)
	if err != nil {
		return false
	}

	return strings.ToLower(cfg.Section("").Key("SERVICE").String()) == "disable"
}

// getVMs runs `virsh list --all` and parses the result
func getVMs(virshPath string) ([]containerStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), virshTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, virshPath, "--connect", "qemu:///system", "list", "--all") // #nosec G204
	// Do not hang on a child process that keeps the pipe open after the kill
	cmd.WaitDelay = time.Second
	// virsh localizes state names, force the C locale so parsing sees English ones
	cmd.Env = append(os.Environ(), "LC_ALL=C")

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("virsh list: %w", err)
	}

	return parseVirshList(string(out)), nil
}

// parseVirshList parses the table printed by `virsh list --all`. VM names may
// contain spaces and states may be two words, so each row is split into the id
// and a remainder, from which a known state suffix is stripped to get the name.
func parseVirshList(out string) []containerStatus {
	var vms []containerStatus

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Trim(line, "-") == "" {
			continue
		}

		fields := strings.Fields(line)
		if fields[0] == "Id" && strings.Contains(line, "Name") && strings.Contains(line, "State") {
			continue
		}

		// Drop the leading Id column (a number or "-")
		rest := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
		name, state := splitVMRow(rest)

		if name == "" {
			continue
		}

		vms = append(vms, containerStatus{Name: name, Status: state})
	}

	return vms
}

// splitVMRow separates "name   state" into its parts
func splitVMRow(rest string) (name string, state string) {
	for _, known := range vmStates {
		if !strings.HasSuffix(rest, known) {
			continue
		}

		head := strings.TrimSuffix(rest, known)
		if head != "" && (strings.HasSuffix(head, " ") || strings.HasSuffix(head, "\t")) {
			return strings.TrimSpace(head), known
		}
	}

	// Unknown state: assume it is the last word
	fields := strings.Fields(rest)
	state = fields[len(fields)-1]
	name = strings.TrimSpace(strings.TrimSuffix(rest, state))

	return name, state
}
