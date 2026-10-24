package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/olekukonko/tablewriter"
	log "github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/writer"
	"gopkg.in/yaml.v2"

	"github.com/dkaser/unraid-motd/datasources"
	"github.com/dkaser/unraid-motd/utils"

	"github.com/arsham/figurine/figurine"
	"golang.org/x/term"
)

var defaultCfgPath = "/boot/config/plugins/motd/config.yaml"

func makeTable(buf *strings.Builder, padding int) (table *tablewriter.Table) {
	table = tablewriter.NewWriter(buf)
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(false)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetBorder(false)
	table.SetTablePadding(strings.Repeat(" ", padding))
	table.SetNoWhiteSpace(true)

	return
}

func mapToTable(buf *strings.Builder, inStr map[string]string, colDef [][]string, padding int) {
	table := makeTable(buf, padding)
	var tmp []string
	// Render a new table every row for compact output
	for _, row := range colDef {
		// Just write block to buffer if it is alone
		if len(row) == 1 {
			a, ok := inStr[row[0]]
			// Skip invalid modules
			if !ok {
				continue
			}
			_, _ = fmt.Fprintln(buf, a)

			continue
		}
		tmp = nil
		for _, k := range row {
			a, ok := inStr[k]
			if !ok {
				continue
			}
			tmp = append(tmp, a)
		}
		table.Append(tmp)
		table.Render()
		// Remake table to avoid imbalanced output
		table = makeTable(buf, padding)
	}
}

// makePrintOrder flattens colDef (if present). If showOrder is defined as well, it is ignored.
func makePrintOrder(c *datasources.Conf) (printOrder []string) {
	// Flatten 2-dim input
	for _, row := range c.ColDef {
		printOrder = append(printOrder, row...)
	}

	return
}

var args struct {
	ConfigFile      string `arg:"-c,--config,env:CONFIG_FILE"             help:"Path to config yaml"`
	Debug           bool   `arg:"--debug,env:DEBUG"                       help:"Debug mode"`
	DumpConfig      bool   `arg:"--dump-config"                           help:"Dump config and exit"`
	HideUnavailable bool   `arg:"--hide-unavailable,env:HIDE_UNAVAILABLE" help:"Hide unavailable modules"`
	LogLevel        string `arg:"--log-level,env:LOG_LEVEL"               help:"Set log level"`
	NoColor         bool   `arg:"--no-color"                              help:"Print without colors; also renders when not in a terminal (width from --width)"`
	Preview         bool   `arg:"--preview"                               help:"Render output even without a terminal (for the settings page preview)"`
	Width           int    `arg:"--width"                                 help:"Width when not using the terminal size (--preview, or --no-color without a terminal; default 80)"`
	CheckConfig     bool   `arg:"--check-config"                          help:"Validate the config file and exit (exit code 1 on error)"`
}

const defaultWidth = 80

// renderWidth decides the output width and whether to render at all.
//
// forceFlagWidth (--preview, --dump-config) always uses flagWidth, even in a terminal.
// allowNoTTY (--no-color) uses the terminal width when there is one, else flagWidth.
// Otherwise output needs a terminal; without one nothing is rendered.
// A flagWidth <= 0 means the default width.
func renderWidth(forceFlagWidth, allowNoTTY bool, termWidth int, termOK bool, flagWidth int) (width int, ok bool) {
	if !forceFlagWidth && termOK {
		return termWidth, true
	}
	if !forceFlagWidth && !allowNoTTY {
		return 0, false
	}
	if flagWidth <= 0 {
		return defaultWidth, true
	}

	return flagWidth, true
}

// terminalWidth returns the width of stdin's terminal; ok is false without a terminal or when the size is unknown.
func terminalWidth() (width int, ok bool) {
	if !term.IsTerminal(0) {
		return 0, false
	}
	width, _, err := term.GetSize(0)

	return width, err == nil
}

// writeBanner prints the figurine header, without colors if requested.
func writeBanner(text, font string) {
	if !utils.NoColors {
		if err := figurine.Write(os.Stdout, text, font); err != nil {
			log.Debug(err.Error())
		}

		return
	}
	var buf bytes.Buffer
	if err := figurine.Write(&buf, text, font); err != nil {
		log.Debug(err.Error())
	}
	fmt.Print(utils.StripANSI(buf.String()))
}

func setupLogging() {
	var logLevel log.Level
	defaultLevel := log.WarnLevel

	var err error
	getLogLevels := func(level log.Level) []log.Level {
		ret := make([]log.Level, 0)
		for _, lvl := range log.AllLevels {
			if level >= lvl {
				ret = append(ret, lvl)
			}
		}

		return ret
	}

	log.SetFormatter(&log.TextFormatter{DisableTimestamp: true, DisableColors: utils.NoColors})
	log.SetOutput(io.Discard)
	if args.Debug {
		logLevel = log.DebugLevel
	} else if args.LogLevel != "" {
		logLevel, err = log.ParseLevel(args.LogLevel)
		if err != nil {
			logLevel = defaultLevel
			log.Warnf("Unknown log level %s, defaulting to %s", args.LogLevel, logLevel.String())
		}
	} else {
		logLevel = defaultLevel
	}
	log.SetLevel(logLevel)
	levels := getLogLevels(logLevel)
	if args.Debug {
		log.AddHook(&writer.Hook{
			Writer:    os.Stderr,
			LogLevels: levels,
		})
	}
}

func runModules(conf *datasources.Conf) {
	outOrder, outData := datasources.RunSources(makePrintOrder(conf), conf)
	outStr := make(map[string]string)
	// Wait and save results
	for _, source := range outOrder {
		sourceData, ok := outData[source]
		if !ok {
			continue
		}
		// Check if we should skip due to unavailable error
		if _, unOK := sourceData.Error.(datasources.UnavailableError); unOK && args.HideUnavailable {
			continue
		}
		if sourceData.Error != nil {
			log.Warnf("%s error: %v", source, sourceData.Error)
		}

		if sourceData.Content != "" {
			outStr[source] = sourceData.Content
		}
	}
	outBuf := &strings.Builder{}

	mapToTable(outBuf, outStr, conf.ColDef, conf.ColPad)

	fmt.Print(outBuf.String())

	// Show timing results
	if args.Debug {
		for _, k := range outOrder {
			log.Debugf("%s ran in: %s", k, outData[k].Time.String())
		}
	}
}

// checkConfig validates the config file, exiting with code 1 on error.
func checkConfig() {
	if _, err := datasources.NewConfFromFile(args.ConfigFile, false); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("OK")
}

func main() {
	args.ConfigFile = defaultCfgPath
	arg.MustParse(&args)
	utils.NoColors = args.NoColor || os.Getenv("NO_COLOR") != ""

	setupLogging()

	// Validate the config only (used by the settings page)
	if args.CheckConfig {
		checkConfig()

		return
	}

	termWidth, termOK := terminalWidth()
	width, ok := renderWidth(args.Preview || args.DumpConfig, args.NoColor, termWidth, termOK, args.Width)
	if !ok {
		return
	}

	var mainStart time.Time
	if args.Debug {
		mainStart = time.Now()
	}
	// Read config file
	conf, err := datasources.NewConfFromFile(args.ConfigFile, args.Debug)
	if err != nil {
		log.Warn(err)
	}

	// Dumping must show the configured value, so only clamp when rendering
	if !args.DumpConfig && conf.FixedTableWidth > width {
		conf.FixedTableWidth = width
	}

	if args.DumpConfig {
		log.Info("Dumping config")
		if flag.NArg() > 0 {
			dumpConfig(&conf, flag.Arg(0))
		} else {
			dumpConfig(&conf, "")
		}

		return
	}

	if conf.Header.Show {
		text := conf.Header.CustomText
		if conf.Header.UseHostname {
			text, _ = os.Hostname()
		}

		writeBanner(text, conf.Header.Font)

		fmt.Println("")
	}

	runModules(&conf)

	// Show timing results
	if args.Debug {
		log.Debugf("main ran in: %s", time.Since(mainStart).String())
	}
}

func dumpConfig(c *datasources.Conf, writeFile string) {
	configDump, err := yaml.Marshal(c)
	if err != nil {
		log.Errorf("Config parse error: %v", err)

		return
	}
	if writeFile != "" {
		err = os.WriteFile(writeFile, configDump, 0600)
		if err != nil {
			log.Errorf("Config dumped failed: %v", err)

			return
		}
		log.Infof("Config dumped to: %s", writeFile)
	} else {
		fmt.Printf("%s\n", string(configDump))
	}
}
