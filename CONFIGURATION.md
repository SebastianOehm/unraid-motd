# Configuration reference

This page describes every option in `config.yaml` in detail. For a short overview see the [README](README.md#configuration).

## Where the configuration lives

The configuration is read from `/boot/config/plugins/motd/config.yaml`. A different file can be passed with `-c <file>` or the `CONFIG_FILE` environment variable.

The file is optional. Every option has a default, and the file only needs to contain the options you want to change. Options you leave out keep their default value. A list you set (for example `display` or `services.monitor`) replaces the default list completely, it is not merged with it.

Unknown keys are ignored without an error, so a typo such as `warning_only` silently has no effect. Run `motd --dump-config` to see the configuration as it is actually applied, including all defaults.

The configuration can also be edited in the webGUI under **Settings → MOTD**, see [Settings page](README.md#settings-page).

## Full default configuration

This is the configuration used when no `config.yaml` exists. Modules that are not listed in `global.display` (`compose`, `vms`, `ups`, `gpu`) are not run, but their options are shown here with their defaults.

```yaml
global:
  warnings_only: true
  display:
    - [sysinfo]
    - [docker, cpu]
    - [services, networks]
    - [user-drives, system-drives]
  padding: 1
  timeout: 10
  table_width: 60
  border: true
header:
  show: true
  use_hostname: true
  custom_text: Custom
  font: Banner.flf
cpu:
  warn: 70
  crit: 90
docker:
  ignore: []
  ignore_stopped: []
  check_health: true
  restart_warn: 3
  restart_window: 10
compose:
  projects_path: /boot/config/plugins/compose.manager/projects
  ignore: []
  ignore_stopped: []
  check_health: true
  restart_warn: 3
  restart_window: 10
vms:
  ignore: []
  ignore_stopped: []
ups:
  ups: ups@localhost
  battery_warn: 50
  battery_crit: 20
gpu:
  warn: 80
  crit: 90
  show_processes: true
sysinfo:
  border: false
  ram_warn: 90
  ram_crit: 95
  swap_warn: 50
  swap_crit: 80
  load_warn: 100
  load_crit: 200
user-drives:
  warnings_only: false
  warn: 70
  crit: 90
  show_zfs_datasets: false
system-drives:
  warnings_only: false
  warn: 70
  crit: 90
  show_zfs_datasets: false
network:
  show_ipv4: true
  show_ipv6: false
services:
  monitor: [nginx, samba, tailscale, nfsd, sshd, docker]
```

## Status levels

Most modules show a status in their title: **OK** (green), **Warning** (yellow) or **Critical** (red). Individual rows are colored the same way. A module that cannot read its data shows **Unavailable** instead. Run `motd --hide-unavailable` (or set `HIDE_UNAVAILABLE=true`) to hide unavailable modules completely.

A module that runs over its [timeout](#global) shows **Timed out** in yellow instead, for example "Services: Timed out". It counts as unavailable, so `--hide-unavailable` hides it too and `--debug` logs it. The module is not cancelled, motd just stops waiting for it. All modules run in parallel and every timeout counts from the same start, so the slowest allowed module bounds the total wait, the times do not add up.

`vms`, `ups` and `gpu` also limit their external command to 5 seconds. When it hits that limit the module shows "Unavailable", not "Timed out". A module timeout below 5 seconds cuts them off earlier, a higher one does not extend that limit.

```yaml
global:
  timeout: 5
services:
  timeout: 15
```

## `global`

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `warnings_only` | bool | `true` | Only show rows that have a warning or critical state. The module title with its overall status is always shown. Can be overridden per module. |
| `display` | list of lists | see above | Which modules run and how they are arranged. See [Layout](#layout-display). |
| `padding` | int | `1` | Number of spaces between modules that share a row. |
| `timeout` | int | `10` | Seconds each module may take before it is shown as "Timed out" (see [Status levels](#status-levels)). `0` or a negative value means no limit. Can be overridden per module, so `timeout: 0` in a module section disables the limit for just that module even when `global.timeout` is set. |
| `table_width` | int | `60` | Fixed width of each module box in characters. `0` lets each box size itself to its content. The width is reduced to the terminal width if the terminal is narrower. Can be overridden per module. |
| `border` | bool | `true` | Draw a border around each module and lines between its columns. Can be overridden per module. |

### Layout (`display`)

`display` is a list of rows, and each row is a list of module names. Modules in the same row are printed side by side, rows are printed from top to bottom. A module only runs if it appears in `display`.

```yaml
global:
  display:
    - [sysinfo]
    - [docker, cpu]
    - [services, networks]
    - [user-drives, system-drives]
```

Available module names:

| Name | Module | Options section |
| --- | --- | --- |
| `sysinfo` | [System information](#sysinfo) | `sysinfo` |
| `cpu` | [CPU temperatures](#cpu) | `cpu` |
| `docker` | [Docker containers](#docker) | `docker` |
| `compose` | [Docker Compose stacks](#compose) | `compose` |
| `vms` | [Virtual machines](#vms) | `vms` |
| `ups` | [UPS (NUT)](#ups) | `ups` |
| `gpu` | [NVIDIA GPUs](#gpu) | `gpu` |
| `services` | [Services](#services) | `services` |
| `networks` | [Network interfaces](#network) | `network` (singular) |
| `user-drives` | [User drives](#user-drives-and-system-drives) | `user-drives` |
| `system-drives` | [System drives](#user-drives-and-system-drives) | `system-drives` |

Note that the network module is called `networks` in `display`, but its options live under `network`.

Unknown module names in `display` are skipped. A row may hold more than two modules, but with the default `table_width` of 60 two modules already need about 120 columns. On a narrow terminal, use one module per row or lower `table_width`.

## Options available in every module

These options can be set in any module section and override the matching `global` value for that module only.

| Option | Type | Description |
| --- | --- | --- |
| `warnings_only` | bool | Override `global.warnings_only`. |
| `border` | bool | Override `global.border`. |
| `table_width` | int | Override `global.table_width`. |
| `timeout` | int | Override `global.timeout`. |

Two modules have different built-in defaults: `sysinfo` has `border: false`, and `user-drives` / `system-drives` have `warnings_only: false` (all drives are listed). Set the option in the module section to change that.

```yaml
global:
  warnings_only: true
docker:
  warnings_only: false   # list all containers, also running ones
sysinfo:
  border: true           # draw a border around sysinfo too
```

## `header`

A large ASCII-art banner printed above the modules.

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `show` | bool | `true` | Print the banner. |
| `use_hostname` | bool | `true` | Print the server's hostname. If `false`, `custom_text` is printed instead. |
| `custom_text` | string | `Custom` | Text to print when `use_hostname` is `false`. |
| `font` | string | `Banner.flf` | FIGlet font. Run `figurine -l` to list the available fonts, or `figurine -sl Test` to preview all of them. An unknown font prints no banner. |

```yaml
header:
  use_hostname: false
  custom_text: Tower
  font: Standard.flf
```

## Modules

### `sysinfo`

Shows the Unraid version, kernel, uptime, CPU usage (user, system, IO wait, idle), load average and RAM usage. A swap row is shown below RAM when the system has swap (Unraid has none by default). RAM and swap are shown as "N% used (X of Y)". RAM used is the total minus the available memory (`MemAvailable`). The version, kernel, uptime, load and RAM rows show "unavailable" when they cannot be read. The swap row is left out when RAM cannot be read.

The RAM, swap and load values are colored when they reach a threshold: yellow at or above the warning threshold, red at or above the critical threshold. Percentages are rounded down, so 89.9 % does not reach `ram_warn: 90`. Critical wins over warning. Values below the warning threshold are not colored. Each of the three load averages (1, 5 and 15 minutes) is colored on its own. The module has no status in its title, and `warnings_only` has no effect here.

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `ram_warn` | int | `90` | RAM used in percent at or above which the RAM value is yellow. `0` disables it. |
| `ram_crit` | int | `95` | RAM used in percent at or above which the RAM value is red. `0` disables it. |
| `swap_warn` | int | `50` | Swap used in percent at or above which the swap value is yellow. `0` disables it. |
| `swap_crit` | int | `80` | Swap used in percent at or above which the swap value is red. `0` disables it. |
| `load_warn` | int | `100` | Load average as a percentage of the logical CPUs (threads) at or above which a load value is yellow. `100` on a CPU with 8 threads means a load of 8.0. `0` disables it. |
| `load_crit` | int | `200` | Same as `load_warn`, but red. `0` disables it. |

The load can exceed 100 %. The load average counts processes that are running or waiting, including processes waiting for disk I/O. This is common on Unraid during a parity check or the mover.

```yaml
sysinfo:
  ram_warn: 80
  ram_crit: 90
  load_warn: 0     # no coloring for the load
  load_crit: 0
```

The [common options](#options-available-in-every-module) also apply.

### `cpu`

Shows the temperature of each CPU core (Intel `coretemp`) or of the CPU sensors (AMD `k10temp`). Shows "Unavailable" when no sensor is found.

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `warn` | int | `70` | Temperature in °C at or above which a core is a warning. |
| `crit` | int | `90` | Temperature in °C at or above which a core is critical. |

With `warnings_only`, only cores at or above `warn` are listed.

### `docker`

Shows the state of Docker containers from the Docker API. Running and created containers are OK. Any other state (exited, paused, restarting, dead) is shown in red. A running container whose Docker healthcheck reports unhealthy is shown as `unhealthy` in red. A container stuck in a restart loop is shown as `restart loop (N)` in red, where N is its restart count. A restart loop takes precedence over `unhealthy`. A container whose healthcheck is still `starting` is not a problem. The title is OK when no container has a problem, Warning when some do, and Critical when none of the containers is running. Shows "Unavailable" when Docker is not running.

A container counts as a restart loop when its restart count is at or above `restart_warn` and it is either restarting right now or was started less than `restart_window` minutes ago. The restart count is the number of restarts by Docker's restart policy since the container was last started manually. Containers are only inspected (one extra Docker API call each) when they are running or restarting and at least one of the checks is enabled. If inspecting a container fails, its plain state is shown.

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `ignore` | list of strings | `[]` | Container names that are never shown and never cause a warning. |
| `ignore_stopped` | list of strings | `[]` | Container names that may be stopped (exited) without a warning. They are hidden while stopped. Any other non-running state still causes a warning. |
| `check_health` | bool | `true` | Flag running containers whose Docker healthcheck reports unhealthy. |
| `restart_warn` | int | `3` | Restart count from which a container counts as a restart loop. `0` disables the restart loop check. |
| `restart_window` | int (minutes) | `10` | A container with enough restarts is only a restart loop while it is restarting or was started less than this many minutes ago. |

`ignore` hides any container, including from the health and restart checks. `ignore_stopped` only hides exited containers.

```yaml
docker:
  ignore: [some-test-container]
  ignore_stopped: [backup, handbrake]
  check_health: true
  restart_warn: 5
  restart_window: 15
```

### `compose`

Reports docker-compose stacks managed by the [Compose Manager](https://forums.unraid.net/topic/114415-plugin-docker-compose-manager/) plugin (opt-in, add `compose` to `display`). Stacks are read from disk instead of from running containers, so a stack stopped with `docker compose down` (which removes its containers) is still reported as stopped. A stack where only some containers run is a warning.

Each stack shows one status. If several apply, the worst one wins, in this order: `exited` (no containers), `restart loop: <services>`, `partial`, `unhealthy: <services>`, `running`. The services are named by their compose service (or the container name if the label is missing), sorted and separated by commas. The health and restart loop checks work as described for the [`docker`](#docker) module.

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `projects_path` | string | `/boot/config/plugins/compose.manager/projects` | Directory with one subdirectory per stack. |
| `ignore` | list of strings | `[]` | Stack names that are never shown. |
| `ignore_stopped` | list of strings | `[]` | Stack names that may be fully stopped without a warning. They are hidden while stopped. A partially running stack still causes a warning. |
| `check_health` | bool | `true` | Flag stacks with running containers whose Docker healthcheck reports unhealthy. |
| `restart_warn` | int | `3` | Restart count from which a container counts as a restart loop. `0` disables the restart loop check. |
| `restart_window` | int (minutes) | `10` | A container with enough restarts is only a restart loop while it is restarting or was started less than this many minutes ago. |

```yaml
compose:
  ignore_stopped: [dev-stack]
  restart_warn: 0
```

### `vms`

Reports libvirt VMs from `virsh list --all` (opt-in, add `vms` to `display`). Running and idle VMs are OK. A shut-off VM is a warning unless it is in `ignore_stopped`. Paused, crashed, suspended or shutting-down VMs are always a warning.

The module is skipped when the VM service is disabled in Unraid or `virsh` is not installed, and shows "Unavailable" when `virsh` fails or takes longer than 5 seconds (for example while libvirt is still starting).

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `ignore` | list of strings | `[]` | VM names that are never shown. |
| `ignore_stopped` | list of strings | `[]` | VM names that may be shut off without a warning. They are hidden while shut off. |

VM names containing spaces work as normal YAML strings: `ignore_stopped: [Windows 11]`.

### `ups`

Reports the UPS state from `upsc` of Network UPS Tools, for example through the NUT plugin (opt-in, add `ups` to `display`). Shows status, battery charge, runtime and load. The title includes the UPS model when NUT reports it.

- Online is OK.
- On battery, replace battery, overload, bypass, off or an unknown status is a warning.
- Low battery and forced shutdown are critical.

The module is skipped when `upsc` is not installed, and shows "Unavailable" when `upsc` fails (for example a wrong UPS name or an unreachable NUT server) or takes longer than 5 seconds.

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `ups` | string | `ups@localhost` | UPS to query, as `name@host` or `name@host:port`. `upsc -l <host>` lists the UPS names on a host. |
| `battery_warn` | int | `50` | Battery charge in percent at or below which the state is a warning. |
| `battery_crit` | int | `20` | Battery charge in percent at or below which the state is critical. |

### `gpu`

Shows NVIDIA GPUs from `nvidia-smi` (opt-in, add `gpu` to `display`; needs the Nvidia Driver plugin). Each GPU is listed with index, name, temperature, utilization and VRAM used/total, followed by the compute processes using it. Processes in Docker containers are shown by container name, all others by process name. Values a card does not report are shown as "n/a".

The title is OK, Warning or Critical based on GPU temperature. With `warnings_only`, only GPUs at or above `warn` are listed. The module is skipped when `nvidia-smi` is not installed and shows "Unavailable" when it fails or takes longer than 5 seconds.

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `warn` | int | `80` | GPU temperature in °C at or above which the state is a warning. |
| `crit` | int | `90` | GPU temperature in °C at or above which the state is critical. |
| `show_processes` | bool | `true` | List the processes using each GPU. |

### `services`

Checks system services by running `/etc/rc.d/rc.<name> status`. A service that is not running is shown in red and makes the module Critical. With `warnings_only`, running services are hidden.

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `monitor` | list of strings | `[nginx, samba, tailscale, nfsd, sshd, docker]` | Services to check. Any name with a matching `/etc/rc.d/rc.<name>` script works. |

Details:

- A service whose `rc.<name>` script does not exist is skipped, so listing a service that is not installed (for example `tailscale`) is harmless.
- `sshd`, `samba`, `nfsd` and `docker` are skipped when they are disabled in the Unraid settings (SSH in Management Access, SMB/NFS in Network Services, Docker in Docker settings), so a disabled service is not reported as down.
- Characters other than letters, digits, `-` and `_` are removed from the names.

```yaml
services:
  monitor: [nginx, sshd, docker, libvirt]
```

### `network`

Lists network interfaces with their IP addresses. Loopback, Docker bridges (`br-`, `docker0`), `veth` and `vnet0` interfaces are hidden, as are interfaces without an address of the selected type. This module has no status, so `warnings_only` has no effect. Add it to `display` as `networks`.

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `show_ipv4` | bool | `true` | Show IPv4 addresses. |
| `show_ipv6` | bool | `false` | Show IPv6 addresses. |

### `user-drives` and `system-drives`

Show disk usage per mount point, with the used percentage and used/total size.

- `user-drives` covers the array disks and pools (file systems `vfat`, `xfs`, `btrfs` and `zfs`), without the system mounts below.
- `system-drives` covers `/boot`, `/var/log` and `/var/lib/docker`.

Mounts of the same device (for example bind mounts) are shown once.

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `warn` | int | `70` | Used space in percent at or above which a drive is a warning. |
| `crit` | int | `90` | Used space in percent at or above which a drive is critical. |
| `show_zfs_datasets` | bool | `false` | Also list ZFS child datasets (for example `cache/appdata`), not only the pool root. |
| `warnings_only` | bool | `false` | When `true`, only drives at or above `warn` are listed. Unlike other modules, the default here is `false`. |

Both sections are configured separately:

```yaml
user-drives:
  warn: 80
  crit: 95
  show_zfs_datasets: true
system-drives:
  warnings_only: true
```

## Command-line options

| Option | Environment variable | Description |
| --- | --- | --- |
| `-c`, `--config <file>` | `CONFIG_FILE` | Config file to read. Default `/boot/config/plugins/motd/config.yaml`. |
| `--dump-config` | | Print the applied configuration (file plus defaults) as YAML and exit. |
| `--check-config` | | Validate the config file and exit. Prints `OK`, or the parser error with exit code 1. |
| `--preview` | | Render the output even when not running in a terminal (used by the settings page). |
| `--width <n>` | | Width used for `--preview`, and for `--no-color` when not attached to a terminal. Default 80. In all other cases the terminal width is used. |
| `--no-color` | `NO_COLOR` | Print the output without color codes, including the header banner. Status words and table borders are still printed. The flag also renders when `motd` is not attached to a terminal, for example from cron, a script or `ssh tower motd --no-color`. `NO_COLOR` (see [no-color.org](https://no-color.org)) turns color off with any non-empty value, but it does not make `motd` print without a terminal. |
| `--hide-unavailable` | `HIDE_UNAVAILABLE` | Hide modules that cannot read their data instead of showing "Unavailable". |
| `--debug` | `DEBUG` | Print debug log and timing of each module to stderr. |
| `--log-level <level>` | `LOG_LEVEL` | Log level (for example `info`, `warn`, `error`). Logs are only printed together with `--debug`. |

Without `--preview`, `--no-color` or `--dump-config`, `motd` prints nothing when it is not attached to a terminal, so it does not disturb scripts or `scp`.
