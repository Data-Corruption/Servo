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
   unchanged, the game remains stopped, and saves survive container recreation.
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
