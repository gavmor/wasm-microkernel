// Package plugin_world provides the WIT-generated interface for the
// "podpedia:kernel/plugin-world" world. Plugins implement PluginWorldExports
// and register via SetExportsPluginWorld; the framework handles the WASM
// export glue and memory management.
package plugin_world

// Result is the WIT native result type. Plugins return this from Execute
// instead of encoding errors as JSON — the host reads Ok/Err without parsing
// the business payload.
type Result[T, E any] struct {
	ok  *T
	err *E
}

// Ok constructs a successful Result.
func Ok[T, E any](v T) Result[T, E] { return Result[T, E]{ok: &v} }

// Err constructs a failed Result.
func Err[T, E any](e E) Result[T, E] { return Result[T, E]{err: &e} }

func (r Result[T, E]) IsOk() bool  { return r.ok != nil }
func (r Result[T, E]) IsErr() bool  { return r.err != nil }
func (r Result[T, E]) Unwrap() T    { return *r.ok }
func (r Result[T, E]) UnwrapErr() E { return *r.err }

// PluginWorldExports is the interface every plugin must satisfy.
type PluginWorldExports interface {
	// Execute receives a JSON request string and returns a JSON response
	// or an error string. Business types live inside the JSON; WIT owns
	// the boundary and the error channel.
	Execute(reqJSON string) (Result[string, string], error)
}

// Exports holds the plugin's registered implementation.
var Exports PluginWorldExports

// SetExportsPluginWorld registers the plugin implementation. Call from init().
func SetExportsPluginWorld(impl PluginWorldExports) { Exports = impl }
