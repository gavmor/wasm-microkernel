# wasm-microkernel

A Go SDK for building host applications and WebAssembly plugins on top of
[Extism](https://extism.org/) (which uses [wazero](https://wazero.io)
under the hood). The SDK is a thin facade: plugin authors and host
applications never touch Extism types or memory APIs.

## Project Type

Go library (no frontend/backend, no runnable web app).

## Layout (v0.7.0)

- `guest/guest.go` *(`//go:build wasip1`)* — plugin-side SDK. Wraps the
  Extism PDK and exposes `Register`, `LogMsg`, `HttpPost`. Exports the
  canonical `execute` symbol the host engine invokes.
- `host/engine.go` — embed-side SDK. `Engine` wraps Extism, with an
  `AllowedHosts` field that controls outbound HTTP from plugins. Exposes
  `Execute(ctx, wasmBytes, reqJSON)`.
- `host/engine_test.go` — integration tests (7 cases; ~12 s total).
- `host/testdata/ping/` — minimal echo/error/log/http test plugin source.
- `host/testdata/ping.wasm` — committed pre-compiled artifact.

That is the entire SDK: two source files, ~130 LOC.

## Stack

- Go 1.26.1
- `github.com/extism/go-sdk` — host runtime (wraps wazero).
- `github.com/extism/go-pdk` — guest plugin development kit.

## Replit Setup

A console workflow named **Tests** runs `go test ./...`. There is no web
server to deploy.

## Common Commands

```bash
go build ./...                                                    # native: host SDK
GOOS=wasip1 GOARCH=wasm go build ./guest                          # verify guest SDK cross-compiles
go generate ./host/...                                            # rebuild host/testdata/ping.wasm
go test ./...                                                     # run integration tests
```
