package datasources

import (
	"strings"
	"testing"

	"github.com/dkaser/unraid-motd/utils"
)

func testTableConf() TableConfig {
	border := true
	width := 60

	return ConfBase{Border: &border, FixedTableWidth: &width}
}

func dockerList(cs ...containerStatus) *containerList {
	return &containerList{Runtime: "Docker", Containers: cs}
}

func vmList(cs ...containerStatus) *containerList {
	return &containerList{Runtime: "VMs", StoppedStatus: "shut off", Containers: cs}
}

type containerContentCase struct {
	name          string
	list          *containerList
	ignore        []string
	ignoreStopped []string
	warnOnly      bool
	contains      []string
	absent        []string
}

var containerContentCases = []containerContentCase{
	{
		name:          "docker exited ignored is hidden",
		list:          dockerList(containerStatus{"web", "Up"}, containerStatus{"batch", "exited"}),
		ignoreStopped: []string{"batch"},
		contains:      []string{"Docker: OK", "web"},
		absent:        []string{"batch", "Warning", "Critical"},
	},
	{
		name:     "docker exited not ignored warns",
		list:     dockerList(containerStatus{"web", "up"}, containerStatus{"batch", "exited"}),
		contains: []string{"Docker: Warning", "web", "batch"},
		absent:   []string{"Critical"},
	},
	{
		name:     "docker all exited is critical",
		list:     dockerList(containerStatus{"alpha-svc", "exited"}, containerStatus{"beta-svc", "exited"}),
		contains: []string{"Docker: Critical", "alpha-svc", "beta-svc"},
		absent:   []string{"Warning"},
	},
	{
		name:     "docker created is ok",
		list:     dockerList(containerStatus{"web", "created"}),
		contains: []string{"Docker: OK", "web"},
	},
	{
		name:          "vm shut off ignored is hidden",
		list:          vmList(containerStatus{"Win", "running"}, containerStatus{"HA", "shut off"}),
		ignoreStopped: []string{"HA"},
		contains:      []string{"VMs: OK", "Win"},
		absent:        []string{"HA", "Warning", "Critical"},
	},
	{
		name:     "vm shut off not ignored warns",
		list:     vmList(containerStatus{"Win", "running"}, containerStatus{"HA", "shut off"}),
		contains: []string{"VMs: Warning", "Win", "HA"},
	},
	{
		name:          "vm paused warns even if in ignore_stopped",
		list:          vmList(containerStatus{"Win", "running"}, containerStatus{"pausedvm", "paused"}),
		ignoreStopped: []string{"pausedvm"},
		contains:      []string{"VMs: Warning", "pausedvm", "Win"},
	},
	{
		name:     "vm idle counts as ok",
		list:     vmList(containerStatus{"idlevm", "idle"}, containerStatus{"Win", "running"}),
		contains: []string{"VMs: OK", "idlevm", "Win"},
		absent:   []string{"Warning", "Critical"},
	},
	{
		name:     "vm all shut off is critical",
		list:     vmList(containerStatus{"HA", "shut off"}),
		contains: []string{"VMs: Critical", "HA"},
	},
	{
		name:     "ignore list hides vm entirely",
		list:     vmList(containerStatus{"Win", "running"}, containerStatus{"secret", "crashed"}),
		ignore:   []string{"secret"},
		contains: []string{"VMs: OK", "Win"},
		absent:   []string{"secret", "Warning"},
	},
	{
		name:     "warnOnly false shows running",
		list:     vmList(containerStatus{"Win", "running"}, containerStatus{"HA", "shut off"}),
		warnOnly: false,
		contains: []string{"Win", "HA"},
	},
	{
		name:     "warnOnly true hides running",
		list:     vmList(containerStatus{"Win", "running"}, containerStatus{"HA", "shut off"}),
		warnOnly: true,
		contains: []string{"VMs: Warning", "HA"},
		absent:   []string{"Win"},
	},
	{
		name:     "all ignored yields ok title",
		list:     vmList(containerStatus{"Win", "running"}),
		ignore:   []string{"Win"},
		contains: []string{"VMs: OK"},
		absent:   []string{"Win"},
	},
}

func TestContainerListGetContent(t *testing.T) {
	prev := utils.NoColors
	utils.NoColors = true

	defer func() { utils.NoColors = prev }()

	for _, testCase := range containerContentCases {
		t.Run(testCase.name, func(t *testing.T) {
			out := testCase.list.getContent(testCase.ignore, testCase.ignoreStopped, testCase.warnOnly, testTableConf())

			for _, s := range testCase.contains {
				if !strings.Contains(out, s) {
					t.Errorf("output missing %q:\n%s", s, out)
				}
			}

			for _, s := range testCase.absent {
				if strings.Contains(out, s) {
					t.Errorf("output unexpectedly contains %q:\n%s", s, out)
				}
			}
		})
	}
}
