# Roadmap

> [!NOTE]
> This is a list of ideas, not a promise. Not every item will necessarily be implemented, and the order, scope and
> details may change. Suggestions and pull requests are welcome via
> [GitHub Issues](https://github.com/SebastianOehm/unraid-motd/issues).

## New modules

| Feature | Data source | What it shows / warns on |
|---|---|---|
| **Array status** | `/var/local/emhttp/var.ini`, `disks.ini`, `/boot/config/parity-checks.log` | Array started or stopped; disabled or emulated disks; disks with read errors; parity check or rebuild progress and estimated finish; age and result of the last parity check |
| **Disk temperatures** | `disks.ini` (doesn't wake spun-down disks) | Per-disk temperature with warn/crit thresholds; spun-down state |
| **Notifications** | `/tmp/notifications/unread/` | Count of unread alerts and warnings, plus the latest few subjects |
| **Updates available** | Unraid OS update check, plugin versions, Docker update status | Pending OS, plugin and container image updates |
| **Mover / cache pool** | Mover status, cache pool usage | Whether the mover is running; warns when the cache will overflow before the next mover run |

## Extensions to existing modules

| Module | Idea |
|---|---|
| **sysinfo** | Warn/crit thresholds for RAM, load and swap |
| **docker** | Show unhealthy containers (Docker healthcheck) and restart loops, not just stopped containers |
| **network** | Warn when a link is down or below expected speed; bond status; optional WireGuard/Tailscale IP |
| **drives** | Projected days until full; ZFS pool health from `zpool status` |

## General

| Feature | Idea |
|---|---|
| **Output modes** | `--no-color`; `--json` for use in Home Assistant and scripts |
| **Notify via cron** | Optionally send motd warnings as Unraid notifications |
| **Tests** | Fixture-based tests for the Unraid state file parsers |
