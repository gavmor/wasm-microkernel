// Package guest is the plugin-side SDK. Plugins call Register from init()
// to install their handler; the SDK exports the canonical "execute" symbol
// that the host engine invokes.
//
// The SDK is a thin facade over github.com/extism/go-pdk: plugins never
// see Extism types or memory APIs, only plain Go strings and errors.
// Swapping the underlying ABI (Extism today, the Component Model tomorrow)
// requires no changes to plugin business logic.
package guest

// pluginHandler is the business-logic callback installed by the plugin.
var pluginHandler func(reqJSON string) (string, error)

// Register installs the plugin's handler. Call this from init() so the
// handler is in place before the host invokes execute.
func Register(handler func(string) (string, error)) {
	pluginHandler = handler
}

// LogMsg sends a fire-and-forget log line to the host.
func LogMsg(msg string) {
	if len(msg) == 0 {
		return
	}
	logMsg(msg)
}

// HttpPost asks the host to POST bodyJSON to url and returns the response
// body as a string. The host enforces an allow-list (Engine.AllowedHosts)
// so plugins can only reach pre-approved destinations.
func HttpPost(url, bodyJSON string) (string, error) {
	return httpPost(url, bodyJSON)
}
