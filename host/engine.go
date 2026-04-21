// Package host is the embed-side SDK. The host application creates one
// Engine, then calls Execute to run a plugin against a JSON request.
// All WebAssembly memory management lives here so the host application
// never sees pointers, lengths, or wazero APIs.
package host

import (
        "context"
        "fmt"

        "github.com/gavmor/wasm-microkernel/abi"
        "github.com/tetratelabs/wazero"
        "github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// Engine wraps a wazero runtime with the standard podpedia host capabilities
// pre-mounted. One Engine should be created per process and reused.
type Engine struct {
        runtime wazero.Runtime
}

// NewEngine boots a wazero runtime and registers the "podpedia_host" module
// containing every host capability the SDK exposes.
func NewEngine(ctx context.Context) (*Engine, error) {
        r := wazero.NewRuntime(ctx)

        // Plugins compiled with `GOOS=wasip1 GOARCH=wasm` link against
        // wasi_snapshot_preview1; without it, instantiation traps before
        // _initialize runs.
        if _, err := wasi_snapshot_preview1.Instantiate(ctx, r); err != nil {
                _ = r.Close(ctx)
                return nil, fmt.Errorf("instantiating WASI: %w", err)
        }

        if _, err := r.NewHostModuleBuilder("podpedia_host").
                NewFunctionBuilder().WithFunc(hostLogMsg).Export("log_msg").
                NewFunctionBuilder().WithFunc(hostHttpPost).Export("http_post").
                Instantiate(ctx); err != nil {
                _ = r.Close(ctx)
                return nil, fmt.Errorf("registering host capabilities: %w", err)
        }

        return &Engine{runtime: r}, nil
}

// Execute compiles wasmBytes, instantiates it, calls its "execute" export
// with reqJSON, and returns the plugin's response string. The plugin module
// is closed before Execute returns.
func (e *Engine) Execute(ctx context.Context, wasmBytes []byte, reqJSON string) (string, error) {
        compiled, err := e.runtime.CompileModule(ctx, wasmBytes)
        if err != nil {
                return "", fmt.Errorf("compiling plugin: %w", err)
        }
        defer func() { _ = compiled.Close(ctx) }()

        // Reactor builds (`-buildmode=c-shared` for wasip1) export _initialize
        // rather than _start. Calling it runs Go's package init() so any
        // guest.Register() the plugin issues from init() takes effect before
        // execute is invoked.
        cfg := wazero.NewModuleConfig().WithStartFunctions("_initialize")
        mod, err := e.runtime.InstantiateModule(ctx, compiled, cfg)
        if err != nil {
                return "", fmt.Errorf("instantiating plugin: %w", err)
        }
        defer func() { _ = mod.Close(ctx) }()

        allocateFn := mod.ExportedFunction("allocate")
        if allocateFn == nil {
                return "", fmt.Errorf("plugin missing 'allocate' export")
        }
        executeFn := mod.ExportedFunction("execute")
        if executeFn == nil {
                return "", fmt.Errorf("plugin missing 'execute' export")
        }

        reqSize := uint64(len(reqJSON))
        var reqPtr uint32
        if reqSize > 0 {
                results, err := allocateFn.Call(ctx, reqSize)
                if err != nil {
                        return "", fmt.Errorf("guest allocate: %w", err)
                }
                if len(results) == 0 {
                        return "", fmt.Errorf("guest allocate returned no value")
                }
                reqPtr = uint32(results[0])
                if !mod.Memory().Write(reqPtr, []byte(reqJSON)) {
                        return "", fmt.Errorf("writing request to guest memory")
                }
        }

        resPacked, err := executeFn.Call(ctx, uint64(reqPtr), reqSize)
        if err != nil {
                return "", fmt.Errorf("plugin execution trapped: %w", err)
        }
        if len(resPacked) == 0 {
                return "", fmt.Errorf("plugin execute returned no value")
        }

        resPtr, resLen := abi.Decode(resPacked[0])
        if resLen == 0 {
                return "", fmt.Errorf("plugin returned empty response")
        }
        resBytes, ok := mod.Memory().Read(resPtr, resLen)
        if !ok {
                return "", fmt.Errorf("reading result from guest memory")
        }
        if resBytes[0] == 1 {
                return "", fmt.Errorf("plugin logic error: %s", string(resBytes[1:]))
        }
        return string(resBytes[1:]), nil
}

// Close releases the wazero runtime and any compiled modules.
func (e *Engine) Close(ctx context.Context) error {
        return e.runtime.Close(ctx)
}
