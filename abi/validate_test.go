package abi_test

import (
	"strings"
	"testing"

	"github.com/eliben/watgo"
	"github.com/gavmor/wasm-microkernel/abi"
)

func compile(t *testing.T, wat string) []byte {
	t.Helper()
	wasm, err := watgo.CompileWATToWASM([]byte(wat))
	if err != nil {
		t.Fatalf("failed to compile WAT: %v", err)
	}
	return wasm
}

func TestValidateABI_MissingRequiredExport(t *testing.T) {
	wasm := compile(t, `(module
		(func (export "present") (result i32) (i32.const 0))
	)`)

	err := abi.ValidateABI(wasm, []string{"present", "missing"}, nil)
	if err == nil {
		t.Fatal("expected error for missing export, got nil")
	}
	if !strings.Contains(err.Error(), "missing required export: missing") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateABI_ImportFromUnknownModule(t *testing.T) {
	wasm := compile(t, `(module
		(import "unknown_mod" "some_func" (func))
	)`)

	host := map[string]map[string]bool{
		"known_mod": {"some_func": true},
	}
	err := abi.ValidateABI(wasm, nil, host)
	if err == nil {
		t.Fatal("expected error for unknown import module, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported import module: unknown_mod") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateABI_UnregisteredCapability(t *testing.T) {
	wasm := compile(t, `(module
		(import "axe_kernel" "play_chime" (func))
	)`)

	host := map[string]map[string]bool{
		"axe_kernel": {"track_artifact": true},
	}
	err := abi.ValidateABI(wasm, nil, host)
	if err == nil {
		t.Fatal("expected error for unregistered capability, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported import capability: axe_kernel.play_chime") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateABI_ValidModule(t *testing.T) {
	wasm := compile(t, `(module
		(import "axe_kernel" "track_artifact" (func))
		(func (export "Execute") (result i32) (i32.const 0))
	)`)

	host := map[string]map[string]bool{
		"axe_kernel": {"track_artifact": true},
	}
	err := abi.ValidateABI(wasm, []string{"Execute"}, host)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}
