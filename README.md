# Servo

Dedicated game server manager, friend group sized.

Start, stop, update, schedule restarts / back ups and restore your game, all
through an HTTPS dashboard. Simply implement a verb based driver for your game,
plop it in `~/.servo/drivers/`, and activate it from the dashboard.

Make a login for your friends via CLI, give them perms based on how much you
trust them. E.g. ability to start, stop, update, etc the server. Point a
domain at it, give your friends the URL, have fun :p

[Usage](https://servo.sproutcli.dev/) ·
[Driver authoring](docs/content/drivers.md) ·
[Development](docs/DEVELOPMENT.md)

## Quick Start

```sh
# install servo
curl -fsSL https://releases.sproutcli.dev/servo/install.sh | sh
# create admin login
servo users add --username admin --perms "admin"
```

Then open https://localhost:8829 or setup a [reverse proxy](https://servo.sproutcli.dev/caddy/).

Place a driver in `~/.servo/drivers/`, make it executable, activate it in
Settings, then click Install and Start. For reference I've used
`drivers/fedora-palworld.sh` a fair bit.

## Who is this for?

This is for semi-technical people, the kind who can run a containerized game
server and would like to wrap that with automated backups, restarts, and a
simple secure UI that allows non-technical friends to do server related actions
without giving them full access to your machine.

> Currently targets Linux amd64/arm64. Windows is still just experimental stubs
