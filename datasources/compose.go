package datasources

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"

	"github.com/dkaser/unraid-motd/utils"
)

// composeProjectLabel is the label docker compose sets on every container it creates,
// naming the stack (project) the container belongs to.
const composeProjectLabel = "com.docker.compose.project"

var composeFileNames = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// ConfCompose extends ConfBase with docker-compose stack settings
type ConfCompose struct {
	ConfBase `yaml:",inline"`
	// Directory containing one subdirectory per compose stack, as managed by the
	// Unraid Compose Manager plugin
	ProjectsPath string `yaml:"projects_path"`
	// List of stack names to ignore
	Ignore []string `yaml:"ignore"`
	// List of stack names for which being fully stopped is not a warning
	IgnoreStopped []string `yaml:"ignore_stopped"`
}

// Init sets the default Compose Manager projects path
func (c *ConfCompose) Init() {
	c.ConfBase.Init()
	c.ProjectsPath = "/boot/config/plugins/compose.manager/projects"
	c.Ignore = []string{}
	c.IgnoreStopped = []string{}
}

// GetCompose reports the status of docker-compose stacks found under ProjectsPath.
//
// docker-compose removes a stack's containers entirely on "down", so unlike
// individual docker containers, a stack can go from existing to invisible in
// `docker ps`. Stacks are therefore enumerated from disk rather than from
// running containers, so a fully-stopped stack is still reported.
func GetCompose(channel chan<- SourceReturn, conf *Conf) {
	sourceConf := conf.Compose
	sourceConf.Load(conf)

	returnData := NewSourceReturn(conf.debug)
	defer func() {
		channel <- returnData.Return()
	}()

	projects, err := listComposeProjects(sourceConf.ProjectsPath)
	if err != nil {
		returnData.Error = &ModuleNotAvailable{"compose", err}

		return
	}

	stacks, err := getComposeStackStatus(projects)
	if err != nil {
		t := GetTableWriter(sourceConf)
		returnData.Content = RenderTable(t, "Compose: "+utils.Warn("Unavailable"))

		return
	}

	cl := containerList{Runtime: "Compose", Containers: stacks}
	returnData.Content = cl.getContent(sourceConf.Ignore, sourceConf.IgnoreStopped, *sourceConf.WarnOnly, sourceConf)
}

// listComposeProjects returns the names of subdirectories of root that contain a compose file
func listComposeProjects(root string) (projects []string, err error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if hasComposeFile(filepath.Join(root, entry.Name())) {
			projects = append(projects, entry.Name())
		}
	}

	return
}

func hasComposeFile(dir string) bool {
	for _, name := range composeFileNames {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}

	return false
}

type composeCounts struct {
	running int
	total   int
}

// getComposeStackStatus reports one synthetic containerStatus per project: "running" if
// every container in the stack is running, "exited" if the stack has no containers at
// all (i.e. it has been "down"ed), or "partial" if only some containers are running.
func getComposeStackStatus(projects []string) (statuses []containerStatus, err error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion(dockerMinAPI))
	if err != nil {
		return
	}

	f := filters.NewArgs(filters.Arg("label", composeProjectLabel))

	containers, err := cli.ContainerList(context.Background(), container.ListOptions{All: true, Filters: f})
	if err != nil {
		return
	}

	counts := make(map[string]composeCounts)

	for _, ct := range containers {
		project, ok := ct.Labels[composeProjectLabel]
		if !ok {
			continue
		}

		c := counts[project]
		c.total++

		if strings.ToLower(ct.State) == "running" {
			c.running++
		}

		counts[project] = c
	}

	for _, project := range projects {
		c := counts[project]

		status := "running"

		switch {
		case c.total == 0:
			status = "exited"
		case c.running < c.total:
			status = "partial"
		}

		statuses = append(statuses, containerStatus{Name: project, Status: status})
	}

	sort.Slice(statuses, func(i, j int) bool { return statuses[i].Name < statuses[j].Name })

	return
}
