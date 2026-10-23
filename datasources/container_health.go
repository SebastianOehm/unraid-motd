package datasources

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"
)

const (
	// stateRunning is the container state of a container that is up.
	stateRunning = "running"
	// stateRestarting is the container state while docker restarts a container.
	stateRestarting = "restarting"
	// healthUnhealthy is the health status of a container whose healthcheck fails.
	healthUnhealthy = "unhealthy"
)

// containerDetail is what the health and restart checks need to know about one container.
type containerDetail struct {
	State        string    // list state: running, exited, restarting, ...
	Health       string    // "", "starting", "healthy", "unhealthy"
	RestartCount int       // number of restarts docker has performed
	StartedAt    time.Time // zero if unknown
}

// containerChecks holds the module settings for the health and restart checks.
type containerChecks struct {
	Health        bool
	RestartWarn   int
	RestartWindow time.Duration
}

// newContainerChecks builds the checks from the module settings, with the restart
// window given in minutes.
func newContainerChecks(checkHealth bool, restartWarn int, restartWindow int) containerChecks {
	return containerChecks{
		Health:        checkHealth,
		RestartWarn:   restartWarn,
		RestartWindow: time.Duration(restartWindow) * time.Minute,
	}
}

// enabled reports whether any check needs the details of a container.
func (c containerChecks) enabled() bool {
	return c.Health || c.RestartWarn > 0
}

// isRestartLoop reports whether the container has restarted at least RestartWarn times
// and is either restarting right now or was started within RestartWindow.
func (c containerChecks) isRestartLoop(detail containerDetail, now time.Time) bool {
	if c.RestartWarn <= 0 || detail.RestartCount < c.RestartWarn {
		return false
	}

	switch strings.ToLower(detail.State) {
	case stateRestarting:
		return true
	case stateRunning:
		return !detail.StartedAt.IsZero() && now.Sub(detail.StartedAt) < c.RestartWindow
	default:
		return false
	}
}

// isUnhealthy reports whether the container is running but fails its healthcheck.
func (c containerChecks) isUnhealthy(detail containerDetail) bool {
	return c.Health &&
		strings.ToLower(detail.State) == stateRunning &&
		strings.ToLower(detail.Health) == healthUnhealthy
}

// dockerRowStatus returns the status shown for one docker container: a restart loop or
// an unhealthy healthcheck replaces the list state, otherwise the list state is kept.
func dockerRowStatus(detail containerDetail, checks containerChecks, now time.Time) string {
	switch {
	case checks.isRestartLoop(detail, now):
		return fmt.Sprintf("restart loop (%d)", detail.RestartCount)
	case checks.isUnhealthy(detail):
		return healthUnhealthy
	default:
		return detail.State
	}
}

// composeMember is one container of a compose stack.
type composeMember struct {
	Service string
	containerDetail
}

// composeStackStatus reduces the containers of a stack to one status, worst first:
// "exited" without containers, "restart loop: <services>", "partial" if a container
// is not running, "unhealthy: <services>" and otherwise "running".
func composeStackStatus(members []composeMember, checks containerChecks, now time.Time) string {
	if len(members) == 0 {
		return "exited"
	}

	var looping, stopped, unhealthy []string

	for _, member := range members {
		switch {
		case checks.isRestartLoop(member.containerDetail, now):
			looping = append(looping, member.Service)
		case strings.ToLower(member.State) != stateRunning:
			stopped = append(stopped, member.Service)
		case checks.isUnhealthy(member.containerDetail):
			unhealthy = append(unhealthy, member.Service)
		}
	}

	switch {
	case len(looping) > 0:
		return "restart loop: " + joinServices(looping)
	case len(stopped) > 0:
		return "partial"
	case len(unhealthy) > 0:
		return healthUnhealthy + ": " + joinServices(unhealthy)
	default:
		return stateRunning
	}
}

// joinServices returns the service names deduplicated, sorted and comma separated.
func joinServices(services []string) string {
	seen := make(map[string]bool, len(services))
	unique := make([]string, 0, len(services))

	for _, service := range services {
		if !seen[service] {
			seen[service] = true
			unique = append(unique, service)
		}
	}

	sort.Strings(unique)

	return strings.Join(unique, ", ")
}

// inspectDetail looks up the healthcheck result, restart count and start time of a
// container. If the inspect fails, only the list state is known.
func inspectDetail(ctx context.Context, cli *client.Client, id string, listState string) containerDetail {
	detail := containerDetail{State: listState}

	info, err := cli.ContainerInspect(ctx, id)
	if err != nil {
		log.Debugf("Could not inspect container %s: %v", id, err)

		return detail
	}

	if info.ContainerJSONBase == nil {
		return detail
	}

	detail.RestartCount = info.RestartCount

	if info.State == nil {
		return detail
	}

	if info.State.Health != nil {
		detail.Health = info.State.Health.Status
	}

	// Docker reports a container that never started as year 1
	if started, parseErr := time.Parse(time.RFC3339Nano, info.State.StartedAt); parseErr == nil && started.Year() > 1 {
		detail.StartedAt = started
	}

	return detail
}
