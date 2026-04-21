# Integration Testing `wasm-microkernel`

The standard Go way to integration test a WebAssembly harness is the
**testdata pattern** combined with a dedicated *Ping* (or *Echo*)
plugin. You do not want to use real production plugins for this — they
carry too much baggage (Ollama instances, audio files, network access,
real credentials). Instead, write a tiny deterministic Wasm plugin
whose only job is to exercise the boundaries of the SDK and prove that
allocation, parameter passing, and host capabilities are wired up
correctly.

This guide is the playbook for setting up that test suite, both inside
this repository and inside any host application that embeds it.

---

## 1. Create the Test Plugin

In the package whose tests you want to write (here, `host/`), create a
`testdata/ping/` directory and write a minimal plugin that:

1. Echoes its input back wrapped in a deterministic envelope.
2. Triggers a host capability so you can prove the import side works.
3. Returns an error on a known sentinel input so you can test the error
   path too.

**`host/testdata/ping/main.go`**

```go
//go:build wasip1

package main

import (
    "fmt"

    "github.com/gavmor/wasm-microkernel/guest"
)

func init() {
    guest.Register(func(reqJSON string) (string, error) {
        if reqJSON == "crash" {
            return "", fmt.Errorf("simulated plugin error")
        }

        // Exercise the host log capability so the import side is covered.
        guest.LogMsg("ping received: " + reqJSON)

        // Deterministic, easy-to-assert response.
        return `{"status":"pong","echo":` + reqJSON + `}`, nil
    })
}

// Required by the Go toolchain; never executed in reactor mode.
func main() {}
```

> **Why `init()` and not `main()`?** Plugins are built as WASI reactors
> with `-buildmode=c-shared`. Reactors never run `main`. Registering
> the handler from `init()` guarantees it is in place before the host
> calls `execute`.

The `//go:build wasip1` constraint keeps `go test ./...` on your dev
machine from trying to compile the plugin source as a host package.

---

## 2. Pre-compile the Plugin via `go:generate`

You generally do not want `go test` shelling out to compile Wasm on the
fly: CI environments may lack the right toolchain, and it slows the
test loop. Compile the plugin once with a `//go:generate` directive
sitting next to the test file, and check the resulting `.wasm` into
the repository.

**`host/engine_test.go`** (top of file)

```go
package host_test

//go:generate env GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o testdata/ping.wasm ./testdata/ping

import (
    "context"
    "os"
    "testing"

    "github.com/gavmor/wasm-microkernel/host"
)
```

Run it once when the plugin source changes:

```bash
go generate ./host/...
git add host/testdata/ping.wasm
```

Add a `.gitattributes` entry so the binary is treated as such by git:

```
host/testdata/*.wasm binary
```

---

## 3. Write the Integration Tests

These are ordinary `go test` cases. They prove that:

- the engine boots,
- the plugin is loaded and `_initialize` runs (so `init()` registers
  the handler),
- the request crosses the boundary intact,
- the response is unframed correctly,
- a plugin-side error is surfaced as a Go `error`.

```go
func TestEngine_Execute_Success(t *testing.T) {
    ctx := context.Background()

    wasmBytes, err := os.ReadFile("testdata/ping.wasm")
    if err != nil {
        t.Fatalf("read test plugin: %v", err)
    }

    engine, err := host.NewEngine(ctx)
    if err != nil {
        t.Fatalf("new engine: %v", err)
    }
    defer engine.Close(ctx)

    payload := `"hello host"`
    got, err := engine.Execute(ctx, wasmBytes, payload)
    if err != nil {
        t.Fatalf("execute: %v", err)
    }

    want := `{"status":"pong","echo":"hello host"}`
    if got != want {
        t.Errorf("want %q, got %q", want, got)
    }
}

func TestEngine_Execute_PluginError(t *testing.T) {
    ctx := context.Background()

    wasmBytes, err := os.ReadFile("testdata/ping.wasm")
    if err != nil {
        t.Fatalf("read test plugin: %v", err)
    }

    engine, err := host.NewEngine(ctx)
    if err != nil {
        t.Fatalf("new engine: %v", err)
    }
    defer engine.Close(ctx)

    _, err = engine.Execute(ctx, wasmBytes, "crash")
    if err == nil {
        t.Fatal("expected an error from plugin, got none")
    }

    const want = "plugin logic error: simulated plugin error"
    if err.Error() != want {
        t.Errorf("want %q, got %q", want, err.Error())
    }
}
```

Two things worth noticing:

- `host.NewEngine` returns `(*Engine, error)` — always check both.
  Reuse the engine across sub-tests when you can; `Execute` is safe
  to call concurrently (see the integration guide, §5).
- `defer engine.Close(ctx)` matters even in tests, so the wazero
  runtime's background goroutines are released between cases.

---

## 4. Useful Edge Cases to Cover

Once the happy path passes, add cases that pin down the contract at
its corners:

| Case                                        | What it proves                           |
| ------------------------------------------- | ---------------------------------------- |
| Empty request (`engine.Execute(ctx, w, "")`)| `allocate(0)` is well-defined.           |
| Very large request (e.g. 1 MiB JSON)        | The fat-pointer path handles big buffers.|
| Concurrent calls to one engine              | Per-call module isolation works.         |
| `context.WithTimeout` shorter than a sleep  | Plugin trapping on cancel is contained.  |
| Re-running the same plugin many times       | No leak in module instantiate/close.     |

Each is a 10-line test against the same `ping.wasm` — the plugin only
needs a couple of new branches (e.g. a `"sleep"` input that calls
`time.Sleep`).

---

## 5. Mocking Host Capabilities

Eventually you will want to test plugins that call `guest.HttpPost`
without hitting the network. The SDK does not expose a hook for this
out of the box, but the change to add one is small and entirely on
the host side.

The general shape:

1. Promote the HTTP client used by `host/host_funcs.go::hostHttpPost`
   to a field on `Engine` (today it implicitly uses
   `http.DefaultClient`).
2. Add a constructor — e.g. `NewEngineWithClient(ctx, *http.Client)` —
   that sets that field.
3. Have `hostHttpPost` read the client from the captured `Engine`
   instead of calling `http.Post` directly.

Sketch:

```go
type Engine struct {
    runtime    wazero.Runtime
    httpClient *http.Client
}

func NewEngineWithClient(ctx context.Context, client *http.Client) (*Engine, error) {
    // Same setup as NewEngine, but stash `client` on the struct and
    // capture `e` in the closure when registering the http_post import:
    //
    //   .NewFunctionBuilder().
    //       WithFunc(func(ctx context.Context, mod api.Module, packed uint64) uint64 {
    //           return e.hostHttpPost(ctx, mod, packed)
    //       }).
    //       Export("http_post").
}
```

Then in tests:

```go
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    body, _ := io.ReadAll(r.Body)
    w.Write([]byte(`{"echo":` + string(body) + `}`))
}))
defer srv.Close()

engine, err := host.NewEngineWithClient(ctx, srv.Client())
```

The plugin's `guest.HttpPost("http://anything/...", body)` call still
flows through the host boundary, but the host now resolves it against
your `httptest.Server` instead of the real internet. Note that your
plugin will need to be told to call `srv.URL` (passed in via the
request payload), since `httptest` issues a fresh URL per test.

If you need finer-grained control — per-call assertions, per-URL
allowlists, recording fixtures — replace `*http.Client` with an
interface like `http.RoundTripper` and inject a fake of your own.

---

## 6. CI Considerations

- **Commit the `.wasm` artifact.** This avoids requiring `wasip1` /
  `wasm` toolchains in CI just to run host tests. The Go toolchain
  built into your CI image is typically enough on its own.
- **Re-run `go generate` in a separate CI job** if you want to detect
  drift between the committed `.wasm` and the test plugin source. Make
  it fail if `git diff --exit-code host/testdata/` reports changes.
- **Run host tests with the race detector** (`go test -race ./host/...`).
  The wazero runtime is safe for concurrent use; the race detector
  catches misuse of the engine in your own code.
- **Skip Wasm tests gracefully** if a downstream consumer's CI cannot
  run them, e.g. `if _, err := os.Stat("testdata/ping.wasm"); err != nil { t.Skip(...) }`.

---

## 7. Why This Pattern Works

- **Determinism.** A purpose-built `ping` plugin has no external
  dependencies, no clocks, no randomness — every assertion is exact.
- **Isolation.** Bugs in production plugins (extractor, transcriber)
  cannot break your harness tests, and vice versa.
- **Speed.** Tests do not shell out to compile anything; they read a
  committed `.wasm` and run wazero in-process.
- **Coverage of the boundary.** The plugin exercises both directions
  (host → guest via `execute`; guest → host via `LogMsg`), so a
  regression in either path fails loudly.

That is the entire integration-testing surface. Once `ping.wasm` is in
place, every new SDK feature gets a one-branch addition to the plugin
plus a one-function test on the host side.
