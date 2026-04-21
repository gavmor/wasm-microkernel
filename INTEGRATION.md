# Integrating `wasm-microkernel` into a Go Program

This guide walks you through embedding `wasm-microkernel` in an existing
Go application — the kind of host that wants to load third-party
WebAssembly plugins at runtime, hand them a JSON request, and trust them
with a small, well-defined set of capabilities.

If you are writing the **plugin** rather than the host, see the README
("Step 1: Write a Plugin"). This guide is for the **host**.

---

## 1. What You Get

Embedding the SDK into your program gives you:

- A single `host.Engine` value that owns no resources directly but
  delegates plugin execution to Extism (which uses `wazero` internally).
- A single call — `engine.Execute(ctx, wasmBytes, reqJSON)` — that
  boots, runs, and tears down a plugin.
- An `AllowedHosts` field that declaratively controls which URLs your
  plugins can reach over HTTP.
- Zero exposure to Extism, `wazero`, or linear memory in your
  application code. Plugins are opaque `[]byte`; requests and responses
  are opaque `string`.

---

## 2. Add the Dependency

```bash
go get github.com/gavmor/wasm-microkernel@latest
```

The SDK pulls in `github.com/extism/go-sdk` and its transitive
dependencies (Extism, `wazero`, OpenTelemetry plumbing). Import the host
package wherever you plan to load plugins:

```go
import "github.com/gavmor/wasm-microkernel/host"
```

You do **not** import `guest/` from your host code — that package is
`//go:build wasip1`-gated and only compiles for plugins.

---

## 3. The Minimum Viable Integration

The simplest possible host loads one plugin from disk and runs it once:

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
    engine.AllowedHosts = []string{"api.example.com"}

    wasmBytes, err := os.ReadFile("plugins/extractor.wasm")
    if err != nil {
        log.Fatalf("read plugin: %v", err)
    }

    out, err := engine.Execute(ctx, wasmBytes, `{"episode_id":"123"}`)
    if err != nil {
        log.Fatalf("plugin: %v", err)
    }
    fmt.Println(out)
}
```

That is the entire integration surface. Everything below is optional
hardening for production hosts.

---

## 4. Engine Lifecycle

`host.Engine` is a lightweight value type. `NewEngine()` allocates
nothing besides the struct itself; the cost lives in each `Execute`
call, where Extism instantiates the plugin. You can:

- Create one engine at startup and share it across goroutines.
- Create a fresh engine per request (fine; it is cheap).
- Mutate `AllowedHosts` between calls (each `Execute` snapshots it into
  the Extism manifest).

`Close(ctx)` is a no-op today, retained for API stability in case future
versions add per-engine pools.

---

## 5. Concurrency Model

Each call to `engine.Execute` boots a fresh Extism plugin instance,
calls `execute`, and tears it down. Because every call gets its own
instance, **concurrent calls on the same `Engine` are safe** — they do
not share linear memory or Go runtime state.

A single plugin instance is single-threaded inside one `execute` call;
plugins do not need to be reentrancy-safe with respect to themselves.

---

## 6. Request and Response Conventions

The SDK is deliberately unopinionated about your payload format: it
hands the plugin a `string` and expects a `string` back. In practice
nearly every host will want JSON. A useful idiom is to push the
encode/decode into a small wrapper around `Execute`:

```go
func Run[Req any, Resp any](
    ctx context.Context,
    eng *host.Engine,
    wasmBytes []byte,
    req Req,
) (Resp, error) {
    var zero Resp

    reqJSON, err := json.Marshal(req)
    if err != nil {
        return zero, fmt.Errorf("marshal request: %w", err)
    }

    respJSON, err := eng.Execute(ctx, wasmBytes, string(reqJSON))
    if err != nil {
        return zero, err
    }

    var resp Resp
    if err := json.Unmarshal([]byte(respJSON), &resp); err != nil {
        return zero, fmt.Errorf("unmarshal response: %w", err)
    }
    return resp, nil
}
```

When `Execute` returns:

- A non-nil `error` means the plugin trapped, called `pdk.SetError`
  (which Extism surfaces as the error text), or Extism itself failed to
  load the module. The error text already includes the plugin's message.
- A nil `error` plus a `string` is the plugin's success payload.

You do not handle any framing bytes, exit codes, or memory pointers
yourself.

---

## 7. Loading Plugins

The SDK takes raw `[]byte`, so where the plugin comes from is your
choice. Common sources:

**From the local filesystem:**

```go
wasmBytes, err := os.ReadFile(path)
```

**From an embedded `embed.FS`** (good for shipping a few first-party
plugins inside your binary):

```go
//go:embed plugins/*.wasm
var pluginsFS embed.FS

wasmBytes, err := pluginsFS.ReadFile("plugins/extractor.wasm")
```

**From an OCI registry** (recommended for third-party / versioned
plugins): use `oras-go` or your registry client of choice and pass the
resulting bytes to `Execute`.

Treat the resulting `[]byte` as immutable and cache it — re-reading
from disk on every request is wasteful.

---

## 8. Error Handling

The errors `Execute` can return fall into a few categories:

| Category               | What it looks like                            | Typical response                    |
| ---------------------- | --------------------------------------------- | ----------------------------------- |
| Bad plugin binary      | `initializing plugin: …`                      | Reject upload; log; alert.          |
| Trap / panic in guest  | `plugin error: <wasm trap text>`              | Surface to caller; do not retry.    |
| Plugin business error  | `plugin error: <pdk.SetError message>`        | Surface to caller.                  |
| Disallowed HTTP        | Plugin's own error from `guest.HttpPost`      | Caller decides; usually log+reject. |
| Non-zero exit code     | `plugin exited with code N`                   | Plugin contract violation.          |

A plugin trap is **not** fatal to your host process — Extism contains
the trap inside the plugin instance, which is closed before `Execute`
returns. You can immediately call `Execute` again.

---

## 9. Logging

Plugins that call `guest.LogMsg(...)` produce one log line per call
through Extism's logger. By default Extism writes to its configured
logger sink; the SDK does not currently override this. If you want to
route plugin logs into your `slog.Logger`, configure Extism's logger
directly via the upstream `extism.SetLogLevel` / log functions before
boot.

---

## 10. Going Beyond the Defaults

### Restricting `http_post`

This is built in. Set `Engine.AllowedHosts` to the list of glob patterns
plugins are permitted to POST to. Patterns match the hostname only:

```go
engine.AllowedHosts = []string{
    "*.deepgram.com",
    "api.openai.com",
    "127.0.0.1",
}
```

An empty list disables outbound HTTP entirely. Plugins that attempt a
disallowed POST receive an HTTP error through `guest.HttpPost`.

### Adding New Capabilities

The current SDK exposes Extism's built-in HTTP and logging. To add a
custom host capability (e.g. `file_read`, `secret_get`), extend
`host/engine.go` to construct an Extism `HostFunction` and pass it as
the fourth argument to `extism.NewPlugin`. Then add a thin wrapper to
`guest/guest.go` that calls `pdk.HostFunctionCall(name, payload)`.
This requires understanding the upstream Extism API; the README
intentionally does not document it because the built-in capabilities
cover most needs.

### Per-Call Timeouts

`Execute` honors the `context.Context` you pass it. A `context.WithTimeout`
will cancel the plugin if it runs too long:

```go
callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()
out, err := engine.Execute(callCtx, wasmBytes, reqJSON)
```

A timeout is the only safe way to bound a misbehaving plugin's CPU
usage; do not omit this in production.

---

## 11. Checklist Before Shipping

- [ ] `Engine.AllowedHosts` is set to the minimum set of hostnames your
      plugins actually need (or empty if they do not need HTTP).
- [ ] Every `Execute` call is wrapped in a `context.WithTimeout`.
- [ ] Plugin `[]byte` is loaded once and reused — not re-read per call.
- [ ] Errors from `Execute` are surfaced to the caller verbatim.
- [ ] If you load untrusted third-party plugins, you have an
      out-of-band review process — Extism sandboxes execution, but the
      capability surface (HTTP, logs) is still attack surface.

That is the complete integration surface. The SDK is small on purpose:
the parts you need to fork — capability policies, plugin distribution,
log routing — are exactly the parts that should be tailored to your
host.
