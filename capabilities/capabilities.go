// Package capabilities provides standard host function implementations
// (HTTP, file I/O, logging) that any wasm-microkernel host can register.
// These are the "syscalls" that plugins consume — analogous to OS userland
// interfaces — so the host application never touches Wasm memory directly.
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

// Config controls which capabilities are registered and how they behave.
type Config struct {
	// ModuleName is the Wasm import module name (e.g. "podpedia_host").
	// Plugins import functions from this name via //go:wasmimport.
	ModuleName string

	// LogWriter receives log messages from the log capability.
	// Defaults to os.Stderr.
	LogWriter io.Writer
}

// Register instantiates the standard capability module in r.
// Plugins can then import: http_post, fetch_url, http_download, file_write, log.
func Register(ctx context.Context, r wazero.Runtime, cfg Config) error {
	if cfg.ModuleName == "" {
		cfg.ModuleName = "wasm_host"
	}
	if cfg.LogWriter == nil {
		cfg.LogWriter = os.Stderr
	}

	one64 := []api.ValueType{api.ValueTypeI64}
	one32 := []api.ValueType{api.ValueTypeI32}
	b := host.NewModuleBuilder(r, cfg.ModuleName)

	// fetch_url(fat_ptr url) -> fat_ptr body  — generic HTTP GET
	b.ExportFunction("fetch_url", one64, one64,
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			off, ln := abi.DecodeFatPointer(stack[0])
			urlBytes, err := abi.ReadGuestBuffer(ctx, mod, off, ln)
			if err != nil {
				stack[0] = writeToGuest(ctx, mod, errJSON(err))
				return
			}
			body, err := httpGet(string(urlBytes))
			if err != nil {
				stack[0] = writeToGuest(ctx, mod, errJSON(err))
				return
			}
			stack[0] = writeToGuest(ctx, mod, body)
		}),
	)

	// http_post(fat_ptr {url,body,content_type?}) -> fat_ptr response-bytes
	// Generic HTTP POST — plugins use this to call Ollama, Deepgram, etc.
	b.ExportFunction("http_post", one64, one64,
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			off, ln := abi.DecodeFatPointer(stack[0])
			raw, _ := abi.ReadGuestBuffer(ctx, mod, off, ln)
			var req struct {
				URL         string `json:"url"`
				Body        string `json:"body"`
				ContentType string `json:"content_type"`
			}
			if err := json.Unmarshal(raw, &req); err != nil {
				stack[0] = writeToGuest(ctx, mod, errJSON(err))
				return
			}
			ct := req.ContentType
			if ct == "" {
				ct = "application/json"
			}
			resp, err := http.Post(req.URL, ct, strings.NewReader(req.Body)) //nolint:gosec
			if err != nil {
				stack[0] = writeToGuest(ctx, mod, errJSON(err))
				return
			}
			defer func() { _ = resp.Body.Close() }()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				stack[0] = writeToGuest(ctx, mod, errJSON(err))
				return
			}
			stack[0] = writeToGuest(ctx, mod, body)
		}),
	)

	// http_download(fat_ptr {url,dest}) -> i32 1=ok 0=err
	b.ExportFunction("http_download", one64, one32,
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			off, ln := abi.DecodeFatPointer(stack[0])
			raw, _ := abi.ReadGuestBuffer(ctx, mod, off, ln)
			var req struct {
				URL  string `json:"url"`
				Dest string `json:"dest"`
			}
			if err := json.Unmarshal(raw, &req); err != nil {
				stack[0] = 0
				return
			}
			if err := downloadFile(req.URL, req.Dest); err != nil {
				stack[0] = 0
				return
			}
			stack[0] = 1
		}),
	)

	// file_write(fat_ptr {path,data}) -> i32 1=ok 0=err
	// Lets plugins persist output without needing WASI dir mounts.
	b.ExportFunction("file_write", one64, one32,
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			off, ln := abi.DecodeFatPointer(stack[0])
			raw, _ := abi.ReadGuestBuffer(ctx, mod, off, ln)
			var req struct {
				Path string `json:"path"`
				Data string `json:"data"`
			}
			if err := json.Unmarshal(raw, &req); err != nil {
				stack[0] = 0
				return
			}
			if err := os.MkdirAll(filepath.Dir(req.Path), 0o755); err != nil {
				stack[0] = 0
				return
			}
			if err := os.WriteFile(req.Path, []byte(req.Data), 0o644); err != nil {
				stack[0] = 0
				return
			}
			stack[0] = 1
		}),
	)

	// log(fat_ptr message) — fire and forget, writes to LogWriter
	b.ExportFunction("log", one64, nil,
		api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			off, ln := abi.DecodeFatPointer(stack[0])
			msg, _ := abi.ReadGuestBuffer(ctx, mod, off, ln)
			_, _ = cfg.LogWriter.Write(append(msg, '\n'))
		}),
	)

	return b.Instantiate(ctx)
}

// ── internal helpers ──────────────────────────────────────────────────────────

// writeToGuest allocates memory in the guest module and writes data into it,
// returning a fat pointer. Used by host capability implementations.
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
