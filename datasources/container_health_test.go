package datasources

import (
	"testing"
	"time"
)

var healthTestNow = time.Date(2025, time.January, 15, 12, 0, 0, 0, time.UTC)

const healthTestWindow = 10 * time.Minute

type dockerRowStatusCase struct {
	name   string
	detail containerDetail
	checks containerChecks
	want   string
}

var dockerRowStatusCases = []dockerRowStatusCase{
	{
		name:   "healthy stays state",
		detail: containerDetail{State: "running", Health: "healthy"},
		checks: containerChecks{Health: true},
		want:   "running",
	},
	{
		name:   "unhealthy",
		detail: containerDetail{State: "running", Health: "unhealthy"},
		checks: containerChecks{Health: true},
		want:   "unhealthy",
	},
	{
		name:   "unhealthy matches case-insensitively",
		detail: containerDetail{State: "Running", Health: "Unhealthy"},
		checks: containerChecks{Health: true},
		want:   "unhealthy",
	},
	{
		name:   "starting is not a problem",
		detail: containerDetail{State: "running", Health: "starting"},
		checks: containerChecks{Health: true},
		want:   "running",
	},
	{
		name:   "unhealthy with health check off",
		detail: containerDetail{State: "running", Health: "unhealthy"},
		checks: containerChecks{},
		want:   "running",
	},
	{
		name:   "unhealthy but exited stays exited",
		detail: containerDetail{State: "exited", Health: "unhealthy"},
		checks: containerChecks{Health: true},
		want:   "exited",
	},
	{
		name:   "original case preserved",
		detail: containerDetail{State: "Up", Health: "healthy"},
		checks: containerChecks{Health: true},
		want:   "Up",
	},
	{
		name:   "loop via restarting state",
		detail: containerDetail{State: "restarting", RestartCount: 5},
		checks: containerChecks{RestartWarn: 3, RestartWindow: healthTestWindow},
		want:   "restart loop (5)",
	},
	{
		name: "loop via running with recent start",
		detail: containerDetail{
			State: "running", RestartCount: 4, StartedAt: healthTestNow.Add(-time.Minute),
		},
		checks: containerChecks{RestartWarn: 3, RestartWindow: healthTestWindow},
		want:   "restart loop (4)",
	},
	{
		name: "loop takes precedence over unhealthy",
		detail: containerDetail{
			State: "running", Health: "unhealthy", RestartCount: 6, StartedAt: healthTestNow.Add(-time.Minute),
		},
		checks: containerChecks{Health: true, RestartWarn: 3, RestartWindow: healthTestWindow},
		want:   "restart loop (6)",
	},
	{
		name: "high count but start outside window",
		detail: containerDetail{
			State: "running", RestartCount: 50, StartedAt: healthTestNow.Add(-time.Hour),
		},
		checks: containerChecks{RestartWarn: 3, RestartWindow: healthTestWindow},
		want:   "running",
	},
	{
		name: "count below threshold",
		detail: containerDetail{
			State: "running", RestartCount: 2, StartedAt: healthTestNow.Add(-time.Minute),
		},
		checks: containerChecks{RestartWarn: 3, RestartWindow: healthTestWindow},
		want:   "running",
	},
	{
		name:   "restart warn zero disables loop",
		detail: containerDetail{State: "restarting", RestartCount: 99},
		checks: containerChecks{RestartWarn: 0, RestartWindow: healthTestWindow},
		want:   "restarting",
	},
	{
		name:   "zero started at while running is not a loop",
		detail: containerDetail{State: "running", RestartCount: 10},
		checks: containerChecks{RestartWarn: 3, RestartWindow: healthTestWindow},
		want:   "running",
	},
	{
		name: "started exactly at window boundary is not a loop",
		detail: containerDetail{
			State: "running", RestartCount: 10, StartedAt: healthTestNow.Add(-healthTestWindow),
		},
		checks: containerChecks{RestartWarn: 3, RestartWindow: healthTestWindow},
		want:   "running",
	},
	{
		name: "started just inside window is a loop",
		detail: containerDetail{
			State: "running", RestartCount: 10, StartedAt: healthTestNow.Add(-healthTestWindow + time.Second),
		},
		checks: containerChecks{RestartWarn: 3, RestartWindow: healthTestWindow},
		want:   "restart loop (10)",
	},
	{
		name:   "count equal to threshold counts",
		detail: containerDetail{State: "restarting", RestartCount: 3},
		checks: containerChecks{RestartWarn: 3, RestartWindow: healthTestWindow},
		want:   "restart loop (3)",
	},
}

func TestDockerRowStatus(t *testing.T) {
	t.Parallel()

	for _, testCase := range dockerRowStatusCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := dockerRowStatus(testCase.detail, testCase.checks, healthTestNow)
			if got != testCase.want {
				t.Errorf("dockerRowStatus() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestIsRestartLoop(t *testing.T) {
	t.Parallel()

	checks := containerChecks{RestartWarn: 3, RestartWindow: healthTestWindow}

	if !checks.isRestartLoop(containerDetail{State: "Restarting", RestartCount: 3}, healthTestNow) {
		t.Error("expected restarting state with count at threshold to be a loop")
	}

	if checks.isRestartLoop(containerDetail{State: "exited", RestartCount: 30}, healthTestNow) {
		t.Error("exited container must not be a loop")
	}
}

type containerChecksEnabledCase struct {
	name   string
	checks containerChecks
	want   bool
}

var containerChecksEnabledCases = []containerChecksEnabledCase{
	{name: "none", checks: containerChecks{}, want: false},
	{name: "health only", checks: containerChecks{Health: true}, want: true},
	{name: "restart warn only", checks: containerChecks{RestartWarn: 3}, want: true},
	{name: "both", checks: containerChecks{Health: true, RestartWarn: 3}, want: true},
	{name: "window alone does not enable", checks: containerChecks{RestartWindow: healthTestWindow}, want: false},
}

func TestContainerChecksEnabled(t *testing.T) {
	t.Parallel()

	for _, testCase := range containerChecksEnabledCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.checks.enabled(); got != testCase.want {
				t.Errorf("enabled() = %v, want %v", got, testCase.want)
			}
		})
	}
}

type composeStackStatusCase struct {
	name    string
	members []composeMember
	checks  containerChecks
	want    string
}

func composeTestMember(service, state, health string, restarts int, started time.Time) composeMember {
	return composeMember{
		Service: service,
		containerDetail: containerDetail{
			State: state, Health: health, RestartCount: restarts, StartedAt: started,
		},
	}
}

var composeStackChecks = containerChecks{Health: true, RestartWarn: 3, RestartWindow: healthTestWindow}

var composeStackStatusCases = []composeStackStatusCase{
	{
		name:    "empty",
		members: nil,
		checks:  composeStackChecks,
		want:    "exited",
	},
	{
		name: "all running",
		members: []composeMember{
			composeTestMember("web", "running", "healthy", 0, time.Time{}),
			composeTestMember("db", "running", "", 0, time.Time{}),
		},
		checks: composeStackChecks,
		want:   "running",
	},
	{
		name: "partial",
		members: []composeMember{
			composeTestMember("web", "running", "", 0, time.Time{}),
			composeTestMember("db", "exited", "", 0, time.Time{}),
		},
		checks: composeStackChecks,
		want:   "partial",
	},
	{
		name: "loop beats partial",
		members: []composeMember{
			composeTestMember("web", "restarting", "", 5, time.Time{}),
			composeTestMember("db", "exited", "", 0, time.Time{}),
		},
		checks: composeStackChecks,
		want:   "restart loop: web",
	},
	{
		name: "partial beats unhealthy",
		members: []composeMember{
			composeTestMember("web", "running", "unhealthy", 0, time.Time{}),
			composeTestMember("db", "exited", "", 0, time.Time{}),
		},
		checks: composeStackChecks,
		want:   "partial",
	},
	{
		name: "unhealthy names sorted and deduplicated",
		members: []composeMember{
			composeTestMember("web", "running", "unhealthy", 0, time.Time{}),
			composeTestMember("api", "running", "unhealthy", 0, time.Time{}),
			composeTestMember("web", "running", "unhealthy", 0, time.Time{}),
			composeTestMember("db", "running", "healthy", 0, time.Time{}),
		},
		checks: composeStackChecks,
		want:   "unhealthy: api, web",
	},
	{
		name: "unhealthy ignored with health check off",
		members: []composeMember{
			composeTestMember("web", "running", "unhealthy", 0, time.Time{}),
		},
		checks: containerChecks{RestartWarn: 3, RestartWindow: healthTestWindow},
		want:   "running",
	},
	{
		name: "loop names sorted",
		members: []composeMember{
			composeTestMember("zeta", "restarting", "", 9, time.Time{}),
			composeTestMember("alpha", "running", "", 4, healthTestNow.Add(-time.Minute)),
			composeTestMember("mid", "running", "", 0, time.Time{}),
		},
		checks: composeStackChecks,
		want:   "restart loop: alpha, zeta",
	},
}

func TestComposeStackStatus(t *testing.T) {
	t.Parallel()

	for _, testCase := range composeStackStatusCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := composeStackStatus(testCase.members, testCase.checks, healthTestNow)
			if got != testCase.want {
				t.Errorf("composeStackStatus() = %q, want %q", got, testCase.want)
			}
		})
	}
}
