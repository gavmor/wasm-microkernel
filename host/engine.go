// Package host is the embed-side SDK. The host application creates one
// Engine, then calls Execute to run a plugin against a JSON request.
// All WebAssembly memory management lives behind Extism so the host
// application never sees pointers, lengths, or wazero APIs.
package host

import (
	"context"
	"fmt"
	"os"

	extism "github.com/extism/go-sdk"
)

// Engine runs WebAssembly plugins through Extism (which uses wazero
// internally). One Engine should be created per process and reused.
//
// AllowedHosts is the glob pattern list passed straight to Extism's
// Manifest. An empty list means plugins cannot make any outbound HTTP
// calls. Use []string{"*"} to allow everything.
type Engine struct {
	AllowedHosts []string
	AllowedPaths map[string]string
}

// NewEngine returns a fresh Engine. Tweak AllowedHosts on the returned
// value before calling Execute if your plugins need outbound HTTP.
func NewEngine() *Engine {
	return &Engine{}
}

// Execute compiles wasmBytes through Extism, calls its "execute" export
// with reqJSON, and returns the plugin's response string. The plugin
// instance is closed before Execute returns.
//
// Note on HttpPost behavior: if a plugin calls guest.HttpPost against a
// host that is not in AllowedHosts (or the transport fails), Extism
// terminates the whole execute call. That surfaces here as a non-nil
// error wrapped with the "plugin error:" prefix — the plugin does NOT
// observe a clean error return from guest.HttpPost in that case.
func (e *Engine) Execute(ctx context.Context, wasmBytes []byte, reqJSON string) (string, error) {
	// Snapshot AllowedHosts so concurrent mutation by the host application
	// cannot race with the manifest the plugin instance is built against.
	allowed := append([]string(nil), e.AllowedHosts...)

	manifest := extism.Manifest{
		Wasm: []extism.Wasm{
			extism.WasmData{Data: wasmBytes},
		},
		AllowedHosts: allowed,
		AllowedPaths: e.AllowedPaths,
		Memory: &extism.ManifestMemory{
			MaxHttpResponseBytes: 1024 * 1024 * 1024, // 1GB
		},
	}

	plugin, err := extism.NewPlugin(ctx, manifest, extism.PluginConfig{
		EnableWasi: true,
	}, []extism.HostFunction{})
	if err != nil {
		return "", fmt.Errorf("initializing plugin: %w", err)
	}
	plugin.SetLogger(func(level extism.LogLevel, msg string) {
		fmt.Fprintf(os.Stderr, "[plugin] %s\n", msg)
	})

	// Cleanup must run on a fresh, non-cancelable context so that a
	// canceled or timed-out caller context does not skip plugin teardown
	// and leak Extism/wazero resources.
	defer plugin.Close(context.Background())

	exitCode, out, err := plugin.CallWithContext(ctx, "execute", []byte(reqJSON))
	if err != nil {
		// Extism surfaces both pdk.SetError messages and runtime traps here.
		return "", fmt.Errorf("plugin error: %w", err)
	}
	if exitCode != 0 {
		return "", fmt.Errorf("plugin exited with code %d", exitCode)
	}
	return string(out), nil
}

// Close is retained for API compatibility; Extism plugins are closed
// per-call inside Execute, so there is no engine-wide resource to release.
func (e *Engine) Close(ctx context.Context) error {
	return nil
}
