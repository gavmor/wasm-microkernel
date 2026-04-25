//go:build !wasip1

package guest

import (
	"fmt"
)

// Execute is a no-op stub for host-side compilation (unit tests).
// The real implementation lives in guest_wasip1.go.
func Execute() int32 { return 0 }

func logMsg(msg string) {
	// No-op on host
}

func httpPost(url, bodyJSON string) (string, error) {
	return "", fmt.Errorf("HttpPost capability not available when running in native Go environment (unit tests)")
}

func httpGet(url string) (string, error) {
	return "", fmt.Errorf("HttpGet capability not available when running in native Go environment (unit tests)")
}

func getConfig(key string) (string, bool) {
	return "", false
}

