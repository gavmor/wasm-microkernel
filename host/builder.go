package host

import (
	"context"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// ModuleBuilder accumulates host functions for a single Wasm module.
type ModuleBuilder struct {
	builder wazero.HostModuleBuilder
}

// NewModuleBuilder starts a new host module definition.
func NewModuleBuilder(r wazero.Runtime, name string) *ModuleBuilder {
	return &ModuleBuilder{
		builder: r.NewHostModuleBuilder(name),
	}
}

// ExportFunction adds a function to the module.
func (mb *ModuleBuilder) ExportFunction(name string, params, results []api.ValueType, fn api.GoModuleFunction) *ModuleBuilder {
	mb.builder.NewFunctionBuilder().
		WithGoModuleFunction(fn, params, results).
		Export(name)
	return mb
}

// Instantiate registers the module in the runtime.
func (mb *ModuleBuilder) Instantiate(ctx context.Context) error {
	_, err := mb.builder.Instantiate(ctx)
	return err
}

// RegisterCapability is a shortcut for registering a single-function module.
func RegisterCapability(r wazero.Runtime, moduleName, funcName string, params, results []api.ValueType, fn api.GoModuleFunction) error {
	return NewModuleBuilder(r, moduleName).
		ExportFunction(funcName, params, results, fn).
		Instantiate(context.Background())
}
