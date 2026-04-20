// Package capabilities provides standard host function implementations
// (HTTP, file I/O, logging) registered under the canonical WIT module name.
//
// The ABI matches wit/podpedia.wit:
//   - Input strings: canonical i32+i32 (ptr, len) — matches cm.LowerString output
//   - Output strings: fat pointer u64 (ptr<<32|len) written into guest memory
//   - Status returns: u32 (1=ok, 0=err)
//
// The host reads strings via wazero's safe mod.Memory().Read(), so no unsafe
// is required on the host side. The only unsafe in the system is in guest/guest.go
// where the plugin reads back host-allocated fat pointers.
package capabilities

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gavmor/wasm-microkernel/abi"
	"github.com/gavmor/wasm-microkernel/host"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// WITModule is the canonical capability module name from wit/podpedia.wit.
// Plugins import functions from this module via //go:wasmimport.
const WITModule = "podpedia:kernel/host-capabilities@0.3.0"

// Config controls which capabilities are registered and how they behave.
type Config struct {
	// ModuleName overrides WITModule if set. Use only for legacy plugin compat.
	ModuleName string

	// LogWriter receives log messages from the log-msg capability.
	// Defaults to os.Stderr.
	LogWriter io.Writer
}

func (c Config) moduleName() string {
	if c.ModuleName != "" {
		return c.ModuleName
	}
	return WITModule
}

// Register instantiates the standard capability module in r under the WIT
// module name. Plugins compiled against guest/guest.go will import from here.
func Register(ctx context.Context, r wazero.Runtime, cfg Config) error {
	if cfg.LogWriter == nil {
		cfg.LogWriter = os.Stderr
	}

	two32one64 := []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}           // (ptr, len) string in
	four32one64 := []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32} // two strings in
	retU64 := []api.ValueType{api.ValueTypeI64}
	retU32 := []api.ValueType{api.ValueTypeI32}

	b := host.NewModuleBuilder(r, cfg.moduleName())

	// http-fetch(url-ptr, url-len) -> u64 fat-ptr  — generic HTTP GET
	b.ExportFunction("http-fetch", two32one64, retU64,
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			url := readString(mod, uint32(stack[0]), uint32(stack[1]))
			body, err := httpGet(url)
			if err != nil {
				stack[0] = writeToGuest(ctx, mod, errJSON(err))
				return
			}
			stack[0] = writeToGuest(ctx, mod, body)
		}),
	)

	// http-post(url-ptr, url-len, body-ptr, body-len) -> u64 fat-ptr
	b.ExportFunction("http-post", four32one64, retU64,
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			url := readString(mod, uint32(stack[0]), uint32(stack[1]))
			body := readString(mod, uint32(stack[2]), uint32(stack[3]))
			resp, err := http.Post(url, "application/json", strings.NewReader(body)) //nolint:gosec
			if err != nil {
				stack[0] = writeToGuest(ctx, mod, errJSON(err))
				return
			}
			defer func() { _ = resp.Body.Close() }()
			respBody, err := io.ReadAll(resp.Body)
			if err != nil {
				stack[0] = writeToGuest(ctx, mod, errJSON(err))
				return
			}
			stack[0] = writeToGuest(ctx, mod, respBody)
		}),
	)

	// http-download(url-ptr, url-len, dest-ptr, dest-len) -> u32 1=ok 0=err
	b.ExportFunction("http-download", four32one64, retU32,
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			url := readString(mod, uint32(stack[0]), uint32(stack[1]))
			dest := readString(mod, uint32(stack[2]), uint32(stack[3]))
			if err := downloadFile(url, dest); err != nil {
				stack[0] = 0
				return
			}
			stack[0] = 1
		}),
	)

	// file-write(path-ptr, path-len, data-ptr, data-len) -> u32 1=ok 0=err
	b.ExportFunction("file-write", four32one64, retU32,
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			path := readString(mod, uint32(stack[0]), uint32(stack[1]))
			data := readString(mod, uint32(stack[2]), uint32(stack[3]))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				stack[0] = 0
				return
			}
			if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
				stack[0] = 0
				return
			}
			stack[0] = 1
		}),
	)

	// log-msg(msg-ptr, msg-len) — fire and forget
	b.ExportFunction("log-msg", two32one64, nil,
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			msg := readString(mod, uint32(stack[0]), uint32(stack[1]))
			_, _ = cfg.LogWriter.Write([]byte(msg + "\n"))
		}),
	)

	return b.Instantiate(ctx)
}

// ── internal helpers ──────────────────────────────────────────────────────────

// readString reads a (ptr, len) string from guest linear memory via wazero's
// safe Memory().Read(). No unsafe required on the host side.
func readString(mod api.Module, ptr, length uint32) string {
	b, _ := mod.Memory().Read(ptr, length)
	return string(b)
}

// writeToGuest allocates memory in the guest (via its "allocate" export) and
// writes data into it, returning a fat pointer. This is how the host returns
// heap-allocated strings to plugins in wasip1.
func writeToGuest(ctx context.Context, mod api.Module, data []byte) uint64 {
	alloc := mod.ExportedFunction("allocate")
	if alloc == nil {
		return 0
	}
	res, err := alloc.Call(ctx, uint64(len(data)))
	if err != nil || len(res) == 0 {
		return 0
	}
	offset := uint32(res[0])
	mod.Memory().Write(offset, data)
	return abi.EncodeFatPointer(offset, uint32(len(data)))
}

func errJSON(err error) []byte {
	b, _ := json.Marshal(map[string]string{"error": err.Error()})
	return b
}

func httpGet(url string) ([]byte, error) {
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}

func downloadFile(url, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(f, resp.Body)
	return err
}
