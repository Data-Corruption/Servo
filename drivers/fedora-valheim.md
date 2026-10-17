# Valheim on Fedora

Setup and validation notes for the [Valheim driver](fedora-valheim.sh).

## Setup

The [Valheim driver](fedora-valheim.sh)
runs a vanilla, password-protected Steam server on Fedora x86-64 using rootless
Podman (4.3 or newer) and your systemd user manager. Crossplay and mods are not
included. It supports install, update, start, stop, backup, restore and uninstall.

Set `SERVER_PASSWORD` in the script to at least five characters, and choose
`SERVER_NAME` and `WORLD_NAME`. Copy it into `~/.servo/drivers/`, run
`chmod 700 ~/.servo/drivers/fedora-valheim.sh`,
activate it in Settings, then click Install and Start. Install downloads the game;
Start launches the installed version. Changes to the configuration block require
Install again, which preserves your saves. Uninstall before changing the container
name so the old container isn't left behind.

Friends connect to `your-hostname:2456`. Open UDP ports **2456–2457** in the host
firewall and forward both ports on your router. Both are needed even with the
default private listing (`PUBLIC=0`). With firewalld:

```sh
sudo firewall-cmd --permanent --add-port=2456-2457/udp
sudo firewall-cmd --reload
```

If you change `GAME_PORT`, open that port and the next one instead. Set `PUBLIC=1`
and Install again to appear in the public server list. The driver does not change
your firewall or router.

Game files live under `~/.servo/driver-data/fedora-valheim.sh/`. `server/` contains
downloaded binaries; `saves/` contains worlds and permission lists. To import a
world, stop the server and copy its complete files or directory into
`saves/worlds_local/`, preserving private permissions and ownership by the Servo
user. Set `WORLD_NAME` to the imported world's name, then Install and Start.
Keep an untouched copy of the original world. Servo backups contain all of
`saves/`, including admin, ban and permitted-player lists.

Stopping Servo leaves the game running. The game does not start automatically
after a host reboot or crash. If an update fails or is interrupted, Start remains
blocked until you successfully rerun Install or Update. A surviving updater must
finish first; it has a 55-minute maximum runtime. A stop timeout reports failure
and leaves the game running rather than force-killing it during a save.

## World settings

Edit these values in the driver's configuration block, then **Stop → Install →
Start** to apply them. They are vanilla Valheim launch options and work with an
existing world. Player-based raids are enabled by default in this driver; other
gameplay overrides are unset. The [official server guide](https://www.valheimgame.com/support/a-guide-to-dedicated-servers/)
documents the launch flags.

| Setting | Default | Choices / effect |
| --- | --- | --- |
| `PLAYER_BASED_RAIDS` | `true` | Raids follow individual player progression. `true` or `false`. |
| `WORLD_PRESET` | `""` | `normal`, `casual`, `easy`, `hard`, `hardcore`, `immersive`, `hammer`. Resets previous world modifiers. |
| `COMBAT` | `""` | `veryeasy`, `easy`, `hard`, `veryhard`. |
| `DEATH_PENALTY` | `""` | `casual`, `veryeasy`, `easy`, `hard`, `hardcore`. |
| `RESOURCE_RATE` | `""` | `muchless` (0.5×), `less` (0.75×), `more` (1.5×), `muchmore` (2×), `most` (3×). |
| `RAID_FREQUENCY` | `""` | `none` (disable raids), `muchless`, `less`, `more`, `muchmore`. Separate from player-based raid progression. |
| `PORTALS` | `""` | `casual` (all items), `hard` (no boss portals), `veryhard` (no portals). |
| `NO_BUILD_COST` | `false` | Build without material costs; recipes still need discovering. |
| `PASSIVE_ENEMIES` | `false` | Enemies wait for provocation. |
| `NO_MAP` | `false` | Disable the map and minimap. |

Use lowercase values exactly as listed. Empty strings omit the modifier; they
do not mean "reset to normal." Likewise, `false` omits a `-setkey` request rather
than explicitly removing a saved world key. To reset world modifiers, choose
`WORLD_PRESET="normal"` and reapply the individual choices you want. A preset is
applied at every launch and resets other world modifiers too; leave it empty
when you want to preserve existing world choices. The driver appends individual
modifiers after the preset, then enabled checkbox keys.

For example, these give more resources and allow metal through portals:

```sh
PLAYER_BASED_RAIDS=true
RESOURCE_RATE="more"
PORTALS="casual"
```

The [AMP Valheim settings](https://github.com/CubeCoders/AMPTemplates/blob/main/valheimconfig.json)
also describe these choices and their resource multipliers.

To disable raids, set this in the configuration block, then **Stop → Install →
Start**:

```sh
RAID_FREQUENCY="none"
```

This passes `-modifier raids none`. `PLAYER_BASED_RAIDS` controls whose progression
determines raids; setting it to `false` does not disable raids.

Disabling raids does not make buildings invulnerable. The official server guide
does not document a launch option to disable building damage, weather decay or
structural collapse. Those controls require mods, which this vanilla driver does
not install. For example, [Valheim Plus](https://github.com/valheimPlus/ValheimPlus/blob/development/valheim_plus.cfg)
defines separate settings for structure damage and structural integrity.
`NO_BUILD_COST` only removes material costs; it does not protect buildings.

Valheim's own save and backup timing is configurable separately from Servo's
scheduled archive backups:

| Setting | Default | Meaning |
| --- | --- | --- |
| `SAVE_INTERVAL` | `1800` | Seconds between world saves (30 minutes). |
| `WORLD_BACKUPS` | `4` | Built-in backups to retain; `0` disables them. |
| `BACKUP_SHORT_INTERVAL` | `7200` | Short backup interval in seconds (2 hours). |
| `BACKUP_LONG_INTERVAL` | `43200` | Long backup interval in seconds (12 hours). |

Intervals must be positive integers. Invalid settings fail before the driver
pulls an image or changes the installation. The quoted argv construction keeps
server names and passwords with spaces or shell punctuation intact.

## Updating

When a Valheim patch comes out, **Stop → Update → Start**. Install and Update do
the same work: SteamCMD checks the files in `server/` against the current public
build and downloads only what changed. The output names the build, for example
`Valheim build: 25730807 -> 25909431`. The operation fails unless the installed
build matches the public build SteamCMD reported during the same run. To read
the installed build yourself:

```sh
grep '"buildid"' ~/.servo/driver-data/fedora-valheim.sh/server/steamapps/appmanifest_896660.acf
```

Each updater is a new container with an empty Steam cache. SteamCMD can't update
an install in place without the previous build's depot manifest from that cache,
and Steam refuses to send it again: Steam's content log shows `Access Denied`, and
SteamCMD then reports `state is 0x6 after update job`. The driver therefore removes
Steam's app manifest before each run, so SteamCMD installs over the existing files.
Files that a newer build no longer ships stay in `server/`.

### Clients see “Incompatible version” after Update

Older copies of this driver could report success without updating. SteamCMD
printed `Success! App '896660' already up to date.` for an outdated install, and
the old build kept running. Check the installed build as above. If it is behind,
copy the updated script over, preserving your configuration block, then
**Stop → Update → Start**.

### Install fails with “Missing configuration”

SteamCMD can report `Failed to install app '896660' (Missing configuration)`
after updating itself and logging in successfully on a fresh install. This has
also been [reported by another Valheim integration](https://github.com/kriegalex/k8s-charts/blob/main/charts/valheim-server/README.md#troubleshooting).
Without the app's metadata, `app_update` can also stop with `Timed out waiting
for update to start`.

Install and Update refresh that metadata with `+app_info_update 1`, then print it
with `+app_info_print 896660` before `+app_update 896660`. Printing makes SteamCMD
wait until the metadata has arrived. The operation output leaves out that long
listing; `runtime/steamcmd.log` keeps it. If your installed driver lacks these
commands, copy the updated script over, preserving your configuration block, then
click Install again. The retry replaces the stopped updater container and
preserves your saves. Leave `runtime/update-pending` in place; a successful
install clears it. If it still fails, keep the full output in `runtime/steamcmd.log`
for diagnosis.

## Startup logs

Unity writes the game log to `runtime/valheim.log` in this driver's data directory.
Start prints the full path, and startup failures also point to the container
output. With the default driver filename, read both as the Servo user:

```sh
tail -n 200 ~/.servo/driver-data/fedora-valheim.sh/runtime/valheim.log
podman logs --tail 200 servo-valheim
podman inspect --format 'Exit={{.State.ExitCode}} OOM={{.State.OOMKilled}} Error={{.State.Error}}' servo-valheim
```

Use your configured container name if it differs. `runtime/steamcmd.log` holds
Install/Update output; the game log holds Unity output. The game log is reset
before each stopped-to-running Start so readiness cannot come from an old launch.
Starting an already-running game leaves its log intact. Game logs are excluded
from save archives.

If container output ends with `Unable to open log file, exiting.`, check the
driver's launch command. Older copies used `-logFile /dev/stdout`, which Unity
could not open under rootless Podman. The driver now uses the regular file
`/data/runtime/valheim.log`. Copy the updated driver over, preserving your
configuration, then click **Install** to recreate the stopped container with the
new command, followed by **Start**. Editing the script alone does not change an
existing container's launch command. Install preserves your saves.

## Runtime

[`fedora-valheim.sh`](fedora-valheim.sh) uses the vanilla `docker.io/cm2network/valheim:base`
runtime, bypassing its startup wrapper. SteamCMD app 896660 is installed/updated
explicitly; the running game uses SteamAppId 892970. The driver owns the game and
updater containers through data-path and role labels. No Servo API changes are
required.

The image supplies SteamCMD, the game libraries and Tini for signal forwarding.
The game runs in a separate systemd scope outside Servo's lifetime. Servo controls
game updates and archive backups; Valheim's own periodic saves and backups
remain enabled.

Sources inspected for the integration:

- [Image definition](https://github.com/CM2Walki/Valheim/blob/master/bookworm/Dockerfile)
- [Upstream startup wrapper](https://github.com/CM2Walki/Valheim/blob/master/etc/entry.sh)
- [Official dedicated server guide](https://www.valheimgame.com/support/a-guide-to-dedicated-servers/)
- [Podman user mapping, SELinux mounts and container timeout](https://docs.podman.io/en/latest/markdown/podman-run.1.html)

## Automated coverage

The Go shell-driver tests use fake Podman/systemd commands and real tar/gzip
archives. They cover command sequencing, ownership and inspection failures,
startup readiness, stop timeouts, interrupted updates and archive validation.
They do not establish compatibility with a real image or game release.

From the repository root, run `./scripts/test.sh` and `./scripts/test.sh -lint`.

## Live acceptance check

Status: **not yet validated on Fedora**. The implementation environment was
Ubuntu 24.04 with no usable Podman runtime. No image digest has been live-tested.

Use disposable Servo data and a Fedora x86-64 host with SELinux enforcing,
rootless Podman >= 4.3, a systemd user manager and a Steam client. Record Fedora,
Podman and game versions, plus `runtime/image-digest` from the driver's data
directory. That file records the pulled image's registry digests after a
successful Install/Update; it is installation provenance, not a test claim.

1. Configure a password, install and start. Confirm private direct-IP joining
   over UDP 2456–2457 from a Steam client and readiness in the operation result.
2. Change the world in-game. Stop, start and rejoin; verify the change was saved.
3. Restart the Servo service with the game running. Verify the client remains
   connected and the container lives outside Servo's service cgroup.
4. Stop and update. Confirm the Steam check runs even when the image digest is
   unchanged, the reported build matches the public build of app 896660, the
   game remains stopped, and saves survive container recreation.
5. Back up, change the world again, then restore and rejoin. Confirm both world
   data and permission lists are restored. Repeat using an imported world.
6. Confirm all mounted data is owned by the host Servo user, the data root
   remains mode 0700, and SELinux stays enforcing without access denials.
7. Interrupt an update. Confirm Start is blocked, another operation cannot
   overlap a surviving updater, and an explicit retry succeeds after it exits.
8. Exercise a shutdown timeout with a disposable test runtime. Confirm failure
   does not force-kill the game or allow the operation's next step.
9. Uninstall the game through Servo. Confirm both owned containers and game data
   are removed, while backups, the driver and shared cached images remain.

Append actual versions, image digest and results here after live validation.

Update path, 2026-10-17, on Ubuntu 24.04 (WSL2) with rootless Podman 4.9.3, not
Fedora or SELinux. Image `cm2network/valheim@sha256:c3700ab02bbd3b27a2ef6d6a340dfc48f5e598564601743d4a44512e322a7c1a`.
The driver updated a real 1.0.17 install (build 25730807) to 1.0.20 (build
25909431) in about a minute. A second Update reported the same build. With the
previous driver's SteamCMD commands, the same install failed with `state is 0x6
after update job`. On a live server, Update had reported `already up to date`
while staying on an older build.
