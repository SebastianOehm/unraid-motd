package datasources

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/dkaser/unraid-motd/utils"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"

	"github.com/shirou/gopsutil/v3/cpu"
)

// errMeminfoIncomplete is used when /proc/meminfo lacks MemTotal or MemAvailable.
var errMeminfoIncomplete = errors.New("MemTotal or MemAvailable missing in meminfo")

// errLoadavgMalformed is used when /proc/loadavg does not start with three load numbers.
var errLoadavgMalformed = errors.New("malformed loadavg")

// loadFields is the number of load averages (1, 5 and 15 minutes).
const loadFields = 3

// sysLevel is the severity of a reading, ordered from best to worst.
type sysLevel int

const (
	sysOK sysLevel = iota
	sysWarning
	sysCritical
)

// ConfSysInfo extends ConfBase with threshold settings
type ConfSysInfo struct {
	ConfBase `yaml:",inline"`
	// RAM used (percent) at or above which the value is yellow, 0 disables
	RAMWarn int `yaml:"ram_warn"`
	// RAM used (percent) at or above which the value is red, 0 disables
	RAMCrit int `yaml:"ram_crit"`
	// Swap used (percent) at or above which the value is yellow, 0 disables
	SwapWarn int `yaml:"swap_warn"`
	// Swap used (percent) at or above which the value is red, 0 disables
	SwapCrit int `yaml:"swap_crit"`
	// Load average as percent of logical CPUs at or above which a value is yellow, 0 disables
	LoadWarn int `yaml:"load_warn"`
	// Load average as percent of logical CPUs at or above which a value is red, 0 disables
	LoadCrit int `yaml:"load_crit"`
}

func (c *ConfSysInfo) Init() {
	c.ConfBase.Init()
	c.Border = new(bool)
	c.RAMWarn = 90
	c.RAMCrit = 95
	c.SwapWarn = 50
	c.SwapCrit = 80
	c.LoadWarn = 100
	c.LoadCrit = 200
}

// meminfo holds the /proc/meminfo values used by the module, in kB.
type meminfo struct {
	MemTotal     float64
	MemAvailable float64
	SwapTotal    float64
	SwapFree     float64
}

// GetSysInfo various stats about the host Linux OS (kernel, distro, load and more)
func GetSysInfo(channel chan<- SourceReturn, conf *Conf) {
	sourceConf := conf.SysInfo
	sourceConf.Load(conf)

	returnData := NewSourceReturn(conf.debug)
	defer func() {
		channel <- returnData.Return()
	}()
	type entry struct {
		name    string
		content string
	}

	outputTable := GetTableWriter(sourceConf)
	outputTable.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, Align: text.AlignLeft},
	})

	// Fetch all the things
	var info = []entry{
		{"Version", getDistroName()},
		{"Kernel", getKernel()},
		{"Uptime", getUptime()},
		{"CPU", getCPU()},
		{"Load", getLoadAvg(runtime.NumCPU(), sourceConf.LoadWarn, sourceConf.LoadCrit)},
	}

	mem, memErr := readMeminfo()
	if memErr != nil || mem.MemTotal == 0 {
		info = append(info, entry{"RAM", utils.Warn("unavailable")})
	} else {
		info = append(info, entry{"RAM", formatMemory(mem.MemTotal-mem.MemAvailable, mem.MemTotal, sourceConf.RAMWarn, sourceConf.RAMCrit)})
		if mem.SwapTotal > 0 {
			info = append(info, entry{"Swap", formatMemory(mem.SwapTotal-mem.SwapFree, mem.SwapTotal, sourceConf.SwapWarn, sourceConf.SwapCrit)})
		}
	}

	for _, e := range info {
		outputTable.AppendRow([]interface{}{e.name, e.content})
	}
	returnData.Content = RenderTable(outputTable, "")
}

func getCPU() (retStr string) {
	times, _ := cpu.Times(false)

	totalCPU := times[0]
	totalCount := totalCPU.User + totalCPU.System + totalCPU.Idle + totalCPU.Iowait + totalCPU.Softirq

	userPercent := (totalCPU.User / totalCount) * 100
	systemPercent := (totalCPU.System / totalCount) * 100
	idlePercent := (totalCPU.Idle / totalCount) * 100
	ioWaitPercent := (totalCPU.Iowait / totalCount) * 100

	retStr = fmt.Sprintf("Usr: %.1f%% Sys: %.1f%% IO: %.1f%% Idle: %.1f%%", userPercent, systemPercent, ioWaitPercent, idlePercent)

	return
}

// runCmd executes command and returns stdout as string
func runCmd(name string, args string, buf *bytes.Buffer) (string, error) {
	var retStr string
	cmd := exec.Command(name, args)
	cmd.Stdout = buf
	err := cmd.Run()
	if err != nil {
		retStr = utils.Warn("unavailable")
	} else {
		retStr = buf.String()
	}
	buf.Reset()

	return retStr, CommandFailedError(fmt.Sprint(err))
}

func getDistroName() (retStr string) {
	file, err := os.Open("/etc/unraid-version")
	if err != nil {
		retStr = utils.Warn("unavailable")

		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Look for pretty name
	re := regexp.MustCompile(`version=(.*)`)
	for scanner.Scan() {
		m := re.FindSubmatch(scanner.Bytes())
		if len(m) > 1 {
			// Remove quotes
			retStr = strings.Replace(string(m[1]), `"`, "", 2)

			return
		}
	}
	if err := scanner.Err(); err != nil {
		retStr = utils.Warn("unavailable")

		return
	}

	return
}

func getUptime() string {
	var buf bytes.Buffer
	uptime, err := runCmd("uptime", "-p", &buf)
	if err != nil {
		return uptime
	}
	re := regexp.MustCompile(`(up\s|\n)`)

	return re.ReplaceAllString(uptime, "")
}

// thresholdLevel returns the level of value: critical at or above crit,
// warning at or above warn. A threshold of 0 (or less) is disabled.
func thresholdLevel(value, warn, crit int) sysLevel {
	switch {
	case crit > 0 && value >= crit:
		return sysCritical
	case warn > 0 && value >= warn:
		return sysWarning
	default:
		return sysOK
	}
}

// colorByLevel colors text according to level, sysOK leaves it untouched.
func colorByLevel(text string, level sysLevel) string {
	switch level {
	case sysCritical:
		return utils.Err(text)
	case sysWarning:
		return utils.Warn(text)
	case sysOK:
		return text
	}

	return text
}

// parseLoadavg returns the first three fields of /proc/loadavg as written
// and as numbers.
func parseLoadavg(raw string) ([loadFields]string, [loadFields]float64, error) {
	var strs [loadFields]string
	var vals [loadFields]float64

	fields := strings.Fields(raw)
	if len(fields) < loadFields {
		return strs, vals, errLoadavgMalformed
	}
	for idx := range loadFields {
		val, err := strconv.ParseFloat(fields[idx], 64)
		if err != nil {
			return strs, vals, fmt.Errorf("%w: %w", errLoadavgMalformed, err)
		}
		strs[idx] = fields[idx]
		vals[idx] = val
	}

	return strs, vals, nil
}

// formatLoad renders the load averages, coloring each number by its share
// of the logical CPUs.
func formatLoad(strs [loadFields]string, vals [loadFields]float64, cpus, warn, crit int) string {
	cpus = max(cpus, 1)

	var colored [loadFields]string
	for idx := range loadFields {
		percent := int(vals[idx] / float64(cpus) * 100)
		colored[idx] = colorByLevel(strs[idx], thresholdLevel(percent, warn, crit))
	}

	return fmt.Sprintf("%s [1m], %s [5m], %s [15m]", colored[0], colored[1], colored[2])
}

func getLoadAvg(cpus, warn, crit int) string {
	loadavg, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return utils.Warn("unavailable")
	}
	strs, vals, err := parseLoadavg(string(loadavg))
	if err != nil {
		return utils.Warn("unavailable")
	}

	return formatLoad(strs, vals, cpus, warn, crit)
}

// parseMeminfo reads the values the module needs from /proc/meminfo content.
// MemTotal and MemAvailable are required, the swap values default to 0.
func parseMeminfo(r io.Reader) (meminfo, error) {
	var info meminfo
	var haveTotal, haveAvailable bool

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		var target *float64
		switch fields[0] {
		case "MemTotal:":
			target, haveTotal = &info.MemTotal, true
		case "MemAvailable:":
			target, haveAvailable = &info.MemAvailable, true
		case "SwapTotal:":
			target = &info.SwapTotal
		case "SwapFree:":
			target = &info.SwapFree
		default:
			continue
		}
		val, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return meminfo{}, fmt.Errorf("parse %s: %w", fields[0], err)
		}
		*target = val
	}
	if err := scanner.Err(); err != nil {
		return meminfo{}, fmt.Errorf("read meminfo: %w", err)
	}
	if !haveTotal || !haveAvailable {
		return meminfo{}, errMeminfoIncomplete
	}

	return info, nil
}

// readMeminfo parses /proc/meminfo
func readMeminfo() (meminfo, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return meminfo{}, fmt.Errorf("open meminfo: %w", err)
	}
	defer file.Close()

	return parseMeminfo(file)
}

// formatMemory renders used and total (kB) as percent and sizes, colored by
// the used percent. total must be greater than 0.
func formatMemory(used, total float64, warn, crit int) string {
	percent := int((used / total) * 100)
	// Convert to GB, meminfo is in kB
	out := fmt.Sprintf("%d%% used (%s of %s)", percent, utils.FormatBytes(used*1024), utils.FormatBytes(total*1024))

	return colorByLevel(out, thresholdLevel(percent, warn, crit))
}

func getKernel() string {
	var buf bytes.Buffer
	var kernel, _ = runCmd("uname", "-sr", &buf)

	return strings.ReplaceAll(kernel, "\n", "")
}
