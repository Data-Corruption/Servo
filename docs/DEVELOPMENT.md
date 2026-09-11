# Build and test

Develop with the Go version in `go.mod`, GCC for race tests, and the tool prerequisites documented by the build script. All downloaded tool versions and checksums belong in `scripts/vendor.sh`.

```sh
./scripts/test.sh
./scripts/test.sh -lint
./scripts/build.sh
```

The default build is development-only: isolated `~/.servo-dev` storage, debug logs, auth bypass, and no updates. Never expose it as a production dashboard. Run the resulting host binary with `service run`.

```sh
./scripts/build.sh --prod
./scripts/build.sh --prod-all
go vet ./...
GOOS=windows go vet ./...
```

Production builds preserve authentication. All-target builds produce Linux and Windows amd64/arm64 artifacts. Shell driver tests run on Linux; native Windows execution tests run on the Windows CI runner.

The embedded frontend uses vanilla JavaScript, Tailwind and DaisyUI. Edit `internal/ui/assets/js/src/` and `internal/ui/assets/css/input.css`, not generated output. The build bundles and fingerprints assets. The docs site uses pinned Hugo Extended and Hextra; see [README.md](README.md) for production site validation.

The Linux lifecycle harness is `./scripts/test.sh -e2e`; publication state-machine checks are `./scripts/test.sh -release`. They use disposable fixtures and must never target a live game installation. `docs/local/` holds historical reference material; it is excluded from the public documentation site.
