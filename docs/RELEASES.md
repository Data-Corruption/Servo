# Publish releases

Servo releases use `https://releases.sproutcli.dev/servo/`. The URL path is also the publication prefix in the configured release bucket. The editable project block is at the top of `scripts/build.sh`.

The release workflow path `.github/workflows/release.yml` defines the Cosign signing identity. Do not move or rename it. Keep the installer artifact layout stable after the first release.

## Prepare

1. Add a real version/date heading and changes to `CHANGELOG.md`; the first `## [vX.Y.Z]` heading selects the release.
2. Configure the workflow's release-storage secrets and enable its `CI_ENABLED` repository variable. Inspect the workflow and build script for the current required inputs.
3. Pass Go race tests, platform checks, driver lint, production frontend builds, lifecycle and release-protocol tests.
4. Validate reference drivers on disposable game hosts before claiming compatibility with a game/image version.

## Publication protocol

CI builds Linux/Windows amd64/arm64 binaries and verifies/signs artifacts. It stages an immutable `releases/<version>/` prefix and remotely verifies it before promoting the root `version` pointer. Installers read and pin that pointer once. The Git tag is pushed last.

Root installers are re-signed only when their deterministically rendered bytes change. Resume publication through the existing build workflow after interruption; do not manually overwrite immutable releases or invent rollback after database migration. Promotion markers and the publication state machine handle retry and retention. Exercise them through `./scripts/test.sh -release`.

The first rebuilt release is a fresh-install stream; there is no migration from old LMDB Servo. Communicate that clearly before publication. Preparing a local build does not deploy artifacts or the docs site.
