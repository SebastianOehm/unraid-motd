package datasources

import (
	"context"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

	"github.com/dkaser/unraid-motd/utils"
)

const (
	dockerMinAPI = "1.40"
)

// ConfDocker extends ConfBase with a list of containers to ignore
type ConfDocker struct {
	ConfBase `yaml:",inline"`
	// List of container names to ignore
	Ignore []string `yaml:"ignore"`
	// List of container names for which a stopped state is not a warning
	IgnoreStopped []string `yaml:"ignore_stopped"`
	// Flag running containers whose healthcheck reports unhealthy
	CheckHealth bool `yaml:"check_health"`
	// Number of restarts from which a container counts as a restart loop, 0 disables the check
	RestartWarn int `yaml:"restart_warn"`
	// Minutes after a start during which a running container with that many restarts is still a restart loop
	RestartWindow int `yaml:"restart_window"`
}

// Init sets up default alignment
func (c *ConfDocker) Init() {
	c.ConfBase.Init()
	c.Ignore = []string{}
	c.IgnoreStopped = []string{}
	c.CheckHealth = true
	c.RestartWarn = 3
	c.RestartWindow = 10
}

// checks returns the health and restart check settings
func (c *ConfDocker) checks() containerChecks {
	return newContainerChecks(c.CheckHealth, c.RestartWarn, c.RestartWindow)
}

// GetDocker docker container status using the API
func GetDocker(channel chan<- SourceReturn, conf *Conf) {
	sourceConf := conf.Docker
	sourceConf.Load(conf)

	returnData := NewSourceReturn(conf.debug)
	defer func() {
		channel <- returnData.Return()
	}()
	var err error
	var containers containerList
	containers, err = getDockerContainers(sourceConf.checks())

	if err != nil {
		t := GetTableWriter(sourceConf)
		returnData.Content = RenderTable(t, "Docker: "+utils.Warn("Unavailable"))
	} else {
		returnData.Content = containers.getContent(sourceConf.Ignore, sourceConf.IgnoreStopped, *sourceConf.WarnOnly, sourceConf)
	}
}

// getDockerContainers lists all containers. Running and restarting containers are
// inspected for the health and restart checks, but only if one of them is enabled.
func getDockerContainers(checks containerChecks) (containers containerList, err error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion(dockerMinAPI))
	if err != nil {
		return
	}

	ctx := context.Background()

	allContainers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return
	}
	containers.Runtime = "Docker"
	containers.Root = true
	now := time.Now()
	for _, container := range allContainers {
		detail := containerDetail{State: container.State}
		if checks.enabled() && needsInspect(container.State) {
			detail = inspectDetail(ctx, cli, container.ID, container.State)
		}

		containers.Containers = append(containers.Containers, containerStatus{
			Name:   dockerContainerName(container),
			Status: dockerRowStatus(detail, checks, now),
		})
	}

	return
}

// needsInspect reports whether a container in this list state can be unhealthy or
// in a restart loop
func needsInspect(state string) bool {
	state = strings.ToLower(state)

	return state == stateRunning || state == stateRestarting
}

func dockerContainerName(container types.Container) string {
	for _, name := range container.Names {
		name = strings.TrimPrefix(strings.TrimSpace(name), "/")
		if name != "" {
			return name
		}
	}

	id := strings.TrimSpace(container.ID)
	if len(id) > 12 {
		id = id[:12]
	}
	if id != "" {
		return id
	}

	return "unknown"
}
