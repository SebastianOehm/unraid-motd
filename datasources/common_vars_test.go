package datasources

import (
	"fmt"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v2"
)

const (
	fakeTitle   = "Fake Slow"
	fakeContent = "fast module content"
)

// registerFake registers a fake module under name and restores the registry afterwards.
// The module waits for either its delay or the release channel (closed on cleanup, or never
// when delay is 0 and block is false), then always sends a result.
func registerFake(t *testing.T, name, title string, moduleTimeout *int, delay time.Duration, block bool, content string) {
	t.Helper()
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	orig, existed := modules[name]
	t.Cleanup(func() {
		if existed {
			modules[name] = orig
		} else {
			delete(modules, name)
		}
	})
	modules[name] = moduleDef{
		run: func(out chan<- SourceReturn, _ *Conf) {
			if block {
				<-release
			} else if delay > 0 {
				select {
				case <-time.After(delay):
				case <-release:
				}
			}
			out <- SourceReturn{Content: content}
		},
		base: func(c *Conf) ConfBase {
			base := ConfBase{Timeout: moduleTimeout}
			base.Load(c)

			return base
		},
		title: title,
	}
}

func intPtr(i int) *int { return &i }

func requireTimedOut(t *testing.T, name string, ret SourceReturn) {
	t.Helper()
	if !strings.Contains(ret.Content, "Timed out") {
		t.Errorf("%s: content %q does not contain \"Timed out\"", name, ret.Content)
	}
	var mna *ModuleNotAvailable
	if !errors.As(ret.Error, &mna) {
		t.Fatalf("%s: error %v is not *ModuleNotAvailable", name, ret.Error)
	}
	if !errors.Is(mna.ParentError, ErrTimeout) {
		t.Errorf("%s: ParentError = %v, want ErrTimeout", name, mna.ParentError)
	}
	if mna.Name != name {
		t.Errorf("%s: Name = %q", name, mna.Name)
	}
	if _, ok := ret.Error.(UnavailableError); !ok {
		t.Errorf("%s: error does not implement UnavailableError", name)
	}
}

func TestRunSourcesTimeoutSlowAndFast(t *testing.T) {
	registerFake(t, "test-slow", fakeTitle, nil, 0, true, "never")
	registerFake(t, "test-fast", "Fake Fast", nil, 0, false, fakeContent)
	conf := &Conf{}
	conf.Init()
	conf.Timeout = 1

	start := time.Now()
	order, out := RunSources([]string{"test-slow", "test-fast"}, conf)
	elapsed := time.Since(start)

	if elapsed > 1500*time.Millisecond {
		t.Errorf("RunSources took %v, want < 1.5s", elapsed)
	}
	if elapsed < 900*time.Millisecond {
		t.Errorf("RunSources returned after %v, before the timeout", elapsed)
	}
	if len(order) != 2 || order[0] != "test-slow" || order[1] != "test-fast" {
		t.Errorf("order = %v", order)
	}
	slow := out["test-slow"]
	requireTimedOut(t, "test-slow", slow)
	if !strings.Contains(slow.Content, fakeTitle) {
		t.Errorf("content %q does not contain title %q", slow.Content, fakeTitle)
	}
	fast := out["test-fast"]
	if fast.Error != nil {
		t.Errorf("fast module error = %v", fast.Error)
	}
	if fast.Content != fakeContent {
		t.Errorf("fast module content = %q, want %q", fast.Content, fakeContent)
	}
}

func TestRunSourcesTimeoutOverrideAndGlobal(t *testing.T) {
	tests := []struct {
		name          string
		global        int
		module        *int
		block         bool
		delay         time.Duration
		wantTimeout   bool
		minDur, maxDu time.Duration
	}{
		{
			name: "module timeout 0 overrides global 1", global: 1, module: intPtr(0),
			delay: 1500 * time.Millisecond, wantTimeout: false,
			minDur: 1400 * time.Millisecond, maxDu: 2500 * time.Millisecond,
		},
		{
			name: "module timeout 1 overrides global 0", global: 0, module: intPtr(1),
			block: true, wantTimeout: true,
			minDur: 900 * time.Millisecond, maxDu: 1500 * time.Millisecond,
		},
		{
			name: "global 0 waits for slow module", global: 0, module: nil,
			delay: 1200 * time.Millisecond, wantTimeout: false,
			minDur: 1100 * time.Millisecond, maxDu: 2200 * time.Millisecond,
		},
		{
			name: "negative global means no limit", global: -5, module: nil,
			delay: 1200 * time.Millisecond, wantTimeout: false,
			minDur: 1100 * time.Millisecond, maxDu: 2200 * time.Millisecond,
		},
	}
	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			registerFake(t, "test-mod", "Fake Mod", tcase.module, tcase.delay, tcase.block, "done")
			conf := &Conf{}
			conf.Init()
			conf.Timeout = tcase.global

			start := time.Now()
			_, out := RunSources([]string{"test-mod"}, conf)
			elapsed := time.Since(start)

			if elapsed < tcase.minDur || elapsed > tcase.maxDu {
				t.Errorf("elapsed %v not in [%v, %v]", elapsed, tcase.minDur, tcase.maxDu)
			}
			ret := out["test-mod"]
			if tcase.wantTimeout {
				requireTimedOut(t, "test-mod", ret)

				return
			}
			if ret.Error != nil || ret.Content != "done" {
				t.Errorf("got content %q error %v, want normal result", ret.Content, ret.Error)
			}
		})
	}
}

func TestRunSourcesTimeoutParallel(t *testing.T) {
	registerFake(t, "test-slow1", "Slow One", nil, 0, true, "never")
	registerFake(t, "test-slow2", "Slow Two", nil, 0, true, "never")
	conf := &Conf{}
	conf.Init()
	conf.Timeout = 1

	start := time.Now()
	_, out := RunSources([]string{"test-slow1", "test-slow2"}, conf)
	elapsed := time.Since(start)

	if elapsed > 1800*time.Millisecond {
		t.Errorf("RunSources took %v, timeouts appear to be summed", elapsed)
	}
	requireTimedOut(t, "test-slow1", out["test-slow1"])
	requireTimedOut(t, "test-slow2", out["test-slow2"])
}

func TestRunSourcesUnknownModule(t *testing.T) {
	registerFake(t, "test-fast", "Fake Fast", nil, 0, false, fakeContent)
	conf := &Conf{}
	conf.Init()

	order, out := RunSources([]string{"does-not-exist", "test-fast"}, conf)

	if len(order) != 1 || order[0] != "test-fast" {
		t.Errorf("order = %v, want [test-fast]", order)
	}
	if _, ok := out["does-not-exist"]; ok {
		t.Error("unknown module present in result map")
	}
	if out["test-fast"].Content != fakeContent {
		t.Errorf("fast content = %q", out["test-fast"].Content)
	}
}

func TestConfTimeoutDefault(t *testing.T) {
	conf, err := NewConfFromFile(filepath.Join(t.TempDir(), "missing.yaml"), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conf.Timeout != 10 {
		t.Errorf("Timeout = %d, want 10", conf.Timeout)
	}
}

func TestConfTimeoutFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "motd.yaml")
	content := "global:\n  timeout: 3\nservices:\n  timeout: 0\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	conf, err := NewConfFromFile(path, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conf.Timeout != 3 {
		t.Fatalf("Timeout = %d, want 3", conf.Timeout)
	}
	conf.Services.ConfBase.Load(&conf)
	conf.Docker.ConfBase.Load(&conf)
	if got := conf.Services.GetTimeout(); got != 0 {
		t.Errorf("services GetTimeout = %v, want 0", got)
	}
	if got := conf.Docker.GetTimeout(); got != 3*time.Second {
		t.Errorf("docker GetTimeout = %v, want 3s", got)
	}
}

func TestConfBaseGetTimeout(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want time.Duration
	}{
		{"positive", 7, 7 * time.Second},
		{"zero", 0, 0},
		{"negative", -3, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ConfBase{Timeout: intPtr(tc.in)}
			if got := cfg.GetTimeout(); got != tc.want {
				t.Errorf("GetTimeout() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConfTimeoutMarshal(t *testing.T) {
	conf := Conf{}
	conf.Init()
	out, err := yaml.Marshal(conf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), "timeout: 10") {
		t.Errorf("marshalled default config lacks \"timeout: 10\":\n%s", out)
	}
}

// A finished module that is collected after a slow one must not be reported as timed out,
// even though its deadline has already passed when the collector reaches it.
func TestRunSourcesTimeoutLateCollectedFinished(t *testing.T) {
	const fastCount = 30
	registerFake(t, "test-block", "Blocker", nil, 0, true, "never")
	runList := []string{"test-block"}
	for i := range fastCount {
		name := fmt.Sprintf("test-fast-%d", i)
		registerFake(t, name, name, nil, 0, false, fakeContent)
		runList = append(runList, name)
	}
	conf := &Conf{}
	conf.Init()
	conf.Timeout = 1

	order, out := RunSources(runList, conf)

	if len(order) != len(runList) {
		t.Fatalf("order has %d entries, want %d", len(order), len(runList))
	}
	requireTimedOut(t, "test-block", out["test-block"])
	for _, name := range runList[1:] {
		ret := out[name]
		if ret.Error != nil || ret.Content != fakeContent {
			t.Errorf("%s: finished module reported content %q error %v", name, ret.Content, ret.Error)
		}
	}
}
