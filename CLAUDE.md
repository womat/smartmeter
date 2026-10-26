# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

`smartmeter` is a Go daemon that polls an energy meter over Modbus TCP or RTU and serves its values as a **Fronius Smart Meter 63A-3**: the proprietary RS485 map polled by Fronius inverters (unit ID 1) and the SunSpec meter block (model 203) read by wallboxes and evcc over Modbus TCP (unit ID 200). It also serves a TLS REST API. Single binary, cross-compiled for the Pi.

## Build / develop

Nothing here is Linux-only, so `go build ./...`, `go vet ./...` and `go test -race ./...` work on macOS too.

```sh
make build_arm6        # Pi 1 / Zero and every Pi with a 32-bit OS — the default PI_ARCH
make build_arm7        # Pi 2/3/4/Zero2, 32-bit OS
make build_arm64       # Pi 3/4/5/Zero2, 64-bit OS
make build_arm6_dev    # + Swagger UI (-tags swagger); _dev variants exist per arch
make test              # go test -race ./...
make lint              # gofmt, go fix -diff, go mod tidy -diff, go vet, golangci-lint (.golangci.yml), govulncheck - for $(PI_ARCH), with and without -tags swagger
make deploy            # build for $(PI_ARCH) then scp to $(PI_USER)@$(PI_HOST)
make clean
```

`ensure_dev_certs` (a prerequisite of every build target and of `make test`) generates `app/certs/dev_{cert,key}.pem` if missing; these are `//go:embed`-ed and gitignored, so a fresh clone must build via `make`, not bare `go build`. `VERSION` (`app/app.go`), `buildDate` and `buildCommit` (`cmd/main.go`) are `var`s injected via `-ldflags` — never edit them in source. The Makefile derives `VERSION` from `git describe --tags`; GoReleaser uses the tag itself.

### Releases

**There is one branch, `main`: work is committed to it and a release is a tag on it.** `make release TAG=vX.Y.Z` refuses to run from any other branch, with a dirty tree, or when `main` and `origin/main` differ; `.github/workflows/release.yml` re-checks that the tagged commit is on `main`. Versioning is SemVer and the Git tag is the single source of truth; the release workflow runs `goreleaser release --clean`, which builds linux arm64/armv7/armv6 and publishes a GitHub release with checksums and a grouped changelog. The `before` hook of `.goreleaser.yaml` must keep running `make ensure_dev_certs`. The archives carry `contrib/smartmeter.service`, the sandboxed unit the README installs (root-owned `/opt/smartmeter`, nothing writable, `CAP_NET_BIND_SERVICE` only, `DeviceAllow=/dev/ttyS0`, `RestartPreventExitStatus=78`). Validate changes with `goreleaser check` and `goreleaser release --snapshot --clean`.

`.github/workflows/ci.yml` runs on every push/PR against `main`, weekly (Monday 05:17 UTC) and by hand; `setup-go` takes the Go version from `go.mod` with `check-latest`, so the weekly run catches a toolchain advisory without a commit. It has a `test` job (native, `make test`) and a `build` matrix over `PI_ARCH` arm6/arm7/arm64 that runs `make lint` and `make build_<arch> build_<arch>_dev`; `release.yml` runs `make test` and `make lint` before GoReleaser. The job sets no `GOOS`/`GOARCH` of its own, or `make lint` would build the analysers for arm. golangci-lint keeps the default linters; `.golangci.yml` holds the exclusions, each one a decision with its reason (so far only errcheck in `_test.go`) — fix a finding rather than adding one. Actions are pinned to a commit SHA with the release in a comment, `govulncheck` and `golangci-lint` to a version in the Makefile only; `.github/dependabot.yml` updates actions (`chore(ci)`) and Go modules (`chore(deps)`, Swagger in a group of its own) weekly, but not the `go install` pins — raise those by hand.

`PI_USER`/`PI_HOST`/`PI_PATH` default to placeholders — real host names and the production config stay out of this public repository. Set the device via environment variables or `Makefile.local` (gitignored).

### Swagger

Swagger UI is behind the `swagger` build tag (`app/swagger.go` vs `app/swagger_stub.go`). Regenerate `docs/` after changing API handlers:

```sh
docs/generate.sh   # from the project root; needs swaggo/swag v1.16.6 installed
```

### Screenshots

The README screenshots and the social preview are rendered from the real `app/ui/index.html` with a mocked `/health` in headless Chromium. Re-run after visible UI changes; the social preview is uploaded by hand in the repository settings:

```sh
docker run --rm -v "$PWD":/src -w /src mcr.microsoft.com/playwright/python:v1.52.0-noble \
  sh -c 'pip install -q --break-system-packages playwright==1.52.0 && python3 docs/screenshots/capture.py'
```

### Running locally

```sh
go run ./cmd/main.go --config config/config.yaml --debug
```

For a test next to a running instance, copy the config, remove the `listen.rtu` block and set `listen.tcp.port: 1502`; the upstream meter is only read.

## Architecture

Layering is strict: `cmd` → `app` → `app/service/*` → `pkg/*`. Lower layers never import upward.

- **`cmd/main.go`** — flags, the `token` subcommand (`web.TokenCommand`, dispatched before the flags are parsed), config load/validate (exit code 78 when it or the log destination fails at start, 2 for a flag error; `cmd/main_test.go` pins the exit codes and the fallback to `lastGood`), logger init (golib `xlog`; a reload sets the new logger before closing the old one), and the **restart loop**. Signals are subscribed **once** in `run()` and handed to every `App`; never `signal.Stop`/`Reset` them inside `app`. A SIGHUP calls `checkReload` (load + validate the file) **before** tearing anything down, so a broken file is refused. A restart that passes the check but fails in `Run` (TLS, port 502 or the serial line busy, upstream meter unreachable) falls back to `lastGood`; only a failing first start exits. `README.md` is `//go:embed`-ed as `--help` output.
- **`app/app.go`** — wiring and lifecycle. `Init` builds the Modbus server and the polling service, `Run` starts server, polling and web server, and the signal handler last (one running during `abort` could take a signal and block on `app.restart`); a failure on the way is undone by `abort` (cancel, `wg.Wait`, `Cleanup`), which releases port 502 and the serial line for the next `App`. `Cleanup` stops polling before closing the server. The signal goroutine is the only caller of `shutdownProcedure`; a web server that stops on its own reports on `app.serverErr`.
- **`app/config.go`** — YAML decoded with `KnownFields(true)` (unknown keys are an error), only `${VAR}` expanded (never a bare `$`), durations need a unit, defaults from `NewConfig()`, `Validate()` (applies the per-meter defaults and sets `MeterConfig.Name` from the map key). `LoadConfig` reads the bearer token hashes from `webserver.tokenFile` (relative to the config file; `readConfig` alone, for `TokenFile` and `smartmeter token`, does not), so a reload picks up a new token and a missing or broken file is an error. `apiKey` is still a field only so `Validate` can reject it with a message naming the replacement; `renamedKeys` covers keys yaml.v3 itself rejects. Keys are camelCase; the keys under a meter's `map:` are canonical field names (snake_case data, not schema).
- **`app/routes.go` / `api_*.go`** — the web page `/` and `/version` public; `/health` and `/registers?unit=N` behind `web.WithAuth` with `webConfig()` — `Authorization: Bearer`, tokens from the token file, nothing else; `web.UserFrom` names the caller. `MeterStatus.Ready` (no valid snapshot for `staleAfter` (3) poll intervals) and the RTU port state are in `/health`. Middleware, outermost first: golib `web.WithLogging` (success at debug, rejected requests as warnings, first access of each caller at info) → `WithIPFilter` → `WithCORS`; `WithCORS` answers preflights itself, so there is no `OPTIONS /` route — it would turn the mux's 404 for an unknown path into 405.
- **`app/ui/index.html` / `api_ui.go`** — the web page, one self-contained file embedded with `//go:embed` (no external scripts or fonts, strict CSP). It polls `/health` every 2 s (served values, limits, request counters for the LEDs) and `/registers` while the register view is open. Keep it in step with `MeterStatus`, `Activity` and `RegisterDump`.
- **`app/webservices.go`** — HTTPS only, TLS 1.3 (`newTLSConfig`, pinned in `TestTLSConfigRequires13`); the embedded dev cert is used only with `env: dev`.
- **`app/service/meters`** — the domain layer. `config.go` holds `MeterConfig` and friends with their validation; `service.go` compiles each meter's map into read blocks (`buildReadBlocks`, merged across `maxBlockGap`), polls it, decodes, applies fixed/expr mappings and `applyDerivedFields`, rejects implausible snapshots (`checkPlausibility`, limits from `maxCurrent`) and writes the same snapshot to every unit ID — only `FieldModbusAddr` (40068) differs. The unit IDs are offline (`mbserver.SetOnline`) until the first valid snapshot and again after `staleTimeout` without one (`checkStale`); offline keeps the registers, so they come back without a moment of empty registers. After a failed read `simonRegisterReader` closes the source connection and opens it again before the next read; `pollReporter` logs the first failure, one a minute and the recovery. `server.go` owns the read-only Modbus server (`github.com/womat/mbserver`, TCP and RTU; write function codes are refused) and writes each snapshot with one `UpdateHoldingRegisters` call, so a client never reads a torn 32-bit value or a half-written snapshot; it also reports the listeners' request counters (`Activity`) and dumps both maps per unit ID (`Registers`, decoded with `fronius.DecodeEntry`); `expr.go` the small expression parser (`{field}`, + − * /, `SQRT`, `ABS`).
- **`pkg/fronius`** — canonical fields, the fixed target register maps (`ProprietaryRegisterMap`, `SunSpecRegisterMap`) and the `Snapshot` → `RegisterBank` encoding (register = round(value / 10^SF), high word first).

**External dependencies:** `github.com/womat/golib/v2` supplies `web` (auth, CORS, IP filter, `Encode`). The upstream client is `github.com/simonvetter/modbus`; its server cannot do RTU, hence `github.com/womat/mbserver` (own fork) for the server side. mbserver opens the serial port itself and detects the end of an RTU request by the t3.5 inter-frame delay; `Close` disconnects TCP clients, so after a reload nobody is served by the old instance. Read them in `$(go env GOMODCACHE)` when behavior is unclear.

### Register maps — keep them as they are

- **Proprietary map (18, 768, 4096–4176)**: not published by Fronius. It was recorded in 2020 by listening to the RS485 traffic between a Fronius inverter and a real Smart Meter 63A-3 and is partly confirmed by the LoxWiki. Keep it **bit-identical** to the emulator of 2020 (types, scaling, high word first, gaps stay 0); the inverter has accepted exactly this since 2020. Do not "correct" it from documents; change it only after a live comparison or a new recording. The TS 65A-3 uses a different map.
- **SunSpec block (40000–40177)**: SunSpec models 1 and 203 and Fronius' `Smart_Meter_Register_Map_Int&SF.xlsx`. The documents are 1-based: address = documented register − 1. PF is published in percent with PF_SF −1; the end block sits at 40176/40177.
- `TestPollAndUpdateWritesLegacyFroniusMap` pins the proprietary map against values read from the productive old emulator.

## Conventions

- Logging is `log/slog` with key/value pairs throughout; `slog.SetDefault` is set once in `cmd`.
- Doc comments: every package and exported symbol is documented, in English; Swagger annotations live on the handlers.
- No "master"/"slave" wording anywhere — use server/client and unit ID.
- Config field docs live in `README.md` and `config/config.yaml` — update both when adding a config key. `cmd/README.md` is the short `--help` text and only points to them.
- Commit subjects follow Conventional Commits, `type(scope): description` with an optional scope (`fix: default port 8443`, `feat(ui): …`), types `feat`, `fix`, `docu`, `chore`, `refactor`. The release changelog groups on them (`.goreleaser.yaml`).
