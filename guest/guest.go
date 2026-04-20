//go:build wasip1

// Package guest provides WIT-compatible host capability bindings for plugins.
//
// Generated from wit/podpedia.wit via wit-bindgen-go, then extended with
// correct u64/u32 return types (pending wasip2 result-lifting support).
//
// Plugins import this package instead of writing //go:wasmimport declarations
// by hand. Input strings use the canonical WIT ABI (cm.LowerString → i32+i32).
// Output strings are returned as fat pointers (ptr<<32|len) and decoded here,
// so callers never see unsafe.
package guest

import (
	"unsafe"

	"github.com/bytecodealliance/wasm-tools-go/cm"
)

// WITModule is the canonical module name plugins import from.
// The host must register capabilities under this name.
const WITModule = "podpedia:kernel/host-capabilities@0.3.0"

// ── Capability wrappers ───────────────────────────────────────────────────────

// HTTPPost calls the host's generic HTTP POST and returns the response body.
func HTTPPost(url, body string) []byte {
	url0, url1 := cm.LowerString(url)
	body0, body1 := cm.LowerString(body)
	return readFatPtr(hostHTTPPost((*uint8)(url0), url1, (*uint8)(body0), body1))
}

// HTTPFetch calls the host's generic HTTP GET and returns the response body.
func HTTPFetch(url string) []byte {
	url0, url1 := cm.LowerString(url)
	return readFatPtr(hostHTTPFetch((*uint8)(url0), url1))
}

// HTTPDownload asks the host to download url to dest. Returns true on success.
func HTTPDownload(url, dest string) bool {
	url0, url1 := cm.LowerString(url)
	dest0, dest1 := cm.LowerString(dest)
	return hostHTTPDownload((*uint8)(url0), url1, (*uint8)(dest0), dest1) == 1
}

// FileWrite asks the host to write data to path. Returns true on success.
func FileWrite(path, data string) bool {
	path0, path1 := cm.LowerString(path)
	data0, data1 := cm.LowerString(data)
	return hostFileWrite((*uint8)(path0), path1, (*uint8)(data0), data1) == 1
}

// Log sends a fire-and-forget log message to the host.
func Log(msg string) {
	msg0, msg1 := cm.LowerString(msg)
	hostLogMsg((*uint8)(msg0), msg1)
}

// ReturnBytes encodes a response byte slice as a fat pointer for export from Execute.
// Plugins call this as their final return: return guest.ReturnBytes(out).
func ReturnBytes(b []byte) uint64 {
	if len(b) == 0 {
		return 0
	}
	// Allocate space in guest's own linear memory for the return value.
	ptr := allocate(uint32(len(b)))
	dst := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), len(b))
	copy(dst, b)
	return (uint64(ptr) << 32) | uint64(len(b))
}

// ── Raw host imports (canonical WIT ABI) ─────────────────────────────────────
// Inputs: strings as (ptr *uint8, len uint32) — canonical i32+i32.
// Outputs: fat pointer (u64) for strings, u32 for status. Return types are
// hand-written because wit-bindgen-go v0.3.2 drops them for wasip1 targets.

//go:wasmimport podpedia:kernel/host-capabilities@0.3.0 http-post
//go:noescape
func hostHTTPPost(url0 *uint8, url1 uint32, body0 *uint8, body1 uint32) uint64

//go:wasmimport podpedia:kernel/host-capabilities@0.3.0 http-fetch
//go:noescape
func hostHTTPFetch(url0 *uint8, url1 uint32) uint64

//go:wasmimport podpedia:kernel/host-capabilities@0.3.0 http-download
//go:noescape
func hostHTTPDownload(url0 *uint8, url1 uint32, dest0 *uint8, dest1 uint32) uint32

//go:wasmimport podpedia:kernel/host-capabilities@0.3.0 file-write
//go:noescape
func hostFileWrite(path0 *uint8, path1 uint32, data0 *uint8, data1 uint32) uint32

//go:wasmimport podpedia:kernel/host-capabilities@0.3.0 log-msg
//go:noescape
func hostLogMsg(msg0 *uint8, msg1 uint32)

// ── Guest memory helpers ──────────────────────────────────────────────────────

// allocate is the guest-side bump allocator, exported for the host to call
// when writing response data into guest memory.
//
//go:wasmexport allocate
func allocate(size uint32) uint32 {
	b := make([]byte, size)
	return uint32(uintptr(unsafe.Pointer(&b[0])))
}

// readFatPtr decodes a fat pointer (ptr<<32|len) from the host into a byte slice.
// The unsafe here is the minimum necessary in wasip1: the host wrote the bytes
// into our linear memory at ptr; we read them back by address.
func readFatPtr(fatPtr uint64) []byte {
	ptr := uint32(fatPtr >> 32)
	size := uint32(fatPtr)
	if ptr == 0 || size == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), size)
}
