package abi

import (
	"fmt"

	"github.com/eliben/watgo"
	"github.com/eliben/watgo/wasmir"
)

// ValidateABI checks that a Wasm binary satisfies the given contract:
//   - Every name in requiredExports must be present as a function export.
//   - Every import (except wasi_snapshot_preview1) must belong to a module
//     listed in availableHostFuncs, and the function name must be registered
//     in that module's set (a nil set means all functions are allowed).
func ValidateABI(wasmBytes []byte, requiredExports []string, availableHostFuncs map[string]map[string]bool) error {
	module, err := watgo.DecodeWASM(wasmBytes)
	if err != nil {
		return fmt.Errorf("failed to decode wasm binary: %w", err)
	}

	// Check required exports.
	exports := make(map[string]bool)
	for _, exp := range module.Exports {
		if exp.Kind == wasmir.ExternalKindFunction {
			exports[exp.Name] = true
		}
	}
	for _, name := range requiredExports {
		if !exports[name] {
			return fmt.Errorf("missing required export: %s", name)
		}
	}

	// Check imports against the declared host capabilities.
	for _, imp := range module.Imports {
		if imp.Kind != wasmir.ExternalKindFunction {
			continue
		}
		if imp.Module == "wasi_snapshot_preview1" {
			continue
		}
		funcs, ok := availableHostFuncs[imp.Module]
		if !ok {
			return fmt.Errorf("unsupported import module: %s", imp.Module)
		}
		if funcs != nil && !funcs[imp.Name] {
			return fmt.Errorf("unsupported import capability: %s.%s", imp.Module, imp.Name)
		}
	}

	return nil
}
