# `wasm-microkernel` v0.6.0

A small Go SDK for embedding WebAssembly plugins in a host application,
built directly on [`wazero`](https://github.com/tetratelabs/wazero) and
`GOOS=wasip1 GOARCH=wasm`. The SDK hides every fat-pointer / linear-memory
detail behind two short packages so plugin authors and host applications
never touch raw Wasm ABI.

> **Why v0.6.0?** Earlier drafts of this repo experimented with WIT
> definitions and `wit-bindgen-go`. In practice, `wit-bindgen-go` only
> emits guest-side bindings and produces no host shim for `wazero`, so
> the WIT layer added ceremony without simplifying anything. v0.6.0 drops
> WIT entirely in favor of a tiny, hand-written fat-pointer protocol that
> `wazero` + `wasip1` actually support today.

## Repository Layout

```text
wasm-microkernel/
├── abi/
│   └── ptr.go         # Shared (ptr<<32 | len) fat-pointer encoding
├── guest/
│   ├── guest.go       # Plugin-side: exports `allocate` and `execute`
│   └── host_funcs.go  # Plugin-side: clean wrappers around //go:wasmimport
└── host/
    ├── engine.go      # Host-side: wazero wrapper and lifecycle
    └── host_funcs.go  # Host-side: real implementations of capabilities
```

Only `github.com/tetratelabs/wazero` is a direct dependency.

## Architecture in One Picture

```
┌──────────────────────┐                ┌────────────────────────┐
│  Your host app       │                │  Your plugin (.wasm)   │
│  (e.g. podpedia)     │                │  GOOS=wasip1           │
│                      │                │                        │
│  engine.Execute(…)   │ ── execute ──▶ │  guest.execute         │
│                      │                │   └ pluginHandler(req) │
│  hostHttpPost ◀───── │ ── http_post ──┤  guest.HttpPost(url,…) │
│  hostLogMsg   ◀───── │ ── log_msg  ───┤  guest.LogMsg("…")     │
└──────────────────────┘                └────────────────────────┘
        host/                                     guest/
```

All cross-boundary values are encoded as a single 64-bit fat pointer
(`ptr<<32 | len`) using `abi.Encode` / `abi.Decode`. Responses use a
1-byte framing flag: `0` for success, `1` for an error string.

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

        body, err := guest.HttpPost("http://ollama.local/api/generate", reqJSON)
        if err != nil {
            return "", err
        }
        return body, nil
    })
}

// Required by the Go toolchain; never executed in reactor mode.
func main() {}
```

You import only `github.com/gavmor/wasm-microkernel/guest`. There are no
`//go:wasmimport` or `//go:wasmexport` directives in your plugin code —
the SDK provides them.

### Available capabilities (today)

| Function                          | Purpose                                  |
| --------------------------------- | ---------------------------------------- |
| `guest.LogMsg(msg string)`        | Fire-and-forget log line to the host.    |
| `guest.HttpPost(url, body) (s, e)`| POST `body` to `url`; returns response.  |

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

    engine, err := host.NewEngine(ctx)
    if err != nil {
        log.Fatal(err)
    }
    defer engine.Close(ctx)

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

- `host.NewEngine(ctx) (*Engine, error)`
- `(*Engine).Execute(ctx, wasmBytes []byte, reqJSON string) (string, error)`
- `(*Engine).Close(ctx) error`

The host application never imports `wazero`, never sees a pointer, and
never marshals a fat pointer.

## Wire Protocol

You only need this if you are debugging at the byte level.

- **Inputs and outputs** are passed as a single `i64` fat pointer:
  `(uint64(ptr) << 32) | uint64(len)`.
- **`execute(ptr, len) -> u64`** is the only plugin export the host
  invokes. The plugin allocates its own response buffer and returns its
  fat pointer.
- **`allocate(size) -> u32`** is exported by the plugin so the host can
  reserve guest memory for the request payload before calling `execute`.
  `size == 0` is well-defined: the plugin returns `0` and the host skips
  the write.
- **Response framing:** the first byte of every `execute` and
  `http_post` response is `0` (success) or `1` (error). The remaining
  bytes are UTF-8.
- **`http_post` request payload:** `"<url>\x00<body>"` so the host can
  split URL from body without a second allocation.
- All host capabilities live under the wazero module name
  **`podpedia_host`** (e.g. `podpedia_host.log_msg`,
  `podpedia_host.http_post`).

## Concurrency and Lifecycle

- `host.Execute` compiles, instantiates, runs, and closes a fresh module
  per call. Heavy use should cache compiled modules in your own code.
- `_initialize` runs on every instantiation, so plugin `init()`s execute
  before `execute` is called.
- Plugins must not assume thread safety; the wazero Go runtime is
  single-threaded per module instance.

## Local Development

```bash
go build ./...                                       # native: host SDK
GOOS=wasip1 GOARCH=wasm go build ./guest             # cross-compile guest SDK
go test ./...                                        # run tests
```

## Changelog

- **v0.6.0** — Rewrote the SDK around a minimal fat-pointer protocol.
  Removed `wit/`, `guest-bindings/`, `kernel/`, `capabilities/`, and
  `plugintest/` packages. The host now exposes a single `Engine` type;
  plugins import only the `guest` package.
