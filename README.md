# Introduction

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![GitHub Releases](https://img.shields.io/github/v/release/SebastianOehm/unraid-motd)](https://github.com/SebastianOehm/unraid-motd/releases)
[![Last Commit](https://img.shields.io/github/last-commit/SebastianOehm/unraid-motd)](https://github.com/SebastianOehm/unraid-motd/commits/main/)
![GitHub Downloads (all assets, all releases)](https://img.shields.io/github/downloads/SebastianOehm/unraid-motd/total)
![GitHub Downloads (all assets, latest release)](https://img.shields.io/github/downloads/SebastianOehm/unraid-motd/latest/total)

Maintained fork of [dkaser/unraid-motd](https://github.com/dkaser/unraid-motd), which is no longer maintained.

Planned ideas are collected in the [roadmap](ROADMAP.md) (no guarantee that all of them will be implemented).

## Configuration

Configuration can be stored at `/boot/config/plugins/motd/config.yaml`.

To view the full configuration, run `motd --dump-config`

### Settings page

The configuration can also be edited in the webGUI under **Settings → MOTD**. If no `config.yaml` exists yet, the page shows the defaults.

- **Preview** renders the current editor content as it would appear at login, without saving it.
- **Save** validates the YAML and writes it to `/boot/config/plugins/motd/config.yaml`. Invalid YAML is rejected with the parser error, and the existing file is left unchanged.
- **Load defaults** replaces the editor content with the default configuration (not saved until you click **Save**).

The same checks are available on the command line: `motd --check-config -c <file>` validates a file, and `motd --preview --width 100 -c <file>` renders it without a terminal.

### Global

- `warnings_only` will hide content unless there is a warning, per-module override available
- `display` arrange module output in columns. Either one or two modules can be included per row.
- `border` draw borders around the output of each module.

```yaml
display:
  - [sysinfo]
  - [docker, cpu]
  - [services, networks]
  - [user-drives, system-drives]
```

### Header

- `show` display fancy header at beginning of output
- `use_hostname` if true, display the hostname. If false, display `custom_text`
- `font` select font to use for output. Run `figurine -l` to get a list of available fonts, or `figurine -sl Test` to view a sample of all fonts.

### Generic options

- `warnings_only` overrides global setting for that module only
- `border` overrides global setting for that module only

### CPU temperatures

- `warn`/`crit` are temperatures to consider warning or critical level

### Disk usage

- `warn`/`crit` percentage of disk space used before it is considered a warning or critical level, default is 70% and 90% respectively

### Docker

- `ignore` list of ignored container names
- `ignore_stopped` list of container names that will not trigger a warning when stopped (they are hidden from the output while stopped; other non-running states still trigger a warning)

### Docker Compose

Reports the status of docker-compose stacks managed by the [Compose Manager](https://forums.unraid.net/topic/114415-plugin-docker-compose-manager/) plugin. Stacks are read from disk rather than from running containers, so a stack that has been fully stopped with `docker compose down` (which removes its containers) is still reported as stopped, unlike the `docker` module.

- `projects_path` directory containing one subdirectory per compose stack, default `/boot/config/plugins/compose.manager/projects`
- `ignore` list of ignored stack names
- `ignore_stopped` list of stack names that will not trigger a warning when fully stopped (they are hidden from the output while stopped; a stack with only some containers running still triggers a warning)

### VMs

Reports the state of libvirt VMs from `virsh list --all` (opt-in, add `vms` to `display`). Running and idle VMs are OK. A shut-off VM is a warning unless it is listed in `ignore_stopped`; paused, crashed, suspended or shutting-down VMs are always a warning. The module is skipped when the VM service is disabled in Unraid or `virsh` is not installed, and shows "Unavailable" when `virsh` fails or takes longer than 5 seconds (for example while libvirt is still starting).

- `ignore` list of ignored VM names
- `ignore_stopped` list of VM names that will not trigger a warning when shut off (they are hidden from the output while shut off)

```yaml
global:
  display:
    - [sysinfo]
    - [docker, vms]
vms:
  ignore_stopped: [Windows 11]
```

### UPS (NUT)

Reports the UPS state from `upsc` of Network UPS Tools, for example the NUT plugin (opt-in, add `ups` to `display`). Shows status, battery charge, runtime and load; the title includes the UPS model when NUT reports it. Online is OK. On battery, replace battery, overload, bypass, off or an unknown status is a warning; low battery and forced shutdown are critical. The battery charge is a warning at or below `battery_warn` and critical at or below `battery_crit`. The module is skipped when `upsc` is not installed, and shows "Unavailable" when `upsc` fails (for example a wrong UPS name or an unreachable NUT server) or takes longer than 5 seconds.

- `ups` UPS to query, as `name@host` or `name@host:port`, default `ups@localhost` (`upsc -l` lists the names on a host)
- `battery_warn` / `battery_crit` battery charge in percent, default 50 and 20

```yaml
global:
  display:
    - [sysinfo]
    - [docker, ups]
ups:
  ups: ups@localhost
  battery_warn: 50
  battery_crit: 20
```

### Network

- `show_ipv4` / `show_ipv6` show IPv4/IPv6 addresses in output

### System information

No extra config
