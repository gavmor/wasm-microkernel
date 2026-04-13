package abi

import "unsafe"

// lastGuestAlloc pins the most recent guest allocation to prevent GC.
var lastGuestAlloc []byte

// GuestHandler processes raw request bytes and returns raw response bytes.
type GuestHandler func([]byte) []byte

// Delegate reads input from guest memory at offset/length, passes it to
// handler, and returns the result as a GC-pinned fat pointer. Use this
// for exports that take input and return output (e.g. Execute).
func Delegate(offset, length uint32, handler GuestHandler) uint64 {
	input := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(offset))), length)
	return ReturnBytes(handler(input))
}

// ReturnBytes pins a byte slice to prevent GC and returns it as a fat
// pointer. Use this for exports that return data without taking input
// (e.g. Metadata).
func ReturnBytes(data []byte) uint64 {
	lastGuestAlloc = data
	return EncodeFatPointer(
		uint32(uintptr(unsafe.Pointer(&lastGuestAlloc[0]))),
		uint32(len(lastGuestAlloc)),
	)
}

// GuestAllocate allocates a GC-pinned byte slice and returns a pointer
// to it. The host writes into this pointer before calling Execute.
func GuestAllocate(size uint32) uint32 {
	lastGuestAlloc = make([]byte, size)
	return uint32(uintptr(unsafe.Pointer(&lastGuestAlloc[0])))
}
