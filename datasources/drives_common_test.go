package datasources

import (
	"reflect"
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
