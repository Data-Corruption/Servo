---
title: Use Servo
linkTitle: Use Servo
description: Dedicated game server manager, friend group sized. Install it, add a driver, and let your friends handle the restart button.
number: "01"
---

## Install Servo

```sh
# Install servo where your game server will run
curl -fsSL https://releases.sproutcli.dev/servo/install.sh | sh

# Create your admin login, enter a strong password
servo users add --username admin --perms "admin"
```

Then open **https://localhost:8829** (or **https://\<LAN IP\>:8829**) and sign
in. You'll be greeted with a scary warning, this is cause servo self-signs it's
certificate. Click trust / proceed anyway.

If you plan on inviting friends or connecting from outside your local network
[put Caddy in front of it]({{< relref "caddy" >}}). Then you'll have a domain
and a trusted HTTPS certificate.

## Add your game

A driver is a small script that knows how to start, stop, update and backup
your game. Servo does the dashboard / calling it part. To keep things simple
you can only have one "active" driver / game at a time.

You can start with the
[Fedora Palworld driver](https://github.com/Data-Corruption/Servo/blob/main/drivers/fedora-palworld.sh)
or [write your own]({{< relref "drivers" >}}). Read the script and
edit its settings first: passwords, ports, container name, the usual stuff.

```sh
# from the directory containing your driver
cp my-game-driver.sh ~/.servo/drivers/
chmod +x ~/.servo/drivers/my-game-driver.sh
```

In **Settings**, select and activate the driver. Then head back to the
dashboard, click **Install**, wait for it to finish, then **Start**.

To install a driver you need shell/SSH access. There is no driver-upload in the
UI. That would be begging to be a remote code execution attack vector, no
thanks.. On that note, read drivers before use.

To switch drivers later, stop the current game first. Switching doesn't
delete its data or backups, but an unknown game status blocks the switch.

## Invite your friends

Give players the controls they need, if any:

```sh
servo users add --username alice --perms "game.control"
servo users add --username bob --perms "game.control game.backup"
servo users list
```

Send them your dashboard URL and their login. Everyone who signs in can see
status, players, connection details and activity. Extra permissions unlock
actions:

| Permission | What they can do |
| --- | --- |
| `game.control` | Start, stop, restart and update the game |
| `game.backup` | Make and download backups |
| `game.restore` | See backup names/dates and restore them |
| `servo.settings` | Change/enable the restart/backup, connection details, themes and service settings |
| `servo.control` | Stop, restart and update Servo itself |
| `admin` | Everything, including driver activation, game install/uninstall, site styling, and detailed logs |

To remove someone, run `servo users remove --username alice`. Their existing
logins are revoked too. For a new password or different permissions, just
remove the account and add it again.

**Logins last 30 minutes.** Clicking around doesn't extend that. If you're
signed out while a backup or update is running, it carries on. Just log back in
to see how it went.

## Day to day

The dashboard is the place for game controls, connected players, metrics and
activity. Some extras depend on what your driver supports.

A version warning means the reported game/container version differs from the
driver's. It doesn't do anything, just a warning "hey this driver was
written when game/container was at version x".

**Game server** controls affect the game. **Servo** controls in Settings affect
the dashboard service. A game managed independently by its driver can keep
running while an idle Servo restarts or updates. Though restarting Servo during
an operation interrupts it.

One operation runs at a time. Update, backup and restore stop a running game
first, then start it again only if every step succeeds. A game that was already
stopped stays stopped. **If any step fails, the sequence ends.** There's no
auto retry / recovery. Check Activity and the game before trying again; admins
get the full log.

In Settings, fill in the game's address and password so friends can copy them
from the dashboard. These are just the join details: editing them doesn't
reconfigure the game. Keep them in sync with your driver.

You can also pick a theme, choose separate login/dashboard backgrounds, and
adjust blur and alignment. Background uploads need admin access and are capped
at 8 MiB (PNG, JPEG, WebP, GIF or AVIF). A login background is visible before
signing in. A forced theme applies to everyone; otherwise each browser can
choose light or dark.

## Backups & restarts

**Backup now** makes an archive you can download. By default Servo keeps the
newest **five** archives; set retention to **zero** for unlimited. Older
archives are pruned when new ones that push the total count over the limit are
created successfully.

To roll back, choose **Restore** on an archive and confirm. It replaces current
game data. Restore has its own permission and needs driver support. Test this
full flow on a dummy game before using it for real.

For daily maintenance, choose a time in Settings. **Daily Restart** and
**Scheduled Backups** share that window, in the **host's local timezone**.

- A running game stops, optionally backs up, then starts again on success.
- A stopped game stays stopped. A scheduled backup can still run.
- Backups work with daily restart disabled, but a running game still needs downtime.
- Busy or missed windows are skipped. Game updates are always manual.

In-game player warnings default to ten minutes ahead, if the driver supports
them; zero turns them off. On a daylight-saving change, a missing clock time is
skipped and a repeated time runs only on its first occurrence.

## Troubleshooting

**Can't reach the dashboard?** Check `servo service status` and `servo config show`.
The dashboard listens on port 8829 on your LAN; check the host address and firewall. For a domain,
check the [Caddy guide]({{< relref "caddy" >}}).

**Driver missing or activation refused?** It must be a regular executable directly
under `~/.servo/drivers/`, not a symlink. Check missing dependencies, stop the outgoing
game, and resolve any status error before switching. Servo expects its directories
to be owned by your user with mode `0700`; it won't quietly fix unexpected permissions.

**Failed or interrupted operation?** Check Activity as an admin, then check the game
itself. Servo doesn't resume interrupted work. A failed stop or status check means
the game might still be running.

**Servo update failed?** Look at `~/.servo/logs/maintenance.log`. Rerun the installer
if the installation needs recovery. Don't manually replace files with an older version.
For a reproducible bug, [open an issue](https://github.com/Data-Corruption/Servo/issues),
with passwords and private log details removed.

## Uninstalling

**Uninstall game** stops and removes the game through its driver, then deletes that
driver's data after successful teardown. It keeps the script and backups.

To uninstall Servo itself run:

```sh
servo uninstall
```

That removes Servo, its settings, logins, etc.
It **keeps** `~/.servo/drivers/`, `~/.servo/driver-data/` and `~/.servo/backups/`.
If you want the game gone too, uninstall it from the dashboard first.
