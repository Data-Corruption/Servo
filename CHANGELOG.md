# Changelog

## [v0.1.0] - 2026-09-11

### Added

- Game server dashboard with status, players, metrics, join information, backups, and live operation progress.
- Driver API v1, a driver template, and a Fedora/Podman Palworld driver. One active driver and game server per installation.
- Install, start, stop, restart, update, backup, restore, and game uninstall operations, with optional actions declared by each driver.
- Daily host-local restart and backup scheduling, optional advance player notifications, and configurable backup retention.
- Separate permissions for game control, backups, restores, settings, and Servo controls; admin-only driver activation and game installation/removal.
- Dashboard themes, uploaded backgrounds, blur, and alignment settings.
- A three-page end-user documentation site covering everyday use, Caddy remote access, and driver authoring, with a dark pixel-inspired design and interactive logo.

### Changed

- Rebuilt Servo on the new Sprout scaffold, using SQLite, transactional installers, coordinated service lifecycle actions, and signed updates.
- Login uses usernames and passwords managed through `servo users`. Sessions expire 30 minutes after login; polling and other activity do not extend them. Accepted operations continue after logout or expiry.
- Fresh production installations listen for HTTPS on `:8829` and enable the loopback reverse-proxy listener on `127.0.0.1:8830`.
- Game operations run one at a time and retain their latest result across Servo restarts. Interrupted work is reported without automatic retries or recovery starts.
- Install requires a confirmed stopped game server. Operation sequences stop on any failed step; successful updates, backups, and restores preserve the previous running or stopped state.
- Backups are validated before retention pruning. Servo uninstall retains driver scripts, game data, and backups; explicit game uninstall preserves scripts and backups.
- Status polling uses cached, bounded probes with visible stale/error states. Raw driver diagnostics are restricted to admins.

### Compatibility

- This rebuild requires a fresh installation; legacy LMDB data, credentials, and layouts are not migrated.
- Linux is the supported game-hosting platform. Windows release binaries include basic native executable driver support; broader Windows game support remains experimental.
