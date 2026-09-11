---
title: Give it a domain
linkTitle: Caddy
number: "02"
description: Put Caddy in front of Servo so your friends get a normal HTTPS URL.
---

## Before you start

You'll need a domain you control and [Caddy installed](https://caddyserver.com/docs/install)
on the host. The commands below assume Caddy's Linux systemd service and its usual
`/etc/caddy/Caddyfile`.

Point an **A record** for `your-domain.com` or `subdomain.your-domain.com` at the
host's public IPv4 address. Only add an **AAAA record** if IPv6 actually reaches
that host. Allow inbound TCP **80** and **443** through your firewall, and forward
those ports to Caddy if you're behind a router (same for the ports your game uses).
This standard setup needs a publicly reachable host; a private address or
carrier-grade NAT won't do.

Additionally if this is a local machine, I
recommend setting up DDNS for that domain. That way if your IP changes you
won't have to update the domain record manually. Optional but nice to have.

## Servo's listeners

A fresh Servo installation already listens on **`:8829` for LAN HTTPS** and
**`127.0.0.1:8830` for a local HTTP reverse proxy**. You can configure Caddy straight
away; there is nothing to enable in the dashboard.

Only Caddy's ports 80 and 443 need to be reachable from outside your network.
The proxy listener on 8830 stays loopback-only.

If you need to use a different port for the reverse proxy, set it from the CLI:

```sh
servo config set --proxy-bind "127.0.0.1:8830"
servo service restart
```

You can restart while the game server is running. Just avoid restarting during operations.
E.g. while the start, stop, backup, etc operations are running.

## Configure Caddy

Add this anywhere in  `/etc/caddy/Caddyfile`, replacing the domain with yours. Keep any other
sites you already have in that file.

```caddyfile
your-domain.com {
    reverse_proxy 127.0.0.1:8830
}
```

Validate the file then reload:

```sh
sudo caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
sudo systemctl reload caddy
```

Now visit **https://your-domain.com**, sign in, and open Settings. You should get
a trusted certificate with no browser warning. Give your friends this URL and their
[Servo accounts]({{< relref "/" >}}#invite-your-friends).

## If it doesn't connect

| Symptom | Check |
| --- | --- |
| No connection | DNS points at this host; firewall/router lets TCP 80 and 443 reach Caddy |
| Certificate error | Check DNS, including stale AAAA records, and Caddy's certificate logs |
| `502 Bad Gateway` | Servo is running, its proxy listener is enabled, and Caddy can reach `127.0.0.1:8830` |
| Login or actions rejected | Use the HTTPS domain; preserve Host and `X-Forwarded-Proto`; avoid adding a path prefix |

```sh
servo service status
servo config show
sudo journalctl -u caddy --since "10 minutes ago"
```

Serve Servo at the root of its own hostname, not a subpath like `/servo/`. If you
later remove Caddy, you can disable the proxy listener with
`servo config set --proxy-bind ""` and restart Servo.
