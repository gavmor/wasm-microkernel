# Integration Testing `wasm-microkernel`

The standard Go way to integration test a WebAssembly harness is the
**testdata pattern** combined with a dedicated *Ping* (or *Echo*)
plugin. You do not want to use real production plugins for this — they
carry too much baggage (real services, real credentials, network
dependencies). Instead, write a tiny deterministic Wasm plugin whose
only job is to exercise the boundaries of the SDK.

This guide is the playbook for setting up that test suite, both inside
this repository and inside any host application that embeds it.

---

## 1. Create the Test Plugin

In the package whose tests you want to write (here, `host/`), create a
`testdata/ping/` directory and write a minimal plugin that:

1. Echoes its input back wrapped in a deterministic envelope.
2. Triggers a host capability (log, HTTP) so the import side is covered.
3. Returns an error on a known sentinel input so the error path is
   covered too.

**`host/testdata/ping/main.go`**

```go
//go:build wasip1

package main

import (
    "fmt"
    "strings"

    "github.com/gavmor/wasm-microkernel/guest"
)

func init() {
    guest.Register(func(reqJSON string) (string, error) {
        switch {
        case reqJSON == "crash":
            return "", fmt.Errorf("simulated plugin error")
        case reqJSON == "":
            return `{"status":"pong","echo":null}`, nil
        case strings.HasPrefix(reqJSON, "POST "):
            // Format: "POST <url> <body>" — exercises guest.HttpPost.
            rest := strings.TrimPrefix(reqJSON, "POST ")
            sp := strings.IndexByte(rest, ' ')
            url, body := rest[:sp], rest[sp+1:]
            return guest.HttpPost(url, body)
        }

        guest.LogMsg("ping received: " + reqJSON)
        return `{"status":"pong","echo":` + reqJSON + `}`, nil
    })
}

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
fly. Compile the plugin once with a `//go:generate` directive sitting
next to the test file, and check the resulting `.wasm` into the repo.

**`host/engine_test.go`** (top of file)

```go
package host_test

//go:generate env GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o testdata/ping.wasm ./testdata/ping
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

These are ordinary `go test` cases. They prove that the engine boots,
the plugin's `init()` runs, the request crosses the boundary intact,
the response is unwrapped correctly, and a plugin-side error is
surfaced as a Go `error`.

```go
func TestEngine_Execute_Success(t *testing.T) {
    wasmBytes, err := os.ReadFile("testdata/ping.wasm")
    if err != nil {
        t.Fatalf("read test plugin: %v", err)
    }

    eng := host.NewEngine()
    defer eng.Close(context.Background())

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    got, err := eng.Execute(ctx, wasmBytes, `"hello host"`)
    if err != nil {
        t.Fatalf("execute: %v", err)
    }

    want := `{"status":"pong","echo":"hello host"}`
    if got != want {
        t.Errorf("want %q, got %q", want, got)
    }
}

func TestEngine_Execute_PluginError(t *testing.T) {
    wasmBytes, err := os.ReadFile("testdata/ping.wasm")
    if err != nil {
        t.Fatalf("read test plugin: %v", err)
    }

    eng := host.NewEngine()
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    _, err = eng.Execute(ctx, wasmBytes, "crash")
    if err == nil {
        t.Fatal("expected an error from plugin, got none")
    }

    // Extism wraps pdk.SetError messages; assert on the inner text only.
    if !strings.Contains(err.Error(), "simulated plugin error") {
        t.Errorf("unexpected error: %v", err)
    }
}
```

Two things worth noticing:

- `host.NewEngine()` takes no arguments and returns no error. It is
  free; you can call it per test.
- Always wrap `Execute` in a `context.WithTimeout` — Extism boots wazero
  internally, which can in pathological cases hang on a malformed module.

---

## 4. Useful Edge Cases to Cover

Once the happy path passes, add cases that pin down the contract at
its corners:

| Case                                        | What it proves                                    |
| ------------------------------------------- | ------------------------------------------------- |
| Empty request (`engine.Execute(ctx, w, "")`)| Empty input crosses the boundary cleanly.         |
| Very large request (e.g. 256 KiB JSON)      | Extism handles non-trivial buffers.               |
| Concurrent calls to one engine              | Per-call instance isolation works.                |
| Re-running the same plugin many times       | No leak in instantiate/close.                     |
| `context.WithTimeout` shorter than work     | Plugin cancellation is contained.                 |
| `HttpPost` against an `httptest.Server`     | AllowedHosts gating + HTTP capability work.       |

Each is a 10-line test against the same `ping.wasm` — the plugin only
needs a couple of new branches.

---

## 5. Testing `guest.HttpPost`

Unlike most plugin frameworks, you do **not** need to mock the HTTP
client to test capability-driven plugins. Extism's HTTP capability is
already host-implemented (it uses Go's `net/http`), so an
`httptest.Server` works out of the box — you just have to add its host
to `Engine.AllowedHosts`:

```go
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    body, _ := io.ReadAll(r.Body)
    w.Write([]byte(`{"echo":` + string(body) + `}`))
}))
defer srv.Close()

eng := host.NewEngine()
eng.AllowedHosts = []string{"127.0.0.1"}  // httptest binds to 127.0.0.1

ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

// ping plugin parses "POST <url> <body>" and calls guest.HttpPost.
payload := "POST " + srv.URL + "/echo " + `{"hello":"world"}`
got, err := eng.Execute(ctx, wasmBytes, payload)
```

This is more honest than mocking the client: it exercises the real
allow-list gate, the real HTTP serialization, and the real response
read-back, end to end.

---

## 6. CI Considerations

- **Commit the `.wasm` artifact.** This avoids requiring `wasip1` /
  `wasm` toolchains in CI just to run host tests.
- **Re-run `go generate` in a separate CI job** if you want to detect
  drift between the committed `.wasm` and the test plugin source. Make
  it fail if `git diff --exit-code host/testdata/` reports changes.
- **Run host tests with the race detector** (`go test -race ./host/...`).
  Extism is safe for concurrent use; the race detector catches misuse
  of the engine in your own code.
- **Fail loudly on missing artifacts.** If `testdata/ping.wasm` is
  absent, prefer `t.Fatal` over `t.Skip` — a missing artifact in your
  own repo means `go generate` was not run.

---

## 7. Why This Pattern Works

- **Determinism.** A purpose-built `ping` plugin has no external
  dependencies, no clocks, no randomness — every assertion is exact.
- **Isolation.** Bugs in production plugins cannot break your harness
  tests, and vice versa.
- **Speed.** Tests do not shell out to compile anything; they read a
  committed `.wasm` and run Extism in-process.
- **Coverage of the boundary.** The plugin exercises both directions
  (host → guest via `execute`; guest → host via `LogMsg` and
  `HttpPost`), so a regression in either path fails loudly.

That is the entire integration-testing surface. Once `ping.wasm` is in
place, every new SDK feature gets a one-branch addition to the plugin
plus a one-function test on the host side.
