package datasources

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/dkaser/unraid-motd/utils"
)

// upscTimeout bounds how long a single upsc invocation may run, so an
// unreachable NUT server cannot stall the whole MOTD.
const upscTimeout = 5 * time.Second

// errNoUPSStatus is used when upsc output lacks the ups.status variable.
var errNoUPSStatus = errors.New("no ups.status in upsc output")

// upsLevel is the severity of a UPS reading, ordered from best to worst.
type upsLevel int

const (
	upsOK upsLevel = iota
	upsWarning
	upsCritical
)

// upsFlags describes the NUT status flags: the text shown for each one and
// the level it contributes. Unknown flags are shown as is and count as warning.
var upsFlags = map[string]struct {
	text  string
	level upsLevel
}{
	"OL":      {"Online", upsOK},
	"CHRG":    {"Charging", upsOK},
	"DISCHRG": {"Discharging", upsOK},
	"TRIM":    {"Trimming", upsOK},
	"BOOST":   {"Boosting", upsOK},
	"CAL":     {"Calibrating", upsOK},
	"OB":      {"On battery", upsWarning},
	"RB":      {"Replace battery", upsWarning},
	"OVER":    {"Overload", upsWarning},
	"BYPASS":  {"Bypass", upsWarning},
	"OFF":     {"Off", upsWarning},
	"LB":      {"Low battery", upsCritical},
	"FSD":     {"Forced shutdown", upsCritical},
}

// upsRow is one line of the UPS table.
type upsRow struct {
	label string
	text  string
	level upsLevel
}

// ConfUPS extends ConfBase with UPS settings
type ConfUPS struct {
	ConfBase `yaml:",inline"`
	// UPS to query, in the form name[@hostname[:port]] as understood by upsc
	UPS string `yaml:"ups"`
	// Battery charge (percent) at or below which a warning is shown
	BatteryWarn int `yaml:"battery_warn"`
	// Battery charge (percent) at or below which the state is critical
	BatteryCrit int `yaml:"battery_crit"`
}

// Init sets default values
func (c *ConfUPS) Init() {
	c.ConfBase.Init()
	c.UPS = "ups@localhost"
	c.BatteryWarn = 50
	c.BatteryCrit = 20
}

// GetUPS reports the state of a UPS managed by NUT, as read with upsc.
func GetUPS(channel chan<- SourceReturn, conf *Conf) {
	sourceConf := conf.UPS
	sourceConf.Load(conf)

	returnData := NewSourceReturn(conf.debug)
	defer func() {
		channel <- returnData.Return()
	}()

	path, err := exec.LookPath("upsc")
	if err != nil {
		returnData.Error = &ModuleNotAvailable{"ups", err}

		return
	}

	vars, err := getUPSVars(path, sourceConf.UPS)
	if err == nil {
		if _, ok := vars["ups.status"]; !ok {
			err = errNoUPSStatus
		}
	}

	if err != nil {
		log.Debugf("Could not read UPS: %v", err)
		t := GetTableWriter(sourceConf)
		returnData.Content = RenderTable(t, "UPS: "+utils.Warn("Unavailable"))

		return
	}

	returnData.Content = formatUPS(vars, &sourceConf)
}

// getUPSVars runs `upsc <target>` and parses the result
func getUPSVars(upscPath string, target string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), upscTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, upscPath, target) // #nosec G204
	// Do not hang on a child process that keeps the pipe open after the kill
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "LC_ALL=C")

	// Only stdout is used, upsc prints SSL notices on stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("upsc %s: %w", target, err)
	}

	return parseUPSC(string(out)), nil
}

// parseUPSC parses the `key: value` lines printed by upsc. Blank and malformed
// lines are skipped.
func parseUPSC(out string) map[string]string {
	vars := make(map[string]string)

	for _, line := range strings.Split(out, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), ": ")
		key = strings.TrimSpace(key)

		if !found || key == "" {
			continue
		}

		vars[key] = strings.TrimSpace(value)
	}

	return vars
}

// upsStatusLevel classifies the space-separated ups.status flags. The worst
// flag decides the level, the display text lists every flag.
func upsStatusLevel(status string) (upsLevel, string) {
	level := upsOK
	texts := []string{}

	for _, flag := range strings.Fields(status) {
		info, known := upsFlags[flag]
		if !known {
			info.text = flag
			info.level = upsWarning
		}

		level = max(level, info.level)
		texts = append(texts, info.text)
	}

	return level, strings.Join(texts, ", ")
}

// upsBatteryLevel classifies a battery charge in percent against the thresholds
func upsBatteryLevel(charge float64, conf *ConfUPS) upsLevel {
	switch {
	case charge <= float64(conf.BatteryCrit):
		return upsCritical
	case charge <= float64(conf.BatteryWarn):
		return upsWarning
	default:
		return upsOK
	}
}

// formatRuntime renders seconds as "1h 05m" or, below an hour, "12m"
func formatRuntime(seconds int) string {
	minutes := max(seconds, 0) / 60
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}

	return fmt.Sprintf("%dh %02dm", minutes/60, minutes%60)
}

// upsRows builds the table rows from the upsc variables, skipping missing ones
func upsRows(vars map[string]string, conf *ConfUPS) []upsRow {
	var rows []upsRow

	if status, ok := vars["ups.status"]; ok {
		level, text := upsStatusLevel(status)
		rows = append(rows, upsRow{"Status", text, level})
	}

	if charge, err := strconv.ParseFloat(vars["battery.charge"], 64); err == nil {
		rows = append(rows, upsRow{"Battery", fmt.Sprintf("%.0f%%", charge), upsBatteryLevel(charge, conf)})
	}

	if runtime, err := strconv.ParseFloat(vars["battery.runtime"], 64); err == nil {
		rows = append(rows, upsRow{"Runtime", formatRuntime(int(runtime)), upsOK})
	}

	if load, err := strconv.ParseFloat(vars["ups.load"], 64); err == nil {
		rows = append(rows, upsRow{"Load", fmt.Sprintf("%.0f%%", load), upsOK})
	}

	return rows
}

// colorUPS colors text according to its level
func colorUPS(level upsLevel, text string) string {
	switch level {
	case upsCritical:
		return utils.Err(text)
	case upsWarning:
		return utils.Warn(text)
	case upsOK:
		return utils.Good(text)
	default:
		return utils.Good(text)
	}
}

// formatUPS renders the UPS table. The title reflects the worse of the status
// and battery levels.
func formatUPS(vars map[string]string, conf *ConfUPS) string {
	outputTable := GetTableWriter(conf)
	worst := upsOK

	for _, row := range upsRows(vars, conf) {
		if row.label == "Status" || row.label == "Battery" {
			worst = max(worst, row.level)
		}

		if *conf.WarnOnly && row.level == upsOK {
			continue
		}

		outputTable.AppendRow([]interface{}{row.label, colorUPS(row.level, row.text)})
	}

	name := "UPS"
	if model := firstNonEmpty(vars["device.model"], vars["ups.model"]); model != "" {
		name += " (" + model + ")"
	}

	title := name + ": " + colorUPS(worst, [...]string{"OK", "Warning", "Critical"}[worst])

	return RenderTable(outputTable, title)
}

// firstNonEmpty returns the first argument that is not empty
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}
