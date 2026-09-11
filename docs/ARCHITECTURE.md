# Architecture

Servo is an ordinary Go application built from Sprout. Each CLI invocation is its own process. The service singleton owns the game runtime and dashboard; ordinary CLI processes never run the scheduler or driver operations.

## Persistence and lifecycle

SQLite runs in WAL mode with a fixed four-connection pool. Configuration updates validate and commit in immediate write transactions. Sessions have separate database rows and fixed expiry. A single `game_operation` row stores the admitted/latest operation, current step and bounded final diagnostics. Startup changes unfinished work to interrupted without replaying it.

Installer-controlled migrations are the only production schema upgrade path. Lifecycle leases, state watchers, detached maintenance, update verification and service readiness remain Sprout's protocol. Runtime cancellation drains probes, operations, scheduler and HTTP before the database closes. See the repository's [MAINTENANCE.md](MAINTENANCE.md) for the exact protocol.

## Game runtime

The runner reserves interactive intent before cancelling a background probe. Its execution gate serializes driver verbs, activation validation and operation sequences. HTTP handlers read snapshots; the worker performs bounded status probes. Activation commits only after candidate validation and a confirmed stopped outgoing game.

There is no job queue or recovery state machine. Operation admission is durable before driver execution, failure stops further steps, and an interrupted operation is never automatically retried. Scheduler windows use the same admission path, preserving deliberately stopped state.

## Files and boundaries

The layout package owns paths and permissions. `drivers/`, `driver-data/` and `backups/` survive Servo uninstall; backgrounds, SQLite and TLS material live under removable `data/`. Explicit game uninstall owns game teardown and successful data removal.

The router preserves same-origin CSRF checks, allows multipart only for the administrator image endpoint, and retains the restrictive CSP. Pages use external assets and text-only dialogs. API session expiry returns 401; page navigation redirects to login. Raw operation logs are administrator-only.

## Platform support

Linux game hosting is supported; amd64 and arm64 artifacts are built. Windows retains the CLI, installer, Scheduled Task and dashboard with experimental native-executable driver scaffolding. Do not interpret a Windows binary as a validated Windows game integration. Future work is tracked in the [icebox](ICEBOX.md).

## Code map

`internal/app` composes process resources; `internal/app/commands` runs the service worker. `internal/driver` owns the executable contract; `internal/ops` owns operation admission, polling, scheduling and archives. `internal/platform/http/router` owns HTTP permissions and handlers. `internal/ui` contains embedded templates, vanilla JavaScript and Tailwind/DaisyUI sources.

## Permission details

Permissions are a bitmask. The CLI accepts a leading `!` to remove bits, such as `admin !game.restore`. Admin-only checks require the complete admin mask, so subtracting a bit also removes access to those actions. Named user removal invalidates their sessions across processes. Authentication details belong here; the public guide explains the individual permissions and fixed session lifetime.

## Listener defaults

New production configurations listen on `:8829` for HTTPS and `127.0.0.1:8830` for the local HTTP proxy. The proxy port follows the build-configured dashboard port by one. Development builds bypass authentication, so their defaults remain loopback HTTPS with no proxy listener. Existing saved bind settings are preserved.
