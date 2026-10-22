package datasources

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/dkaser/unraid-motd/utils"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type UnavailableError interface {
	error
	UnavailableError()
}

type ModuleNotAvailable struct {
	Name        string
	ParentError error
}

func (m *ModuleNotAvailable) Error() string {
	return "module " + m.Name + " is not available: " + m.ParentError.Error()
}

func (ModuleNotAvailable) UnavailableError() {}

// SourceReturn is the data returned by a datasource through a channel
type SourceReturn struct {
	// Datasource output content string
	Content string
	// Error
	Error error
	// Time taken, non-zero only in debug mode
	Time time.Duration
	// Internal
	start time.Time
}

func (sr *SourceReturn) Return() SourceReturn {
	if !sr.start.IsZero() {
		sr.Time = time.Since(sr.start)
	}

	return *sr
}

func NewSourceReturn(debug bool) *SourceReturn {
	sr := SourceReturn{}
	if debug {
		sr.start = time.Now()
	}

	return &sr
}

// ConfInterface defines the interface for config structs
type ConfInterface interface {
	Init()
}

func NewConfFromFile(path string, debug bool) (c Conf, err error) {
	c.Init()
	c.debug = debug
	yamlFile, errF := os.ReadFile(path)
	if errors.Is(errF, fs.ErrNotExist) {
		return
	}
	if errF != nil {
		err = ConfigFileError(errF.Error())

		return
	}
	err = yaml.Unmarshal(yamlFile, &c)
	if err != nil {
		err = ParseError(fmt.Sprintf("%s: %v", path, err))

		return
	}

	return
}

// moduleDef describes a data source, its config and its title
type moduleDef struct {
	run   func(chan<- SourceReturn, *Conf)
	base  func(*Conf) ConfBase
	title string
}

var modules = map[string]moduleDef{
	"cpu":           {GetCPUTemp, func(c *Conf) ConfBase { return c.CPU.ConfBase }, "CPU Temp"},
	"docker":        {GetDocker, func(c *Conf) ConfBase { return c.Docker.ConfBase }, "Docker"},
	"compose":       {GetCompose, func(c *Conf) ConfBase { return c.Compose.ConfBase }, "Compose"},
	"vms":           {GetVMs, func(c *Conf) ConfBase { return c.VMs.ConfBase }, "VMs"},
	"ups":           {GetUPS, func(c *Conf) ConfBase { return c.UPS.ConfBase }, "UPS"},
	"gpu":           {GetGPU, func(c *Conf) ConfBase { return c.GPU.ConfBase }, "GPU"},
	"sysinfo":       {GetSysInfo, func(c *Conf) ConfBase { return c.SysInfo.ConfBase }, "System Info"},
	"user-drives":   {GetUserDrives, func(c *Conf) ConfBase { return c.UserDrives.ConfBase }, "User Drives"},
	"system-drives": {GetSystemDrives, func(c *Conf) ConfBase { return c.SystemDrives.ConfBase }, "System Drives"},
	"networks":      {GetNetworks, func(c *Conf) ConfBase { return c.Networks.ConfBase }, "Networks"},
	"services":      {GetServices, func(c *Conf) ConfBase { return c.Services.ConfBase }, "Services"},
}

// RunSources runs data sources in runList, the names are validated and returned as the first value
//
// A source exceeding its timeout is reported as unavailable, its goroutine is abandoned
func RunSources(runList []string, conf *Conf) ([]string, map[string]SourceReturn) {
	channels := make(map[string]chan SourceReturn)
	out := make(map[string]SourceReturn)
	var validRuns []string
	// Start goroutines
	for _, source := range runList {
		def, ok := modules[source]
		if !ok {
			log.Warnf("no data source named %s", source)

			continue
		}
		// Buffered, so an abandoned source never blocks when it finally sends
		channel := make(chan SourceReturn, 1)
		go def.run(channel, conf)
		channels[source] = channel
		validRuns = append(validRuns, source)
	}
	start := time.Now()
	// Wait for results
	log.Debug("Wait for goroutines")
	for _, source := range validRuns {
		if _, done := out[source]; done {
			continue
		}
		def := modules[source]
		base := def.base(conf)
		base.Load(conf)
		timeout := base.GetTimeout()
		if timeout == 0 {
			out[source] = <-channels[source]

			continue
		}
		// A result already waiting always wins, select would pick randomly when the deadline passed
		select {
		case result := <-channels[source]:
			out[source] = result

			continue
		default:
		}
		timer := time.NewTimer(time.Until(start.Add(timeout)))
		select {
		case result := <-channels[source]:
			out[source] = result
		case <-timer.C:
			log.Debugf("module %s timed out after %s", source, timeout)
			result := SourceReturn{
				Content: RenderTable(GetTableWriter(base), def.title+": "+utils.Warn("Timed out")),
				Error:   &ModuleNotAvailable{Name: source, ParentError: ErrTimeout},
			}
			if conf.debug {
				result.Time = time.Since(start)
			}
			out[source] = result
		}
		timer.Stop()
	}

	return validRuns, out
}
