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

- A single `host.Engine` value that owns one `wazero` runtime and the
  pre-mounted host-capability module (`podpedia_host`).
- A single call — `engine.Execute(ctx, wasmBytes, reqJSON)` — that
  compiles, instantiates, runs, and tears down a plugin.
- Zero exposure to fat pointers, linear memory, or `wazero` types in
  your application code. Plugins are opaque `[]byte`; requests and
  responses are opaque `string`.

What you do **not** get out of the box (and how to add it) is covered
later in this document under "Going Beyond the Defaults."

---

## 2. Add the Dependency

```bash
go get github.com/gavmor/wasm-microkernel@latest
```

The SDK pulls in exactly one transitive dependency,
`github.com/tetratelabs/wazero`. Import the host package wherever you
plan to load plugins:

```go
import "github.com/gavmor/wasm-microkernel/host"
```

You do **not** import `abi/` or `guest/` from your host code — those
are for the plugin side and the cross-boundary protocol respectively.

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

    engine, err := host.NewEngine(ctx)
    if err != nil {
        log.Fatalf("engine: %v", err)
    }
    defer engine.Close(ctx)

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

`host.Engine` is meant to be **created once and reused**. Creating a new
engine per request is wasteful — each engine spins up a fresh `wazero`
runtime and re-mounts the capability module. A single engine is also
safe to share across goroutines, with the caveat in §5.

Recommended pattern in a long-running service:

```go
type Service struct {
    engine *host.Engine
    // ... your other fields ...
}

func NewService(ctx context.Context) (*Service, error) {
    eng, err := host.NewEngine(ctx)
    if err != nil {
        return nil, fmt.Errorf("plugin engine: %w", err)
    }
    return &Service{engine: eng}, nil
}

func (s *Service) Shutdown(ctx context.Context) error {
    return s.engine.Close(ctx)
}
```

`Close` releases the `wazero` runtime and any compiled modules it cached.
Always `Close` the engine on shutdown — preferably via `defer` or a
signal handler — so background goroutines and file descriptors held by
`wazero` are released cleanly.

---

## 5. Concurrency Model

Each call to `engine.Execute` performs five steps in order:

1. Compile the plugin bytes.
2. Instantiate a fresh module (which runs `_initialize`, including the
   plugin's `init()` functions).
3. Allocate guest memory for the request and write it.
4. Call `execute(ptr, len)`.
5. Read and unframe the response, then close the module instance.

Because every call gets its own module instance, **concurrent calls to
the same `Engine` are safe** — they do not share guest linear memory.

Two practical implications:

- **You can call `Execute` from many goroutines.** The capability host
  functions (`hostHttpPost`, `hostLogMsg`) receive a per-call `api.Module`
  from `wazero`, so they always read and write the right instance's
  memory.
- **A single plugin instance is single-threaded.** Inside one `execute`
  call, the plugin's Go runtime runs on one goroutine. Plugins do not
  need to be reentrancy-safe with respect to themselves.

If your throughput is high and compilation cost matters, see
"Caching Compiled Modules" in §10.

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
        return zero, err // already framed by the SDK
    }

    var resp Resp
    if err := json.Unmarshal([]byte(respJSON), &resp); err != nil {
        return zero, fmt.Errorf("unmarshal response: %w", err)
    }
    return resp, nil
}
```

The SDK's response framing — first byte `0` for success, `1` for
error — is decoded for you. By the time `Execute` returns:

- A non-nil `error` means the plugin trapped, returned a framed error,
  or the host could not allocate / read guest memory. The `error` text
  already includes the plugin's message.
- A nil `error` plus a `string` is the plugin's success payload, with
  the framing byte already stripped.

You do not need to inspect the first byte yourself.

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
plugins):

The SDK does not include an OCI client to keep dependencies minimal.
Use `oras-go` or your registry client of choice to pull the
`application/vnd.module.wasm.content.layer.v1+wasm` layer and pass the
bytes to `Execute`.

Whichever source you choose, treat the resulting `[]byte` as immutable
and cache it — re-reading from disk on every request is wasteful and
defeats the compiled-module cache (§10).

---

## 8. Error Handling

The errors `Execute` can return fall into a few categories. Handle them
the same way you would handle any external dependency:

| Category               | What it looks like                                     | Typical response                     |
| ---------------------- | ------------------------------------------------------ | ------------------------------------ |
| Bad plugin binary      | `compiling plugin: …`                                  | Reject upload; log; alert.           |
| Missing exports        | `plugin missing 'allocate' export`                     | Plugin is not built against the SDK. |
| Trap / panic in guest  | `plugin execution trapped: …`                          | Surface to caller; do not retry.     |
| Plugin business error  | `plugin logic error: <plugin's own message>`           | Surface to caller.                   |
| Empty / malformed reply| `plugin returned empty response`, `reading result …`   | Likely SDK-version mismatch.         |

A plugin trap is **not** fatal to your host process — `wazero` contains
the trap inside the module instance, which is closed before `Execute`
returns. You can immediately call `Execute` again with a fresh plugin
or different input.

---

## 9. Logging

Plugins that call `guest.LogMsg(...)` produce one line per call on the
host's standard output, prefixed with `[PLUGIN LOG]:`. This is
intentionally simple. If you want structured logging, redirect or
replace the implementation by editing `host/host_funcs.go` to write to
your `slog.Logger` of choice. The capability surface is small enough
that forking the function is a one-line change.

---

## 10. Going Beyond the Defaults

These are the most common production needs not covered by the
out-of-the-box SDK. Each is a small, well-scoped change.

### Caching Compiled Modules

`Execute` calls `runtime.CompileModule` on every invocation. Compilation
is expensive; instantiation is cheap. If you call the same plugin
repeatedly, cache the compiled module yourself:

```go
type CachedEngine struct {
    eng      *host.Engine
    runtime  wazero.Runtime
    compiled sync.Map // map[plugin name] wazero.CompiledModule
}
```

This requires a small fork of `host.Execute` that takes a
`wazero.CompiledModule` instead of `[]byte`. The wire protocol is
unchanged.

### Restricting `http_post`

The default `http_post` capability calls `net/http` with no allowlist —
plugins can reach any URL the host can. For untrusted plugins, fork
`host/host_funcs.go::hostHttpPost` and consult an allowlist or per-plugin
policy before issuing the request. Because policies are usually
plugin-specific, a clean pattern is to thread a `policy` parameter
through `NewEngine` and capture it in the closure when you register the
host module.

### Adding New Capabilities

The full list of plugin-visible host functions is the chain in
`host/engine.go::NewEngine`:

```go
r.NewHostModuleBuilder("podpedia_host").
    NewFunctionBuilder().WithFunc(hostLogMsg).Export("log_msg").
    NewFunctionBuilder().WithFunc(hostHttpPost).Export("http_post").
    Instantiate(ctx)
```

To add `file_read`, for example:

1. Append `.NewFunctionBuilder().WithFunc(hostFileRead).Export("file_read")`
   to that chain.
2. Implement `hostFileRead(ctx context.Context, mod api.Module, packed uint64) uint64`
   in `host/host_funcs.go`, decoding the request with `abi.Decode` and
   writing the response back via the existing `writeBack` helper.
3. Add a corresponding `//go:wasmimport podpedia_host file_read` and a
   clean wrapper to `guest/host_funcs.go` so plugins can call it as
   `guest.FileRead(...)`.

Match the `0`/`1` framing convention in the response so existing plugin
code continues to work the same way.

### Per-Call Timeouts

`Execute` honors the `context.Context` you pass it. A `context.WithTimeout`
will trap the plugin if it runs too long:

```go
callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()
out, err := engine.Execute(callCtx, wasmBytes, reqJSON)
```

A timeout is the only safe way to bound a misbehaving plugin's CPU
usage; do not omit this in production.

---

## 11. Checklist Before Shipping

- [ ] `host.Engine` is created once and shared across requests.
- [ ] `engine.Close(ctx)` runs on shutdown.
- [ ] Every `Execute` call is wrapped in a `context.WithTimeout`.
- [ ] Compiled modules are cached if the same plugin runs many times.
- [ ] `http_post` is either disabled or backed by a per-plugin allowlist
      if you load untrusted plugins.
- [ ] Plugin `[]byte` is loaded once and reused — not re-read per call.
- [ ] Errors from `Execute` are surfaced to the caller verbatim; the
      framing byte is already handled.

That is the complete integration surface. The SDK is small on purpose:
the parts you need to fork — capability policies, module caching,
plugin distribution — are exactly the parts that should be tailored to
your host.
