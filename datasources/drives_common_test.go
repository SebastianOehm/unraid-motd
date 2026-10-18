package datasources

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/shirou/gopsutil/v3/disk"
)

func TestDedupePartitions(t *testing.T) {
	part := func(device string, mountpoint string) disk.PartitionStat {
		return disk.PartitionStat{Device: device, Mountpoint: mountpoint}
	}

	tests := []struct {
		name  string
		parts []disk.PartitionStat
		want  []disk.PartitionStat
	}{
		{
			name:  "distinct devices are kept in order",
			parts: []disk.PartitionStat{part("/dev/md1p1", "/mnt/disk1"), part("/dev/md2p1", "/mnt/disk2")},
			want:  []disk.PartitionStat{part("/dev/md1p1", "/mnt/disk1"), part("/dev/md2p1", "/mnt/disk2")},
		},
		{
			name: "bind mount after the drive is dropped",
			parts: []disk.PartitionStat{
				part("/dev/nvme0n1p1", "/mnt/cache"),
				part("/dev/nvme0n1p1", "/usr/local/emhttp/plugins/example/data"),
			},
			want: []disk.PartitionStat{part("/dev/nvme0n1p1", "/mnt/cache")},
		},
		{
			name: "mount under /mnt wins over an earlier bind mount",
			parts: []disk.PartitionStat{
				part("/dev/nvme0n1p1", "/tmp/x"),
				part("/dev/md1p1", "/mnt/disk1"),
				part("/dev/nvme0n1p1", "/mnt/cache"),
			},
			want: []disk.PartitionStat{part("/dev/nvme0n1p1", "/mnt/cache"), part("/dev/md1p1", "/mnt/disk1")},
		},
		{
			name: "shortest path wins outside /mnt",
			parts: []disk.PartitionStat{
				part("/dev/sdb1", "/srv/data/nested"),
				part("/dev/sdb1", "/srv/data"),
			},
			want: []disk.PartitionStat{part("/dev/sdb1", "/srv/data")},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := dedupePartitions(testCase.parts)
			if !reflect.DeepEqual(got, testCase.want) {
				t.Errorf("dedupePartitions() = %#v, want %#v", got, testCase.want)
			}
		})
	}
}

// installFakeZFS puts a fake `zfs` script with the given body first in PATH.
func installFakeZFS(t *testing.T, body string) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("fake zfs shell script requires a POSIX system")
	}

	dir := t.TempDir()
	script := "#!/bin/sh\n" + body + "\n"

	if err := os.WriteFile(filepath.Join(dir, "zfs"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake zfs: %v", err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func newDrivesConf() *ConfDrives {
	conf := &ConfDrives{}
	conf.Init()

	return conf
}

// zfsListLine builds a fake `zfs list` output line with real tabs.
func zfsListLine(name string, used, avail int) string {
	return fmt.Sprintf(
		`printf '%s\t-\t%d\t%d\t/mnt/pool\toff\tfilesystem\t-\t0\t0\t0\t0\t0\n'`,
		name, used, avail,
	)
}

// runProcessDriveZFS calls processDriveZFS and fails the test on panic.
func runProcessDriveZFS(t *testing.T, conf *ConfDrives) (status string, percent int, err error) {
	t.Helper()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("processDriveZFS panicked: %v", r)
		}
	}()

	status, percent, _, _, err = processDriveZFS(conf, "/mnt/pool", "o")

	return status, percent, err
}

func TestProcessDriveZFSCommandFails(t *testing.T) {
	installFakeZFS(t, "echo 'cannot open dataset' >&2\nexit 1")

	_, _, err := runProcessDriveZFS(t, newDrivesConf())
	if err == nil {
		t.Fatal("expected error when zfs fails, got nil")
	}

	if !errors.Is(err, ErrZFS) {
		t.Fatalf("expected ErrZFS, got %v", err)
	}
}

func TestProcessDriveZFSZeroSize(t *testing.T) {
	installFakeZFS(t, zfsListLine("pool", 0, 0))

	status, percent, err := runProcessDriveZFS(t, newDrivesConf())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if percent != 0 {
		t.Errorf("percent = %d, want 0", percent)
	}

	if status != "o" {
		t.Errorf("status = %q, want %q", status, "o")
	}
}

func TestProcessDriveZFSUsage(t *testing.T) {
	tests := []struct {
		name        string
		used, avail int
		percent     int
		status      string
	}{
		{"ok", 30, 70, 30, "o"},
		{"critical", 95, 5, 95, "e"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installFakeZFS(t, zfsListLine("pool", tt.used, tt.avail))

			status, percent, err := runProcessDriveZFS(t, newDrivesConf())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if percent != tt.percent {
				t.Errorf("percent = %d, want %d", percent, tt.percent)
			}

			if status != tt.status {
				t.Errorf("status = %q, want %q", status, tt.status)
			}
		})
	}
}

func TestProcessDriveZFSSkipsChildDataset(t *testing.T) {
	installFakeZFS(t, zfsListLine("pool/data", 30, 70))

	_, _, err := runProcessDriveZFS(t, newDrivesConf())
	if !errors.Is(err, ErrZFS) {
		t.Fatalf("expected ErrZFS, got %v", err)
	}
}
