//go:build wasip1

// Package guest is the plugin-side SDK. Plugins call Register from init()
// to install their handler; the SDK exports the canonical "execute" symbol
// that the host engine invokes.
//
// The SDK is a thin facade over github.com/extism/go-pdk: plugins never
// see Extism types or memory APIs, only plain Go strings and errors.
// Swapping the underlying ABI (Extism today, the Component Model tomorrow)
// requires no changes to plugin business logic.
package guest

import (
        "fmt"

        "github.com/extism/go-pdk"
)

// pluginHandler is the business-logic callback installed by the plugin.
var pluginHandler func(reqJSON string) (string, error)

// Register installs the plugin's handler. Call this from init() so the
// handler is in place before the host invokes execute.
func Register(handler func(string) (string, error)) {
        pluginHandler = handler
}

//go:wasmexport execute
func execute() int32 {
        if pluginHandler == nil {
                pdk.SetError(fmt.Errorf("plugin handler not registered: call guest.Register from init()"))
                return 1
        }

        // Extism reads the host-supplied input and gives us the bytes;
        // no manual fat-pointer decode required.
        res, err := pluginHandler(string(pdk.Input()))
        if err != nil {
                pdk.SetError(err)
                return 1
        }

        pdk.OutputString(res)
        return 0
}

// LogMsg sends a fire-and-forget log line to the host.
func LogMsg(msg string) {
        if len(msg) == 0 {
                return
        }
        pdk.Log(pdk.LogInfo, msg)
}

// HttpPost asks the host to POST bodyJSON to url and returns the response
// body as a string. The host enforces an allow-list (Engine.AllowedHosts)
// so plugins can only reach pre-approved destinations.
//
// Error contract:
//   - HTTP status >= 400: returned as an error; the plugin can recover
//     and choose its own response.
//   - Disallowed host or transport failure: Extism aborts the whole
//     execute call. The plugin does NOT receive control back; the host
//     sees this as a "plugin error" from Engine.Execute. Plugins that
//     need to soft-fail on policy denials must ensure the host has
//     allow-listed every URL they may attempt.
func HttpPost(url, bodyJSON string) (string, error) {
        req := pdk.NewHTTPRequest(pdk.MethodPost, url)
        req.SetHeader("Content-Type", "application/json")
        req.SetBody([]byte(bodyJSON))

        resp := req.Send()
        if status := resp.Status(); status >= 400 {
                return "", fmt.Errorf("http_post: HTTP %d", status)
        }
        return string(resp.Body()), nil
}
