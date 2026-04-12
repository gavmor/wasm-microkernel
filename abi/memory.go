package abi

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero/api"
)

// ReadGuestBuffer safely extracts a byte slice from the Wasm guest's linear memory.
func ReadGuestBuffer(ctx context.Context, m api.Module, offset, length uint32) ([]byte, error) {
	if length == 0 {
		return []byte{}, nil
	}
	bytes, ok := m.Memory().Read(offset, length)
	if !ok {
		return nil, fmt.Errorf("memory read out of bounds: offset %d, length %d", offset, length)
	}
	// Return a copy to prevent the guest from mutating the slice underneath the host
	result := make([]byte, length)
	copy(result, bytes)
	return result, nil
}

// WriteGuestBuffer writes a byte slice into the guest's linear memory.
func WriteGuestBuffer(ctx context.Context, m api.Module, offset uint32, data []byte) bool {
	return m.Memory().Write(offset, data)
}

// EncodeFatPointer packs a 32-bit memory address and a 32-bit length into a single 64-bit integer.
func EncodeFatPointer(offset, length uint32) uint64 {
	return (uint64(offset) << 32) | uint64(length)
}

// DecodeFatPointer unpacks a 64-bit integer into its constituent 32-bit offset and length.
func DecodeFatPointer(ptr uint64) (uint32, uint32) {
	return uint32(ptr >> 32), uint32(ptr)
}
