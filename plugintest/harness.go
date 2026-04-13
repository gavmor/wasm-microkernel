package plugintest

import (
	"context"
	"fmt"
	"testing"

	"github.com/gavmor/wasm-microkernel/abi"
	"github.com/gavmor/wasm-microkernel/host"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

type Harness struct {
	Runtime    wazero.Runtime
	Module     api.Module
	t          *testing.T
	mockedFuncs map[string]map[string]bool
}

// New creates an isolated Wasm environment for unit testing.
func New(t *testing.T) *Harness {
	ctx := context.Background()
	r := wazero.NewRuntime(ctx)

	// Must instantiate WASI for standard library functions (like panic or fmt.Println)
	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	return &Harness{
		Runtime:     r,
		t:           t,
		mockedFuncs: make(map[string]map[string]bool),
	}
}

// Load compiles and instantiates the given .wasm bytes.
// Before compilation it validates that every non-WASI import has a
// corresponding mock registered via MockHostFunction.
func (h *Harness) Load(ctx context.Context, wasmBytes []byte) error {
	if err := abi.ValidateABI(wasmBytes, nil, h.mockedFuncs); err != nil {
		return err
	}

	compiled, err := h.Runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return fmt.Errorf("failed to compile plugin: %w", err)
	}

	config := wazero.NewModuleConfig().
		WithName("").
		WithStartFunctions("_initialize")

	mod, err := h.Runtime.InstantiateModule(ctx, compiled, config)
	if err != nil {
		return fmt.Errorf("failed to instantiate plugin: %w", err)
	}
	h.Module = mod
	return nil
}

// NewHostModule starts the process of building a complex host module with multiple functions.
func (h *Harness) NewHostModule(name string) *host.ModuleBuilder {
	return host.NewModuleBuilder(h.Runtime, name)
}

// MockHostFunction is a convenience method for registering a single-function host module.
// It also records the function in the mockedFuncs map so that Load-time import
// validation knows which host capabilities are available.
func (h *Harness) MockHostFunction(module, name string, params, results []api.ValueType, fn api.GoModuleFunction) {
	if h.mockedFuncs[module] == nil {
		h.mockedFuncs[module] = make(map[string]bool)
	}
	h.mockedFuncs[module][name] = true

	err := host.RegisterCapability(h.Runtime, module, name, params, results, fn)
	if err != nil {
		panic(err)
	}
}

// MockedFuncs returns the map of mocked host functions keyed by module name,
// suitable for passing to abi.ValidateABI for standalone validation calls.
func (h *Harness) MockedFuncs() map[string]map[string]bool {
	return h.mockedFuncs
}

// CallExport invokes a generic export and handles fat-pointer unpacking.
func (h *Harness) CallExport(ctx context.Context, name string, params ...uint64) ([]byte, error) {
	fn := h.Module.ExportedFunction(name)
	if fn == nil {
		return nil, fmt.Errorf("export %q not found", name)
	}

	results, err := fn.Call(ctx, params...)
	if err != nil {
		return nil, fmt.Errorf("call to %q failed: %w", name, err)
	}

	if len(results) == 0 {
		return nil, nil
	}

	ptr, size := abi.DecodeFatPointer(results[0])
	return abi.ReadGuestBuffer(ctx, h.Module, ptr, size)
}

// Allocate allocates memory in the guest using its 'allocate' export.
func (h *Harness) Allocate(ctx context.Context, size uint32) (uint32, error) {
	fn := h.Module.ExportedFunction("allocate")
	if fn == nil {
		return 0, fmt.Errorf("allocate export not found")
	}

	results, err := fn.Call(ctx, uint64(size))
	if err != nil {
		return 0, fmt.Errorf("allocate call failed: %w", err)
	}

	if len(results) == 0 {
		return 0, fmt.Errorf("allocate returned no results")
	}

	return uint32(results[0]), nil
}

// Close releases all wazero resources.
func (h *Harness) Close() error {
	if h.Runtime != nil {
		return h.Runtime.Close(context.Background())
	}
	return nil
}
