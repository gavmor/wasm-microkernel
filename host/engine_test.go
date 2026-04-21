package host_test

//go:generate env GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o testdata/ping.wasm ./testdata/ping

import (
        "context"
        "io"
        "net/http"
        "net/http/httptest"
        "os"
        "strconv"
        "strings"
        "sync"
        "testing"
        "time"

        "github.com/gavmor/wasm-microkernel/host"
)

// callTimeout bounds every Execute call so a regression cannot hang the
// whole suite. wazero compilation of the 2.6 MB ping plugin takes ~0.8 s
// on a typical laptop, so 30 s is generous headroom.
const callTimeout = 30 * time.Second

// loadPing reads the pre-compiled test plugin. Failing here (rather than
// skipping) is deliberate: this repo's own CI is the source of truth for
// the artifact, so a missing ping.wasm means `go generate ./host/...` was
// not run before commit.
func loadPing(t *testing.T) []byte {
        t.Helper()
        wasmBytes, err := os.ReadFile("testdata/ping.wasm")
        if err != nil {
                t.Fatalf("testdata/ping.wasm missing; run `go generate ./host/...`: %v", err)
        }
        return wasmBytes
}

// newEngine returns a fresh Engine. By default no outbound HTTP is
// allowed; tests that need it set AllowedHosts on the returned value.
func newEngine(t *testing.T) *host.Engine {
        t.Helper()
        eng := host.NewEngine()
        t.Cleanup(func() { _ = eng.Close(context.Background()) })
        return eng
}

// callCtx returns a fresh per-call context with the standard timeout.
func callCtx(t *testing.T) (context.Context, context.CancelFunc) {
        t.Helper()
        return context.WithTimeout(context.Background(), callTimeout)
}

func TestEngine_Execute_Success(t *testing.T) {
        wasmBytes := loadPing(t)
        eng := newEngine(t)
        ctx, cancel := callCtx(t)
        defer cancel()

        const payload = `"hello host"`
        got, err := eng.Execute(ctx, wasmBytes, payload)
        if err != nil {
                t.Fatalf("execute: %v", err)
        }

        const want = `{"status":"pong","echo":"hello host"}`
        if got != want {
                t.Errorf("want %q, got %q", want, got)
        }
}

func TestEngine_Execute_PluginError(t *testing.T) {
        wasmBytes := loadPing(t)
        eng := newEngine(t)
        ctx, cancel := callCtx(t)
        defer cancel()

        _, err := eng.Execute(ctx, wasmBytes, "crash")
        if err == nil {
                t.Fatal("expected an error from plugin, got none")
        }

        // Extism wraps the pdk.SetError message inside its own
        // "guest call failed" envelope; assert on the inner text only.
        const want = "simulated plugin error"
        if !strings.Contains(err.Error(), want) {
                t.Errorf("want error containing %q, got %q", want, err.Error())
        }
}

// TestEngine_Execute_EmptyRequest passes an empty request through to the
// plugin, confirming pdk.Input() returns an empty byte slice (not nil-
// pointer panic) and the guest's empty-string branch fires cleanly.
func TestEngine_Execute_EmptyRequest(t *testing.T) {
        wasmBytes := loadPing(t)
        eng := newEngine(t)
        ctx, cancel := callCtx(t)
        defer cancel()

        got, err := eng.Execute(ctx, wasmBytes, "")
        if err != nil {
                t.Fatalf("execute: %v", err)
        }

        const want = `{"status":"pong","echo":null}`
        if got != want {
                t.Errorf("want %q, got %q", want, got)
        }
}

func TestEngine_Execute_LargePayload(t *testing.T) {
        wasmBytes := loadPing(t)
        eng := newEngine(t)
        ctx, cancel := callCtx(t)
        defer cancel()

        // 256 KiB JSON string ("AAAA…") to exercise the fat-pointer path with
        // a non-trivial buffer size.
        payload := `"` + strings.Repeat("A", 256*1024) + `"`
        got, err := eng.Execute(ctx, wasmBytes, payload)
        if err != nil {
                t.Fatalf("execute: %v", err)
        }

        want := `{"status":"pong","echo":` + payload + `}`
        if got != want {
                t.Errorf("response did not echo large payload: got len=%d, want len=%d", len(got), len(want))
        }
}

// TestEngine_Execute_HttpPost validates that guest.HttpPost reaches an
// allow-listed host through Extism's built-in HTTP capability. The plugin
// parses "POST <url> <body>" and calls guest.HttpPost; the server echoes
// the request body back into a JSON envelope which the plugin returns
// verbatim. This also exercises Engine.AllowedHosts as the security gate.
func TestEngine_Execute_HttpPost(t *testing.T) {
        wasmBytes := loadPing(t)
        eng := newEngine(t)
        // httptest.Server binds 127.0.0.1 on a random port; allow that host.
        eng.AllowedHosts = []string{"127.0.0.1"}

        var (
                mu          sync.Mutex
                gotMethod   string
                gotPath     string
                gotBody     []byte
                gotContentType string
        )
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                body, _ := io.ReadAll(r.Body)
                mu.Lock()
                gotMethod = r.Method
                gotPath = r.URL.Path
                gotBody = body
                gotContentType = r.Header.Get("Content-Type")
                mu.Unlock()
                _, _ = w.Write([]byte(`{"echo":"` + string(body) + `"}`))
        }))
        defer srv.Close()

        ctx, cancel := callCtx(t)
        defer cancel()

        const reqBody = `{"hello":"world"}`
        payload := "POST " + srv.URL + "/echo " + reqBody

        got, err := eng.Execute(ctx, wasmBytes, payload)
        if err != nil {
                t.Fatalf("execute: %v", err)
        }

        want := `{"echo":"` + reqBody + `"}`
        if got != want {
                t.Errorf("response: want %q, got %q", want, got)
        }

        mu.Lock()
        defer mu.Unlock()
        if gotMethod != http.MethodPost {
                t.Errorf("server saw method %q, want POST", gotMethod)
        }
        if gotPath != "/echo" {
                t.Errorf("server saw path %q, want /echo", gotPath)
        }
        if string(gotBody) != reqBody {
                t.Errorf("server saw body %q, want %q", string(gotBody), reqBody)
        }
        if gotContentType != "application/json" {
                t.Errorf("server saw content-type %q, want application/json", gotContentType)
        }
}

// TestEngine_Execute_HttpPost_Denied proves that AllowedHosts is a real
// security gate: when the target host is not allow-listed, Extism aborts
// the plugin call and Execute surfaces it as an error. This is the SDK's
// SSRF defense — losing it would silently expose every plugin's network
// surface to the host's outbound connectivity.
func TestEngine_Execute_HttpPost_Denied(t *testing.T) {
        wasmBytes := loadPing(t)
        eng := newEngine(t)
        // Deliberately leave AllowedHosts empty (deny-all).

        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                _, _ = w.Write([]byte(`should-never-be-reached`))
        }))
        defer srv.Close()

        ctx, cancel := callCtx(t)
        defer cancel()

        payload := "POST " + srv.URL + "/echo {}"
        _, err := eng.Execute(ctx, wasmBytes, payload)
        if err == nil {
                t.Fatal("expected denied-host call to fail, got nil error")
        }
}

// TestEngine_Execute_RepeatedCalls proves that successive Execute calls do
// not leak module instances or corrupt later results. Kept small because
// every call recompiles the plugin from scratch (~0.8s each on a typical
// laptop); run with `-count=N` for soak testing.
func TestEngine_Execute_RepeatedCalls(t *testing.T) {
        if testing.Short() {
                t.Skip("skipping repeated-call test in -short mode")
        }
        wasmBytes := loadPing(t)
        eng := newEngine(t)

        for i := 0; i < 5; i++ {
                ctx, cancel := callCtx(t)
                got, err := eng.Execute(ctx, wasmBytes, `"x"`)
                cancel()
                if err != nil {
                        t.Fatalf("call %d: %v", i, err)
                }
                if got != `{"status":"pong","echo":"x"}` {
                        t.Fatalf("call %d: unexpected response %q", i, got)
                }
        }
}

// TestEngine_Execute_Concurrent proves that concurrent Execute calls on a
// shared engine remain isolated (each call gets its own module instance
// and its own slice of guest linear memory). Goroutine count is
// deliberately small: each call fully recompiles the plugin via wazero's
// optimizing backend, so high concurrency stresses the test runner more
// than it stresses the SDK contract under test.
func TestEngine_Execute_Concurrent(t *testing.T) {
        if testing.Short() {
                t.Skip("skipping concurrent test in -short mode")
        }
        wasmBytes := loadPing(t)
        eng := newEngine(t)

        const goroutines = 3

        var wg sync.WaitGroup
        errs := make(chan error, goroutines)

        for g := 0; g < goroutines; g++ {
                wg.Add(1)
                go func(g int) {
                        defer wg.Done()
                        payload := `"goroutine-` + strconv.Itoa(g) + `"`
                        want := `{"status":"pong","echo":` + payload + `}`
                        ctx, cancel := callCtx(t)
                        defer cancel()
                        got, err := eng.Execute(ctx, wasmBytes, payload)
                        if err != nil {
                                errs <- err
                                return
                        }
                        if got != want {
                                errs <- &mismatchError{want: want, got: got}
                        }
                }(g)
        }

        wg.Wait()
        close(errs)
        for err := range errs {
                t.Errorf("concurrent call failed: %v", err)
        }
}

type mismatchError struct{ want, got string }

func (e *mismatchError) Error() string {
        return "unexpected response: want " + e.want + ", got " + e.got
}
