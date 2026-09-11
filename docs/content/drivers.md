---
title: Teach it your game
linkTitle: Drivers
number: "03"
description: Drivers are executable scripts that implement a small verb contract. Copy the template, fill in the verbs, and test it from your shell.
---

A driver is a single executable that teaches Servo how to manage one game server.
POSIX sh is the usual choice, but anything executable works on Linux. Servo has no
game-specific knowledge compiled in. The driver is the whole integration.

```text
<driver> <verb> [args...]
```

That's the interface: argv, environment variables, exit codes and stdout. No SDK.
Start with the [driver template](https://github.com/Data-Corruption/Servo/blob/main/drivers/driver.template.sh),
fill in the verbs, then copy it into `~/.servo/drivers/` and make it executable.
Activate it from Settings as an admin. The
[Palworld driver](https://github.com/Data-Corruption/Servo/blob/main/drivers/fedora-palworld.sh)
is a fuller example using Fedora, rootless Podman and user systemd.

Drivers are arbitrary code running as the Servo user. Install them through shell
access, and only install ones you've read. Servo doesn't accept driver uploads
through the UI.

## Environment

Every invocation receives:

| Variable | Meaning |
| --- | --- |
| `SERVO_DATA_DIR` | This driver's game data and scratch directory |
| `SERVO_BACKUP_DIR` | Where this driver writes its backup archives |
| `SERVO_VERSION` | Servo's version string |

The data and backup paths live under `~/.servo/driver-data/` and `~/.servo/backups/`,
keyed by the driver filename. Use them directly; there's no need for another game-name
subdirectory. Renaming the driver doesn't move its state. If you rename, move stuff over manually.

Servo prepares private directories for activation and operations. Read-only probes
must tolerate missing game data, including after game uninstall. Don't change directory
ownership or permissions, or replace them with symlinks. A successful game uninstall
deletes `SERVO_DATA_DIR`; it keeps the script and archives.

## Describe your driver

`describe` prints metadata, quickly and without side effects:

```text
DRIVER_API=1
NAME=My game
GAME=my-game
CONTAINERIZED=false
CAPABILITIES=restore,uninstall,notify,players,metrics,version
```

`DRIVER_API` and `NAME` are required. `GAME` and `CONTAINERIZED` are informational.
Unknown keys are ignored. Unsupported API versions are rejected.

**Declare the optional verbs you implement in `CAPABILITIES`.** Missing or empty
means none. Unknown capabilities are rejected. Servo never calls restore or uninstall
just to find out whether they work.

Optional `TARGET_SERVER_VERSION` and `TARGET_CONTAINER_VERSION` fields let the dashboard
show a mismatch warning against the live versions. These are plain string comparisons,
not compatibility checks. Only set target versions you've actually tested.

## Verb contract

Driver API v1 has eight required verbs:

| Verb | What it should do | Stdout |
| --- | --- | --- |
| `describe` | Print metadata; fast, no side effects | `KEY=VALUE` lines |
| `deps` | Check every external tool you need; failure blocks activation | Missing tools, one per line |
| `status` | Report online or stopped through the exit code | Optional detail |
| `start` | Start the game; already running is success | Log output |
| `stop` | Stop gracefully; already stopped is success | Log output |
| `install` | First-time setup or recreate the stopped runtime | Log output |
| `update` | Update the stopped game; succeed as a no-op if already current | Log output |
| `backup` | Write one completed compressed archive | Absolute archive path as the **last line** |

And these optional verbs:

| Verb | What it should do | Stdout |
| --- | --- | --- |
| `restore <archive>` | Replace game data from the supplied archive | Log output |
| `uninstall` | Remove resources created outside `SERVO_DATA_DIR` | Log output |
| `notify <message>` | Warn players before scheduled maintenance | Log output |
| `players` | List connected players | One name per line; empty means zero |
| `metrics` | Summarize live metrics | One short display line |
| `version` | Report the live game version | Version string |
| `container-version` | Report the live container version | Version string |

Exit codes:

- **0**: success. For `status`, the game is online; for `deps`, everything is present.
- **3**: `status` only, the game is stopped. This isn't an error.
- **4**: an optional verb is unsupported.
- **Anything else**: failure. Detailed output is available to administrators.

Don't turn an inspection error into “stopped.” A failed container query is a failure,
not proof that the game is offline. Likewise, a successful `stop` must mean the game
actually stopped. Make both `start` and `stop` idempotent.

## Let Servo do the sequencing

There is no `restart` verb. Servo calls `stop`, then `start`. Update, backup and restore
follow the same idea: check status, stop if online, do the work, then start again only
if the game was running beforehand. Install also requires confirmed stopped state.

**Any failed step ends the sequence.** A failed backup doesn't lead to a recovery start.
An interrupted operation isn't resumed. Keep that in mind when deciding what success
means for your verb.

Only one driver invocation runs at a time. Read probes run in the background and may
be cancelled to let an interactive operation begin. Keep them fast and side-effect free.
Timeouts are 30 seconds for probes/notifications, 10 minutes for start/stop, and
60 minutes for install/update/backup/restore/uninstall.

Keep machine-readable data on stdout and diagnostics on stderr. Captured probe output
is capped at 64 KiB per stream; overflowing it fails the probe. Live operation logs
keep a bounded tail. Don't put secrets in output.

## Keep the game alive independently

Servo normally runs as a systemd user service. A child left in that service's cgroup
can be killed when Servo stops, restarts or updates. The game needs to outlive the
short-lived driver invocation, in its own service or scope.

The Palworld driver starts its container this way:

```sh
systemd-run --user --collect --scope -- podman start my-container
```

**Fail start if the scope can't be created.** Don't fall back to launching the game
inside Servo's lifetime. Short-lived work like image pulls, tar and RCON calls can
stay in the invocation group. On Linux, Servo kills that process group on cancellation
or timeout.

Windows currently has native `.exe` invocation, immediate-process cancellation and
bounded pipe waiting. There is no shared game shutdown protocol or script-host support.
Treat it as scaffolding, I need to think more about how to properly handel it.

## Backups / restore

`backup` runs while the game is stopped. Write one compressed regular file directly
in `SERVO_BACKUP_DIR`. Pick the format yourself, and use an extension that says what
it is. Print the absolute path as the last stdout line.

Create the archive in a staging subdirectory, then rename it into place **after**
compression succeeds. Don't leave a partial archive looking like a finished backup.
Servo checks the reported file before pruning older backups.

`restore <archive>` receives an existing archive from this driver's backup directory.
Validate its paths and entry types, and extract to staging before removing current
saves. Reject traversal paths and links that could escape the intended data tree.
The Palworld example does this for `Pal/Saved`; it does not provide automatic rollback.

Test restoration, not just archive creation. A backup you've never restored is a hope,
not a backup.

## Test from the shell

Use disposable directories and game resources. Even a shell test runs real driver code:

```sh
scratch=$(mktemp -d)
mkdir "$scratch/data" "$scratch/backups"
export SERVO_DATA_DIR="$scratch/data"
export SERVO_BACKUP_DIR="$scratch/backups"
export SERVO_VERSION=dev
./my-game.sh describe
./my-game.sh deps
./my-game.sh status
# inspect the exit code from status
printf '%s\n' "$?"
```

Then exercise install, start, stop, backup and restore against a disposable game.
Check offline behavior, missing dependencies, failed probes, cancellation and failed
archive creation. Restart Servo while an otherwise idle game is running to verify
that you've separated their lifetimes properly.
