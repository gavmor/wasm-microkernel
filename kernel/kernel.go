// Package kernel provides a high-level Kernel type that the host application
// uses to load and invoke plugins. It owns the wazero runtime, WASI setup,
// and standard capability registration — the host app only provides config.
package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/gavmor/wasm-microkernel/abi"
	"github.com/gavmor/wasm-microkernel/capabilities"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// Config is the only thing the host application needs to provide.
type Config struct {
	// ModuleName overrides the canonical WIT module name for capability registration.
	// Defaults to capabilities.WITModule ("podpedia:kernel/host-capabilities@0.3.0").
	// Set this only for legacy plugin compatibility.
	ModuleName string

	// LogWriter receives plugin log messages. Defaults to os.Stderr.
	LogWriter io.Writer
}

// Kernel manages the wazero runtime and all loaded plugins.
// The host application creates one Kernel, loads plugins into it,
// then calls Execute to invoke them — no Wasm ABI details required.
type Kernel struct {
	ctx     context.Context
	runtime wazero.Runtime
	plugins map[string]*plugin
}

type plugin struct {
	mu     sync.Mutex
	module api.Module
}

// New boots the wazero runtime, instantiates WASI, and registers all
// standard host capabilities. The host application calls this once at startup.
func New(ctx context.Context, cfg Config) (*Kernel, error) {
	if cfg.LogWriter == nil {
		cfg.LogWriter = os.Stderr
	}

	r := wazero.NewRuntime(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	if err := capabilities.Register(ctx, r, capabilities.Config{
		ModuleName: cfg.ModuleName,
		LogWriter:  cfg.LogWriter,
	}); err != nil {
		_ = r.Close(ctx)
		return nil, fmt.Errorf("registering capabilities: %w", err)
	}

	return &Kernel{ctx: ctx, runtime: r, plugins: make(map[string]*plugin)}, nil
}

// Load compiles and instantiates a WASM plugin from disk.
func (k *Kernel) Load(name, path string) error {
	wasmBytes, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading plugin %s: %w", name, err)
	}
	compiled, err := k.runtime.CompileModule(k.ctx, wasmBytes)
	if err != nil {
		return fmt.Errorf("compiling plugin %s: %w", name, err)
	}
	cfg := wazero.NewModuleConfig().
		WithName(name).
		WithStartFunctions("_initialize")
	mod, err := k.runtime.InstantiateModule(k.ctx, compiled, cfg)
	if err != nil {
		return fmt.Errorf("instantiating plugin %s: %w", name, err)
	}
	k.plugins[name] = &plugin{module: mod}
	return nil
}

// Call invokes a plugin's Execute export with JSON-encoded input,
// returning JSON-encoded output. This is the only method the host
// application needs for plugin dispatch.
func (k *Kernel) Call(name string, input any) ([]byte, error) {
	p, ok := k.plugins[name]
	if !ok {
		return nil, fmt.Errorf("plugin %q not loaded", name)
	}

	// WASM Go runtime is single-threaded; serialize concurrent callers per plugin.
	p.mu.Lock()
	defer p.mu.Unlock()

	payload, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshalling input: %w", err)
	}

	alloc := p.module.ExportedFunction("allocate")
	if alloc == nil {
		return nil, fmt.Errorf("plugin %q missing allocate export", name)
	}
	res, err := alloc.Call(k.ctx, uint64(len(payload)))
	if err != nil {
		return nil, fmt.Errorf("guest allocate: %w", err)
	}
	offset := uint32(res[0])

	if !p.module.Memory().Write(offset, payload) {
		return nil, fmt.Errorf("writing payload to guest memory")
	}

	execute := p.module.ExportedFunction("Execute")
	if execute == nil {
		return nil, fmt.Errorf("plugin %q missing Execute export", name)
	}
	out, err := execute.Call(k.ctx, uint64(offset), uint64(len(payload)))
	if err != nil {
		return nil, fmt.Errorf("execute: %w", err)
	}

	ptr, size := abi.DecodeFatPointer(out[0])
	return abi.ReadGuestBuffer(k.ctx, p.module, ptr, size)
}

// Close shuts down the wazero runtime.
func (k *Kernel) Close() { _ = k.runtime.Close(k.ctx) }
