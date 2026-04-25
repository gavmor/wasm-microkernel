//go:build wasip1

package guest

import (
	"fmt"
	"github.com/extism/go-pdk"
)

// Execute is the body of the WASM "execute" export. Plugins must re-export
// it from package main with //go:wasmexport so the Go toolchain generates a
// WASI reactor (_initialize) rather than a command (_start). Without that,
// the Go runtime exits after _start and subsequent extism calls to execute
// hit runtime.notInitialized.
func Execute() int32 {
	if pluginHandler == nil {
		pdk.SetError(fmt.Errorf("plugin handler not registered: call guest.Register from init()"))
		return 1
	}

	res, err := pluginHandler(string(pdk.Input()))
	if err != nil {
		pdk.SetError(err)
		return 1
	}

	pdk.OutputString(res)
	return 0
}

func logMsg(msg string) {
	pdk.Log(pdk.LogInfo, msg)
}

func httpPost(url, bodyJSON string) (string, error) {
	req := pdk.NewHTTPRequest(pdk.MethodPost, url)
	req.SetHeader("Content-Type", "application/json")
	req.SetBody([]byte(bodyJSON))

	resp := req.Send()
	if status := resp.Status(); status >= 400 {
		return "", fmt.Errorf("http_post: HTTP %d", status)
	}
	return string(resp.Body()), nil
}

func httpGet(url string) (string, error) {
	req := pdk.NewHTTPRequest(pdk.MethodGet, url)
	resp := req.Send()
	if status := resp.Status(); status >= 400 {
		return "", fmt.Errorf("http_get: HTTP %d", status)
	}
	return string(resp.Body()), nil
}

func getConfig(key string) (string, bool) {
	return pdk.GetConfig(key)
}

