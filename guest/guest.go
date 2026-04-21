//go:build wasip1

// Package guest is the plugin-side SDK. Plugins call Register from init()
// to install their handler, and the SDK exports the canonical "allocate"
// and "execute" symbols that the host engine invokes.
package guest

import (
        "unsafe"

        "github.com/gavmor/wasm-microkernel/abi"
)

// pluginHandler is the business-logic callback installed by the plugin.
var pluginHandler func(reqJSON string) (string, error)

// pinned holds the most recent allocation/result so Go's GC does not free
// memory the host still has a pointer into.
var pinned []byte

// Register installs the plugin's handler. Call this from init() so the
// handler is in place before the host invokes execute.
func Register(handler func(string) (string, error)) {
        pluginHandler = handler
}

//go:wasmexport allocate
func allocate(size uint32) uint32 {
        if size == 0 {
                // A zero-length allocation has no addressable byte; return 0.
                // The host treats reqJSON=="" as a no-op and never writes through this.
                pinned = nil
                return 0
        }
        pinned = make([]byte, size)
        return uint32(uintptr(unsafe.Pointer(&pinned[0])))
}

//go:wasmexport execute
func execute(ptr, length uint32) uint64 {
        var input []byte
        if length > 0 && ptr != 0 {
                input = unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)
        }

        if pluginHandler == nil {
                return frame(1, "plugin handler not registered: call guest.Register from init()")
        }

        res, err := pluginHandler(string(input))
        if err != nil {
                return frame(1, err.Error())
        }
        return frame(0, res)
}

// frame packs (flag, payload) into the SDK wire format and returns it as a
// pinned fat pointer. flag is 0 for success, 1 for error.
func frame(flag byte, payload string) uint64 {
        out := make([]byte, 1+len(payload))
        out[0] = flag
        copy(out[1:], payload)
        pinned = out
        return abi.Encode(uint32(uintptr(unsafe.Pointer(&pinned[0]))), uint32(len(pinned)))
}
