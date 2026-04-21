// Package abi defines the shared fat-pointer encoding used by the
// wasm-microkernel host and guest to pass (offset, length) pairs across
// the WebAssembly boundary as a single i64.
package abi

// Encode packs a memory offset and length into a single 64-bit integer.
func Encode(ptr, length uint32) uint64 {
	return (uint64(ptr) << 32) | uint64(length)
}

// Decode unpacks a 64-bit integer into a memory offset and length.
func Decode(packed uint64) (ptr uint32, length uint32) {
	ptr = uint32(packed >> 32)
	length = uint32(packed)
	return
}
