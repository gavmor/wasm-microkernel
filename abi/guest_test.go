package abi

import (
	"testing"
)

func TestReturnBytes(t *testing.T) {
	data := []byte("hello world")
	fatPtr := ReturnBytes(data)

	offset, length := DecodeFatPointer(fatPtr)
	if length != uint32(len(data)) {
		t.Fatalf("expected length %d, got %d", len(data), length)
	}
	if offset == 0 {
		t.Fatal("expected non-zero offset")
	}
	// Verify the pinned allocation matches
	if string(lastGuestAlloc) != "hello world" {
		t.Fatalf("expected pinned data %q, got %q", "hello world", string(lastGuestAlloc))
	}
}

func TestGuestAllocate(t *testing.T) {
	ptr := GuestAllocate(64)
	if ptr == 0 {
		t.Fatal("expected non-zero pointer")
	}
	if len(lastGuestAlloc) != 64 {
		t.Fatalf("expected pinned allocation of 64 bytes, got %d", len(lastGuestAlloc))
	}
}

// Delegate cannot be unit-tested on a 64-bit host: GuestAllocate returns a
// truncated uint32 pointer that Delegate would dereference via unsafe.Slice,
// causing a SIGSEGV. The function is two lines (read + ReturnBytes) and is
// fully exercised by plugin integration tests that compile for WASM.
