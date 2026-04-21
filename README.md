# `wasm-microkernel` v0.7.0

A small Go SDK for embedding WebAssembly plugins in a host application.
A thin facade over [Extism](https://extism.org/) (which uses
[`wazero`](https://github.com/tetratelabs/wazero) under the hood).

The SDK has one purpose: **give plugin authors the developer experience
of the Component Model — Go strings in, Go strings out, capabilities
that feel like standard library calls — using the most production-ready
Wasm plugin framework available for Go today.**

## What This Is (and What It Isn't)

This SDK is a **facade**. Plugin authors import only
`github.com/gavmor/wasm-microkernel/guest`; host applications import
only `github.com/gavmor/wasm-microkernel/host`. Behind both packages,
Extism manages WASI, linear memory, the host-call boundary, and the
allow-list-based HTTP capability.

### Why a facade and not "just use Extism"?

The Extism API is broad and evolving. Pinning every plugin in your
ecosystem to Extism's exact surface ties your plugin contract to a
third-party release schedule. The microkernel here is a stable shim:

- Plugins write against `guest.Register / LogMsg / HttpPost`. Three
  functions, plain Go types, zero `//go:wasmimport` directives.
- Hosts write against `host.NewEngine().Execute(...)`. One method.
- The day a better runtime ships (wazero's Component Model support,
  `wit-bindgen-go` host bindings, a different framework entirely), the
  internals of the two SDK files swap out and **plugin code does not
  change**. That is the entire point of the architecture.

### The actual differentiator: plugin DevEx insulation

Plugin authors write code like this:

```go
guest.Register(func(req string) (string, error) {
    guest.LogMsg("running...")
    body, err := guest.HttpPost("https://api.example.com/v1/things", req)
    if err != nil {
        return "", err
    }
    return body, nil
})
```

Standard `string`, standard `error`, capabilities that look and feel
like the standard library. The microkernel is the only thing that knows
the runtime is Extism underneath.

## Repository Layout

```text
wasm-microkernel/
├── guest/
│   └── guest.go     # Plugin-side SDK: Register, LogMsg, HttpPost
└── host/
    ├── engine.go    # Host-side SDK: Engine, NewEngine, Execute
    └── engine_test.go
```

Two source files, ~130 LOC. Direct dependencies are `extism/go-sdk` and
`extism/go-pdk`.

## Architecture in One Picture

```
┌──────────────────────┐                    ┌────────────────────────┐
│  Your host app       │                    │  Your plugin (.wasm)   │
│                      │                    │  GOOS=wasip1           │
│  engine.Execute(…)   │ ── execute ──────▶ │  guest.execute         │
│                      │                    │   └ pluginHandler(req) │
│  Extism HTTP host    │ ◀─── http ─────────┤  guest.HttpPost(url,…) │
│  (allow-list gate)   │                    │                        │
│  Extism logger       │ ◀─── log ──────────┤  guest.LogMsg("…")     │
└──────────────────────┘                    └────────────────────────┘
        host/                                         guest/
```

All cross-boundary marshaling is Extism's job. The SDK does no pointer
math, no fat-pointer encoding, no manual `linear-memory.Read/Write`.

## Prerequisites

- Go 1.26 or newer (the module declares `go 1.26.1`).
- Plugins must be built for `wasip1` in reactor mode (see Step 4).

## Step 1: Write a Plugin

A plugin is an ordinary Go program with one entry point. Register your
handler from `init()` — **not** `main()`, because reactor builds never
invoke `main`.

```go
package main

import (
    "github.com/gavmor/wasm-microkernel/guest"
)

func init() {
    guest.Register(func(reqJSON string) (string, error) {
        guest.LogMsg("running extractor for: " + reqJSON)

        body, err := guest.HttpPost("https://api.example.com/v1/things", reqJSON)
        if err != nil {
            return "", err
        }
        return body, nil
    })
}

// Required by the Go toolchain; never executed in reactor mode.
func main() {}
```

You import only `github.com/gavmor/wasm-microkernel/guest`.

### Available capabilities

| Function                          | Purpose                                              |
| --------------------------------- | ---------------------------------------------------- |
| `guest.LogMsg(msg string)`        | Fire-and-forget log line to the host.                |
| `guest.HttpPost(url, body) (s, e)`| POST `body` to `url`; subject to host's allow-list.  |

## Step 2: Compile the Plugin

Plugins are **WASI Reactors** — long-lived modules whose exports are
called many times. Use `c-shared` so the Go linker emits `_initialize`
instead of `_start`:

```bash
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o my-plugin.wasm
```

## Step 3: Run a Plugin from the Host

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/gavmor/wasm-microkernel/host"
)

func main() {
    ctx := context.Background()

    engine := host.NewEngine()
    // Allow-list which hosts plugins may POST to. Empty = no HTTP.
    engine.AllowedHosts = []string{"api.example.com"}

    wasmBytes, err := os.ReadFile("my-plugin.wasm")
    if err != nil {
        log.Fatal(err)
    }

    out, err := engine.Execute(ctx, wasmBytes, `{"episode_id":"123"}`)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(out)
}
```

That is the entire host surface area:

- `host.NewEngine() *Engine`
- `(*Engine).AllowedHosts []string` — glob patterns Extism enforces.
- `(*Engine).Execute(ctx, wasmBytes []byte, reqJSON string) (string, error)`
- `(*Engine).Close(ctx) error` — no-op today; reserved for future
  resource pools.

The host application never imports `extism` or `wazero`.

## Security: AllowedHosts

`Engine.AllowedHosts` is passed directly to Extism's `Manifest`. Patterns
match the **hostname only** (no scheme, no port). An empty list means
**no plugin can make outbound HTTP calls** — a safe default.

```go
engine.AllowedHosts = []string{
    "*.deepgram.com",   // glob: any subdomain
    "api.openai.com",   // exact hostname
    "127.0.0.1",        // any port on loopback (useful for httptest)
}
```

Plugins that try to reach a non-allow-listed host receive an HTTP error
through `guest.HttpPost`, which they can surface to the caller.

## Concurrency and Lifecycle

- `host.Execute` boots a fresh Extism plugin per call, runs `execute`,
  and tears it down. Concurrent calls on one `Engine` are safe; each
  call gets an isolated plugin instance with its own linear memory.
- `_initialize` runs on every instantiation, so plugin `init()`s
  (including `guest.Register`) execute before `execute` is called.

## Local Development

```bash
go build ./...                                       # native: host SDK
GOOS=wasip1 GOARCH=wasm go build ./guest             # cross-compile guest SDK
go generate ./host/...                               # rebuild host/testdata/ping.wasm
go test ./...                                        # run integration tests
```

## Changelog

- **v0.7.0** — Replaced the hand-rolled fat-pointer ABI with an Extism
  facade. Deleted `abi/`, `host/host_funcs.go`, `guest/host_funcs.go`,
  `host/builder.go`. SDK shrank from ~370 LOC to ~130 LOC. Plugin code
  did not change. `Engine.AllowedHosts` is the new security gate for
  outbound HTTP, replacing the old unrestricted `hostHttpPost`.
- **v0.6.0** — Rewrote the SDK around a minimal fat-pointer protocol.
  Removed `wit/`, `guest-bindings/`, `kernel/`, `capabilities/`, and
  `plugintest/` packages. The host now exposes a single `Engine` type;
  plugins import only the `guest` package.
