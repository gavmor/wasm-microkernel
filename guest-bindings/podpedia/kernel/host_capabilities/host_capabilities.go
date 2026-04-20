//go:build wasip1

// Package host_capabilities provides WIT-generated wrappers for the
// "podpedia:kernel/host-capabilities@0.3.0" interface. Functions return
// idiomatic (value, error) pairs; the underlying fat-pointer ABI is hidden.
package host_capabilities

import (
	"encoding/json"
	"fmt"

	"github.com/gavmor/wasm-microkernel/guest"
)

// HttpPost performs a generic HTTP POST and returns the response body.
// Returns an error if the host reported a failure.
func HttpPost(url, body string) (string, error) {
	return parseResponse(guest.HTTPPost(url, body))
}

// HttpFetch performs a generic HTTP GET and returns the response body.
func HttpFetch(url string) (string, error) {
	return parseResponse(guest.HTTPFetch(url))
}

// HttpDownload downloads url to dest on the host filesystem.
func HttpDownload(url, dest string) error {
	if !guest.HTTPDownload(url, dest) {
		return fmt.Errorf("download failed: %s → %s", url, dest)
	}
	return nil
}

// FileWrite writes data to path on the host filesystem.
func FileWrite(path, data string) error {
	if !guest.FileWrite(path, data) {
		return fmt.Errorf("file write failed: %s", path)
	}
	return nil
}

// LogMsg sends a fire-and-forget log message to the host.
func LogMsg(msg string) { guest.Log(msg) }

// parseResponse interprets a raw host response. If the host wrote an
// {"error":"..."} JSON object (via capabilities.errJSON), it returns
// a Go error. Otherwise it returns the raw bytes as a string.
func parseResponse(raw []byte) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var check struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &check); err == nil && check.Error != "" {
		return "", fmt.Errorf("%s", check.Error)
	}
	return string(raw), nil
}
