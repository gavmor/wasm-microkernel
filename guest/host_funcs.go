//go:build wasip1

package guest

import (
        "fmt"
        "unsafe"

        "github.com/gavmor/wasm-microkernel/abi"
)

// ── Raw host imports ─────────────────────────────────────────────────────────
// All host capabilities live under the "podpedia_host" module name.

//go:wasmimport podpedia_host log_msg
func hostLogMsg(packed uint64)

//go:wasmimport podpedia_host http_post
func hostHttpPost(packed uint64) uint64

// ── Capability wrappers for plugins ──────────────────────────────────────────

// LogMsg sends a fire-and-forget log line to the host.
func LogMsg(msg string) {
        if len(msg) == 0 {
                return
        }
        b := []byte(msg)
        ptr := uint32(uintptr(unsafe.Pointer(&b[0])))
        hostLogMsg(abi.Encode(ptr, uint32(len(b))))
}

// HttpPost asks the host to POST bodyJSON to url and returns the response
// body as a string. Errors reported by the host are returned as Go errors.
func HttpPost(url, bodyJSON string) (string, error) {
        // Wire format: "<url>\x00<body>"
        payload := []byte(url + "\x00" + bodyJSON)
        reqPtr := uint32(uintptr(unsafe.Pointer(&payload[0])))

        resPacked := hostHttpPost(abi.Encode(reqPtr, uint32(len(payload))))
        resPtr, resLen := abi.Decode(resPacked)
        // A zero-length response means the host could not allocate or write a
        // framed reply — treat it as a transport error rather than silent success.
        if resLen == 0 || resPtr == 0 {
                return "", fmt.Errorf("host http_post: empty response (host allocation failure)")
        }

        raw := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(resPtr))), resLen)
        if raw[0] == 1 {
                return "", fmt.Errorf("%s", string(raw[1:]))
        }
        return string(raw[1:]), nil
}
