# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

`smartmeter` is a Go daemon that polls an energy meter over Modbus TCP or RTU and serves its values as a **Fronius Smart Meter 63A-3**: the proprietary RS485 map polled by Fronius inverters (unit ID 1) and the SunSpec meter block (model 203) read by wallboxes and evcc over Modbus TCP (unit ID 200). It also serves a TLS REST API. Single binary, cross-compiled for the Pi. It replaces the emulator of 2020 (`v1.0.23`, `cmd/emu.go` in the history), which served only the proprietary map.

## Build / develop

Nothing here is Linux-only, so `go build ./...`, `go vet ./...` and `go test -race ./...` work on macOS too.

```sh
make build_arm6        # Pi 1 / Zero and every Pi with a 32-bit OS — the default PI_ARCH
make build_arm7        # Pi 2/3/4/Zero2, 32-bit OS
make build_arm64       # Pi 3/4/5/Zero2, 64-bit OS
make build_arm6_dev    # + Swagger UI (-tags swagger); _dev variants exist per arch
make test              # go test -race ./...
make deploy            # build for $(PI_ARCH) then scp to $(PI_USER)@$(PI_HOST)
make clean
```

`ensure_dev_certs` (a prerequisite of every build target and of `make test`) generates `app/certs/dev_{cert,key}.pem` if missing; these are `//go:embed`-ed and gitignored, so a fresh clone must build via `make`, not bare `go build`. `VERSION` (`app/app.go`), `buildDate` and `buildCommit` (`cmd/main.go`) are `var`s injected via `-ldflags` — never edit them in source. The Makefile derives `VERSION` from `git describe --tags`; GoReleaser uses the tag itself.

### Releases

**There is one branch, `main`: work is committed to it and a release is a tag on it.** `make release TAG=vX.Y.Z` refuses to run from any other branch, with a dirty tree, or when `main` and `origin/main` differ; `.github/workflows/release.yml` re-checks that the tagged commit is on `main`. Versioning is SemVer and the Git tag is the single source of truth; the release workflow runs `goreleaser release --clean`, which builds linux arm64/armv7/armv6 and publishes a GitHub release with checksums and a grouped changelog. The `before` hook of `.goreleaser.yaml` must keep running `make ensure_dev_certs`. Validate changes with `goreleaser check` and `goreleaser release --snapshot --clean`.

`.github/workflows/ci.yml` runs on every push/PR against `main`: `make test` and a build matrix over armv6/armv7/arm64 that vets, builds (also `-tags swagger`) and runs govulncheck. Actions are pinned to a commit SHA; dependabot updates them and the Go modules, but not the `go install` pins.

`PI_USER`/`PI_HOST`/`PI_PATH` default to placeholders — real host names and the production config stay out of this public repository. Set the device via environment variables or `Makefile.local` (gitignored).

### Swagger

Swagger UI is behind the `swagger` build tag (`app/swagger.go` vs `app/swagger_stub.go`). Regenerate `docs/` after changing API handlers:

```sh
docs/generate.sh   # from the project root; needs swaggo/swag v1.16.6 installed
```

### Running locally

```sh
go run ./cmd/main.go --config config/config.yaml --debug
```

For a test next to a running instance, copy the config and set `listen.rtu.enabled: false` and `listen.tcp.port: 1502`; the upstream meter is only read.

## Architecture

Layering is strict: `cmd` → `app` → `app/service/*` → `pkg/*`. Lower layers never import upward.

- **`cmd/main.go`** — flags, config load/validate, logger init (`newLogger`, plain `log/slog`), and the **restart loop**. Signals are subscribed **once** in `run()` and handed to every `App`; never `signal.Stop`/`Reset` them inside `app`. A SIGHUP calls `checkReload` (load + validate the file) **before** tearing anything down, so a broken file is refused. A restart that passes the check but fails in `Run` (TLS, port 502 or the serial line busy, upstream meter unreachable) falls back to `lastGood`; only a failing first start exits. `README.md` is `//go:embed`-ed as `--help` output.
- **`app/app.go`** — wiring and lifecycle. `Init` builds the Modbus server and the polling service, `Run` starts server, polling, signal handler and web server; a failure on the way is undone by `abort` (cancel, `wg.Wait`, `Cleanup`), which releases port 502 and the serial line for the next `App`. `Cleanup` stops polling before closing the server. The signal goroutine is the only caller of `shutdownProcedure`; a web server that stops on its own reports on `app.serverErr`.
- **`app/config.go`** — YAML decoded with `KnownFields(true)` (unknown keys are an error), only `${VAR}` expanded (never a bare `$`), durations need a unit, defaults from `NewConfig()`, `Validate()` (applies the per-meter defaults and sets `MeterConfig.Name` from the map key) and `Warnings()`. Keys are camelCase; the keys under a meter's `map:` are canonical field names (snake_case data, not schema).
- **`app/routes.go` / `api_*.go`** — `/version` and `/ready` public, `/health` behind `web.WithAuth` (`X-API-Key`). `/ready` is 503 while a meter has no valid snapshot for `staleAfter` (3) poll intervals. Middleware, outermost first: `WithLogging` → `WithIPFilter` → `WithCORS`.
- **`app/webservices.go`** — HTTPS only; the embedded dev cert is used only with `env: dev`.
- **`app/service/meters`** — the domain layer. `config.go` holds `MeterConfig` and friends with their validation; `service.go` compiles each meter's map into read blocks (`buildReadBlocks`, merged across `maxBlockGap`), polls it, decodes, applies fixed/expr mappings and `applyDerivedFields`, rejects implausible snapshots (`checkPlausibility`, limits from `maxCurrent`) and writes the same snapshot to every unit ID — only `FieldModbusAddr` (40068) differs. `server.go` owns the Modbus TCP/RTU listeners — the RTU port is opened with `jacobsa/go-serial` on purpose: its VMIN/VTIME read returns a whole frame, which mbserver requires (one `Read` = one frame); `goburrow/serial` would deliver partial frames; `expr.go` the small expression parser (`{field}`, + − * /, `SQRT`, `ABS`).
- **`pkg/fronius`** — canonical fields, the fixed target register maps (`ProprietaryRegisterMap`, `SunSpecRegisterMap`) and the `Snapshot` → `RegisterBank` encoding (register = round(value / 10^SF), high word first).
- **`pkg/mbserver`** — read-only wrapper around `github.com/womat/mbserver`. The library handles requests in its own goroutine without locking, so function code 3 is served under the wrapper's `RWMutex` and `WriteSnapshot` updates registers under the write lock — this prevents torn 32-bit reads (`TestNoTornReads` fails under `-race` without it). Write function codes are refused.

**External dependency `github.com/womat/golib`** supplies `web` (auth, CORS, IP filter, `Encode`). The upstream client is `github.com/simonvetter/modbus`. Read them in `$(go env GOMODCACHE)` when behavior is unclear.

### Register maps — keep them as they are

- **Proprietary map (18, 768, 4096–4176)**: not published by Fronius. It was recorded in 2020 by listening to the RS485 traffic between a Fronius inverter and a real Smart Meter 63A-3 and is partly confirmed by the LoxWiki. Keep it **bit-identical** to the old emulator (types, scaling, high word first, gaps stay 0); the inverter has accepted exactly this since 2020. Do not "correct" it from documents; change it only after a live comparison or a new recording. The TS 65A-3 uses a different map.
- **SunSpec block (40000–40177)**: SunSpec models 1 and 203 and Fronius' `Smart_Meter_Register_Map_Int&SF.xlsx`. The documents are 1-based: address = documented register − 1. PF is published in percent with PF_SF −1; the end block sits at 40176/40177.
- `TestPollAndUpdateWritesLegacyFroniusMap` pins the proprietary map against values read from the productive old emulator.

## Conventions

- Logging is `log/slog` with key/value pairs throughout; `slog.SetDefault` is set once in `cmd`.
- Doc comments: every package and exported symbol is documented, in English; Swagger annotations live on the handlers.
- No "master"/"slave" wording anywhere — use server/client and unit ID.
- Config field docs live in `README.md` and `config/config.yaml` — update both when adding a config key. `cmd/README.md` is the short `--help` text and only points to them.
- Commit subjects use the prefixes `feat()`, `fix()`, `docu()`, `chore()`, `refactor()`; the release changelog groups on them (`.goreleaser.yaml`).
