# wasm-microkernel

A Go SDK for building host applications and WebAssembly plugins on top of
wazero + wasip1. The SDK hides all fat-pointer / linear-memory bookkeeping
behind two small packages so plugin authors and host applications never
touch raw Wasm ABI.

## Project Type

Go library (no frontend/backend, no runnable web app).

## Layout (v0.6.0)

- `abi/ptr.go` — shared `Encode` / `Decode` for the `(ptr<<32 | len)` fat-pointer
  protocol used on both sides of the boundary.
- `guest/` *(`//go:build wasip1`)* — plugin-side SDK.
  - `guest.go` exports `allocate` and `execute`; plugins call `Register(handler)`
    from `init()`.
  - `host_funcs.go` wraps the raw `//go:wasmimport` declarations under the
    `podpedia_host` module (`log_msg`, `http_post`).
- `host/` — embed-side SDK.
  - `engine.go` — `Engine` wraps wazero, instantiates the `podpedia_host`
    capability module, and exposes `Execute(ctx, wasmBytes, reqJSON)`.
  - `host_funcs.go` — Go implementations of the host capabilities.

The plugin/host wire format prefixes every `execute`/`http_post` response
with one byte: `0` for success, `1` for an error string.

## Stack

- Go 1.26.1
- `github.com/tetratelabs/wazero` v1.11.0 — only direct dependency.

## Replit Setup

A console workflow named **Tests** runs `go test ./...`. There is no web
server to deploy.

## Common Commands

```bash
go build ./...
go test ./...
GOOS=wasip1 GOARCH=wasm go build ./guest   # cross-compile guest SDK
```
