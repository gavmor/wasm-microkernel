//go:build wasip1

package plugin_world

import (
	"encoding/json"

	"github.com/gavmor/wasm-microkernel/abi"
)

// wireResult is the on-wire encoding of Result[string, string].
// The host reads {"ok":"..."} for success or {"err":"..."} for failure,
// eliminating the need for plugins to format error JSON themselves.
type wireResult struct {
	Ok  *string `json:"ok,omitempty"`
	Err *string `json:"err,omitempty"`
}

// allocate is exported so the host can write response data into guest
// memory before returning fat pointers. Plugins do not call this directly.
//
//go:wasmexport allocate
func allocate(size uint32) uint32 { return abi.GuestAllocate(size) }

// Execute is the single entry point for all plugins. It lifts the raw
// fat-pointer input into a Go string, calls the registered implementation,
// and lowers the Result back to a fat pointer for the host.
//
//go:wasmexport Execute
func Execute(offset, length uint32) uint64 {
	return abi.Delegate(offset, length, func(input []byte) []byte {
		if Exports == nil {
			return encode(wireResult{Err: ptr("plugin not initialized: call SetExportsPluginWorld from init()")})
		}
		result, err := Exports.Execute(string(input))
		switch {
		case err != nil:
			e := err.Error()
			return encode(wireResult{Err: &e})
		case result.IsErr():
			e := result.UnwrapErr()
			return encode(wireResult{Err: &e})
		default:
			v := result.Unwrap()
			return encode(wireResult{Ok: &v})
		}
	})
}

func encode(r wireResult) []byte { b, _ := json.Marshal(r); return b }
func ptr(s string) *string       { return &s }
