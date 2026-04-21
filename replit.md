# wasm-microkernel

A Go SDK library implementing a WebAssembly microkernel architecture for loading and executing third-party plugins as WASI Reactors.

## Project Type

Go library (not a runnable web/server application). There is no frontend or backend — this is a reusable SDK consumed by other Go projects.

## Stack

- **Language:** Go 1.26.1
- **Key dependencies:** `github.com/tetratelabs/wazero` (Wasm runtime), `github.com/eliben/watgo`, `github.com/bytecodealliance/wasm-tools-go`

## Layout

- `abi/` — guest-side memory translation helpers (fat pointers, host buffer reads)
- `capabilities/` — host capability definitions
- `guest/`, `guest-bindings/` — guest-side bindings (WIT-generated)
- `host/` — host-side builder utilities
- `kernel/` — microkernel implementation
- `plugintest/` — local test harness powered by wazero
- `wit/` — WIT interface definitions

## Replit Setup

A console workflow named **Tests** runs `go test ./...` to validate the library on demand. There is no web server to deploy.

## Common Commands

```bash
go build ./...
go test ./...
```
