# Icebox

These are loose ideas for the future, not promises of anything.

## Windows game integration

Current scope is native `.exe` discovery/invocation, basic immediate-process cancellation, and bounded output waiting. Sprout's own Scheduled Task stop leases already work; they are separate from stopping a game.

Deferred implementation:

- A file-based game shutdown signal/acknowledgement contract, with stale-request handling and bounded waits.
- A demonstration Windows driver showing that graceful shutdown protocol.
- Process-tree cleanup and an explicit strategy for keeping the lasting game independent of the short-lived driver.
- Optional PowerShell or other script hosts, without implicit command-string execution.
- Disposable Windows game-host integration tests and a documented support matrix.

Keep these choices driver-owned and unopinionated until a real game integration justifies shared policy.

## Other ideas

An optional read-only game update-check verb could show an update-available badge. It must not automatically apply game updates.

## Not doing

Multi-server management, driver marketplaces/uploads, mod management and integrated off-host backup synchronization.
