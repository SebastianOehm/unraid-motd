package datasources

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	log "github.com/sirupsen/logrus"

	"github.com/dkaser/unraid-motd/utils"
)

// nvidiaSmiTimeout bounds how long a single nvidia-smi invocation may run, so a
// hung driver cannot stall the whole MOTD.
const nvidiaSmiTimeout = 5 * time.Second

// gpuUnknown is displayed for values nvidia-smi could not report.
const gpuUnknown = "n/a"

// Minimum field counts of the CSV lines. The name columns sit between the
// fixed ones and may themselves contain ", ".
const (
	gpuMinFields  = 7
	gpuTailFields = 4
	procMinFields = 4
)

// bytesPerMiB converts the MiB values reported by nvidia-smi to bytes.
const bytesPerMiB = 1024 * 1024

// Severity levels of a GPU, the highest one decides the table title.
const (
	gpuSeverityOK = iota
	gpuSeverityWarn
	gpuSeverityCrit
)

// containerIDRe matches a docker container ID in a cgroup path, either as a
// path element (/docker/<id>) or as a systemd scope (docker-<id>.scope).
var containerIDRe = regexp.MustCompile(`(?:/docker/|docker-)([0-9a-f]{64})(?:\.scope|/|$)`)

// ConfGPU extends ConfBaseWarn with GPU specific settings
type ConfGPU struct {
	ConfBaseWarn `yaml:",inline"`
	// Whether to list the processes using each GPU
	ShowProcesses bool `yaml:"show_processes"`
}

// Init sets default values
func (c *ConfGPU) Init() {
	c.ConfBaseWarn.Init()
	c.Warn = 80
	c.Crit = 90
	c.ShowProcesses = true
}

// gpuValue is a number that nvidia-smi may report as unknown
type gpuValue struct {
	Val int
	OK  bool
}

// gpuInfo is one row of the GPU query
type gpuInfo struct {
	Index    string
	UUID     string
	Name     string
	Temp     gpuValue
	Util     gpuValue
	MemUsed  gpuValue
	MemTotal gpuValue
}

// gpuProcess is one row of the compute apps query, Label is the container
// name if the process runs in a docker container.
type gpuProcess struct {
	UUID  string
	PID   int
	Name  string
	Mem   gpuValue
	Label string
}

// GetGPU reports temperature, utilization and memory of NVIDIA GPUs, as listed
// by nvidia-smi.
func GetGPU(channel chan<- SourceReturn, conf *Conf) {
	sourceConf := conf.GPU
	sourceConf.Load(conf)

	returnData := NewSourceReturn(conf.debug)
	defer func() {
		channel <- returnData.Return()
	}()

	path, err := exec.LookPath("nvidia-smi")
	if err != nil {
		returnData.Error = &ModuleNotAvailable{"gpu", err}

		return
	}

	gpus, err := queryGPUs(path)
	if err != nil {
		log.Debugf("Could not query GPUs: %v", err)
		t := GetTableWriter(sourceConf)
		returnData.Content = RenderTable(t, "GPU: "+utils.Warn("Unavailable"))

		return
	}

	var procs []gpuProcess
	if sourceConf.ShowProcesses {
		procs = queryGPUProcesses(path)
	}

	returnData.Content = formatGPUs(gpus, procs, &sourceConf)
}

// runNvidiaSmi runs nvidia-smi with the given arguments and returns its output
func runNvidiaSmi(smiPath string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), nvidiaSmiTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, smiPath, args...) // #nosec G204
	// Do not hang on a child process that keeps the pipe open after the kill
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "LC_ALL=C")

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("nvidia-smi %s: %w", args[0], err)
	}

	return string(out), nil
}

// queryGPUs lists the GPUs
func queryGPUs(smiPath string) ([]gpuInfo, error) {
	out, err := runNvidiaSmi(smiPath,
		"--query-gpu=index,uuid,name,temperature.gpu,utilization.gpu,memory.used,memory.total",
		"--format=csv,noheader,nounits")
	if err != nil {
		return nil, err
	}

	return parseGPUCSV(out), nil
}

// queryGPUProcesses lists the compute processes and resolves container names.
// Failures are not fatal, the GPUs are then shown without processes.
func queryGPUProcesses(smiPath string) []gpuProcess {
	out, err := runNvidiaSmi(smiPath,
		"--query-compute-apps=gpu_uuid,pid,process_name,used_memory",
		"--format=csv,noheader,nounits")
	if err != nil {
		log.Debugf("Could not query GPU processes: %v", err)

		return nil
	}

	procs := parseProcessCSV(out)
	if len(procs) == 0 {
		return nil
	}

	names, err := getDockerNamesByID()
	if err != nil {
		log.Debugf("Could not list docker containers: %v", err)
	}

	labelProcesses(procs, readProcCgroup, names)

	return procs
}

// parseGPUValue parses a numeric field, "[N/A]" and similar count as unknown
func parseGPUValue(field string) gpuValue {
	val, err := strconv.Atoi(strings.TrimSpace(field))
	if err != nil {
		return gpuValue{}
	}

	return gpuValue{Val: val, OK: true}
}

// splitCSVLine splits a line by ", " and reports whether it has enough fields
func splitCSVLine(line string, minFields int) ([]string, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, false
	}

	fields := strings.Split(line, ", ")

	return fields, len(fields) >= minFields
}

// parseGPUCSV parses the output of the GPU query. The name may contain commas,
// so it is everything between the two leading and four trailing fields.
func parseGPUCSV(out string) []gpuInfo {
	var gpus []gpuInfo

	for _, line := range strings.Split(out, "\n") {
		fields, ok := splitCSVLine(line, gpuMinFields)
		if !ok {
			continue
		}

		split := len(fields) - gpuTailFields
		tail := fields[split:]
		gpus = append(gpus, gpuInfo{
			Index:    fields[0],
			UUID:     fields[1],
			Name:     strings.Join(fields[2:split], ", "),
			Temp:     parseGPUValue(tail[0]),
			Util:     parseGPUValue(tail[1]),
			MemUsed:  parseGPUValue(tail[2]),
			MemTotal: parseGPUValue(tail[3]),
		})
	}

	return gpus
}

// parseProcessCSV parses the output of the compute apps query. The process
// name may contain commas, so it is everything between the two leading fields
// and the trailing memory field.
func parseProcessCSV(out string) []gpuProcess {
	var procs []gpuProcess

	for _, line := range strings.Split(out, "\n") {
		fields, ok := splitCSVLine(line, procMinFields)
		if !ok {
			continue
		}

		pid, err := strconv.Atoi(strings.TrimSpace(fields[1]))
		if err != nil {
			continue
		}

		last := len(fields) - 1
		procs = append(procs, gpuProcess{
			UUID: fields[0],
			PID:  pid,
			Name: strings.Join(fields[2:last], ", "),
			Mem:  parseGPUValue(fields[last]),
		})
	}

	return procs
}

// containerIDFromCgroup extracts a docker container ID from the content of
// /proc/<pid>/cgroup, or returns "" when the process is not in a container.
func containerIDFromCgroup(content string) string {
	for _, line := range strings.Split(content, "\n") {
		m := containerIDRe.FindStringSubmatch(line)
		if len(m) > 1 {
			return m[1]
		}
	}

	return ""
}

// readProcCgroup reads the cgroup file of a process, "" if that is not possible
func readProcCgroup(pid int) string {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cgroup")
	if err != nil {
		return ""
	}

	return string(data)
}

// getDockerNamesByID maps full container IDs to container names
func getDockerNamesByID() (map[string]string, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion(dockerMinAPI))
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}

	allContainers, err := cli.ContainerList(context.Background(), container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("docker list: %w", err)
	}

	names := make(map[string]string, len(allContainers))
	for _, c := range allContainers {
		names[c.ID] = dockerContainerName(c)
	}

	return names, nil
}

// labelProcesses sets the label of processes that run in a known container
func labelProcesses(procs []gpuProcess, readCgroup func(pid int) string, names map[string]string) {
	for idx := range procs {
		id := containerIDFromCgroup(readCgroup(procs[idx].PID))
		if id == "" {
			continue
		}

		procs[idx].Label = names[id]
	}
}

// displayName is the container name if known, else the executable's base name
func (p gpuProcess) displayName() string {
	if p.Label != "" {
		return p.Label
	}

	return filepath.Base(p.Name)
}

// fmtMiB formats a MiB value as a size, or "n/a" if unknown
func fmtMiB(val gpuValue) string {
	if !val.OK {
		return gpuUnknown
	}

	return utils.FormatBytes(float64(val.Val) * bytesPerMiB)
}

// fmtUtil formats the utilization, or "n/a" if unknown
func fmtUtil(val gpuValue) string {
	if !val.OK {
		return gpuUnknown
	}

	return strconv.Itoa(val.Val) + "%"
}

// fmtTemp colors the temperature and reports its severity
func fmtTemp(temp gpuValue, sourceConf *ConfGPU) (text string, severity int) {
	switch {
	case !temp.OK:
		return gpuUnknown, gpuSeverityOK
	case temp.Val >= sourceConf.Crit:
		return utils.Err(temp.Val) + "°C", gpuSeverityCrit
	case temp.Val >= sourceConf.Warn:
		return utils.Warn(temp.Val) + "°C", gpuSeverityWarn
	}

	return utils.Good(temp.Val) + "°C", gpuSeverityOK
}

// gpuTitle builds the table title from the highest severity
func gpuTitle(severity int) string {
	switch severity {
	case gpuSeverityOK:
		return "GPU: " + utils.Good("OK")
	case gpuSeverityWarn:
		return "GPU: " + utils.Warn("Warning")
	}

	return "GPU: " + utils.Err("Critical")
}

// formatGPUs renders the GPUs and their processes
func formatGPUs(gpus []gpuInfo, procs []gpuProcess, sourceConf *ConfGPU) string {
	outputTable := GetTableWriter(sourceConf)
	outputTable.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, Align: text.AlignLeft},
	})
	if len(gpus) == 0 {
		return RenderTable(outputTable, "GPU: "+utils.Warn("No GPU found"))
	}

	worst := gpuSeverityOK

	for _, gpu := range gpus {
		temp, severity := fmtTemp(gpu.Temp, sourceConf)
		worst = max(worst, severity)

		if severity == gpuSeverityOK && *sourceConf.WarnOnly {
			continue
		}

		outputTable.AppendRow([]interface{}{
			fmt.Sprintf("GPU%s %s", gpu.Index, shortGPUName(gpu.Name)),
			fmt.Sprintf("%s, %s, %s / %s", temp, fmtUtil(gpu.Util),
				fmtMiB(gpu.MemUsed), fmtMiB(gpu.MemTotal)),
		})

		if sourceConf.ShowProcesses {
			for _, proc := range procs {
				if proc.UUID == gpu.UUID {
					outputTable.AppendRow([]interface{}{"  " + proc.displayName(), fmtMiB(proc.Mem)})
				}
			}
		}
	}

	return RenderTable(outputTable, gpuTitle(worst))
}

// shortGPUName drops the vendor and brand prefixes to keep the label short
func shortGPUName(name string) string {
	name = strings.TrimPrefix(name, "NVIDIA ")

	return strings.TrimPrefix(name, "GeForce ")
}
