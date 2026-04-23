package host

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	extism "github.com/extism/go-sdk"
)

// Config defines the security policy and capabilities for the Kernel.
type Config struct {
	AllowedHosts []string
	AllowedPaths map[string]string
}

// PluginInstance represents a pre-compiled, ready-to-run Wasm plugin.
type PluginInstance struct {
	Manifest extism.Manifest
}

// Kernel provides a managed runtime for microkernel plugins. It handles
// plugin loading, configuration injection, and resource management.
type Kernel struct {
	baseManifest extism.Manifest
	plugins      map[string]*PluginInstance
	mu           sync.RWMutex
}

// NewKernel initializes a Kernel with the provided configuration.
func NewKernel(config Config) *Kernel {
	return &Kernel{
		baseManifest: extism.Manifest{
			AllowedHosts: config.AllowedHosts,
			AllowedPaths: config.AllowedPaths,
			Memory: &extism.ManifestMemory{
				MaxHttpResponseBytes: 1024 * 1024 * 1024, // 1GB
			},
		},
		plugins: make(map[string]*PluginInstance),
	}
}

// Load stores a WASM plugin by name for later execution.
// Note: Extism caches the Wasm compilation under the hood when the same 
// Wasm data/hash is used in the manifest.
func (k *Kernel) Load(name string, wasmBytes []byte) {
	k.mu.Lock()
	defer k.mu.Unlock()

	// Clone the base manifest and attach the Wasm data
	manifest := k.baseManifest
	manifest.Wasm = append([]extism.Wasm(nil), k.baseManifest.Wasm...)
	manifest.Wasm = append(manifest.Wasm, extism.WasmData{Data: wasmBytes})

	k.plugins[name] = &PluginInstance{
		Manifest: manifest,
	}
}

// Call executes the named plugin, marshaling 'input' to JSON and 
// passing 'config' to the plugin's environment.
func (k *Kernel) Call(ctx context.Context, pluginName string, input any, config map[string]string) ([]byte, error) {
	k.mu.RLock()
	instance, ok := k.plugins[pluginName]
	k.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("plugin not loaded: %s", pluginName)
	}

	reqJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// 1. Inject the runtime configuration
	manifest := instance.Manifest
	manifest.Config = config

	// 2. Instantiate the plugin
	plugin, err := extism.NewPlugin(ctx, manifest, extism.PluginConfig{
		EnableWasi: true,
	}, []extism.HostFunction{})
	if err != nil {
		return nil, fmt.Errorf("failed to init plugin: %w", err)
	}
	defer plugin.Close(context.Background())

	// 3. Execute
	exit, out, err := plugin.CallWithContext(ctx, "execute", reqJSON)
	if err != nil {
		return nil, fmt.Errorf("plugin error: %w", err)
	}
	if exit != 0 {
		return nil, fmt.Errorf("plugin exited with code %d", exit)
	}

	return out, nil
}

// Close is a no-op as individual plugins are closed after Call.
func (k *Kernel) Close() error {
	return nil
}
