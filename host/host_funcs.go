package host

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/gavmor/wasm-microkernel/abi"
	"github.com/tetratelabs/wazero/api"
)

// hostLogMsg implements the "log_msg" capability. Fire-and-forget.
func hostLogMsg(ctx context.Context, mod api.Module, packed uint64) {
	offset, length := abi.Decode(packed)
	b, _ := mod.Memory().Read(offset, length)
	fmt.Printf("[PLUGIN LOG]: %s\n", string(b))
}

// hostHttpPost implements the "http_post" capability. The request payload is
// "<url>\x00<body>"; the response is "<flag><payload>" where flag is 0 (ok)
// or 1 (err), allocated in guest memory and returned as a fat pointer.
func hostHttpPost(ctx context.Context, mod api.Module, packed uint64) uint64 {
	offset, length := abi.Decode(packed)
	input, ok := mod.Memory().Read(offset, length)
	if !ok {
		return writeBack(ctx, mod, []byte{1})
	}

	parts := bytes.SplitN(input, []byte{0}, 2)
	if len(parts) != 2 {
		return writeBack(ctx, mod, append([]byte{1}, []byte("malformed http_post payload")...))
	}
	url, body := string(parts[0]), parts[1]

	var resultData []byte
	resp, err := http.Post(url, "application/json", bytes.NewReader(body)) //nolint:gosec
	if err != nil {
		resultData = append([]byte{1}, []byte(err.Error())...)
	} else {
		defer func() { _ = resp.Body.Close() }()
		b, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			resultData = append([]byte{1}, []byte(readErr.Error())...)
		} else {
			resultData = append([]byte{0}, b...)
		}
	}

	return writeBack(ctx, mod, resultData)
}

// writeBack allocates space in the guest, writes data into it, and returns
// the resulting fat pointer. Returns 0 if the guest cannot satisfy the
// allocation — the plugin SDK treats a zero-length response as "no data".
func writeBack(ctx context.Context, mod api.Module, data []byte) uint64 {
	allocFn := mod.ExportedFunction("allocate")
	if allocFn == nil {
		return 0
	}
	res, err := allocFn.Call(ctx, uint64(len(data)))
	if err != nil || len(res) == 0 {
		return 0
	}
	resPtr := uint32(res[0])
	if !mod.Memory().Write(resPtr, data) {
		return 0
	}
	return abi.Encode(resPtr, uint32(len(data)))
}
