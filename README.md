# `wasm-microkernel` Plugin Development Guide

Welcome to the `wasm-microkernel` ecosystem! This generic Go SDK utilizes a WebAssembly (Wasm) microkernel architecture to securely load and execute third-party extensions for any host application.

Because `wasm-microkernel` executes plugins within a strictly isolated Wasm sandbox, your code cannot crash the host process or access unauthorized system resources. To provide maximum flexibility and the best developer experience, this framework enforces a **Tri-Module Architecture**:

1. **Host Application:** The core application embedding the microkernel (you don't need to import this).
    
2. **Domain Contract:** The shared contract containing data structures (DTOs) and interface definitions defined by the host application. This decoupled approach is heavily inspired by the architectural patterns of the Model Context Protocol (MCP).
    
3. **`wasm-microkernel`:** This lightweight SDK providing memory translation, host function builders, and a local testing harness.
    

## Prerequisites

- **Go 1.26:** Go 1.26 is strictly recommended. For WebAssembly applications, the Go 1.26 runtime manages chunks of heap memory in much smaller increments, which leads to significantly reduced memory usage for lightweight applications with heaps under 16 MiB.
    

## Step 1: Project Setup

Initialize your Go module and fetch the required dependencies. You **do not** need to clone or import the core host application's repository, only its shared protocol and this SDK.
```bash
go mod init github.com/your-org/my-custom-plugin
go get github.com/your-org/host-protocol
go get github.com/your-org/wasm-microkernel
```

## Step 2: Implementing the Contract (WASI Reactor)

Plugins built with this SDK are compiled as **WASI Reactors**. Unlike standard command modules that run a `main()` function and exit, a reactor initializes its state once and remains continuously alive in memory, allowing its exported functions to be called multiple times.

To expose your function to the Wasm host, use the `//go:wasmexport` compiler directive. Because Wasm operates in a 32-bit address space, you must use the `wasm-microkernel` SDK's ABI helpers to pass complex types (like JSON or structs) across the boundary using "fat pointers" (a 64-bit integer combining the memory address and the length).

```go
package main

import (
	"encoding/json"
	"unsafe"
	
	"github.com/your-org/host-protocol"
	"github.com/your-org/wasm-microkernel/abi"
)

// main is required by the compiler but skipped in reactor mode.
func main() {}

//go:wasmexport Execute
func Execute(payloadOffset, payloadLength uint32) uint32 {
    // 1. Read the payload from the host using the SDK
    rawBytes := abi.ReadHostBuffer(payloadOffset, payloadLength)
    
    // 2. Parse the host's protocol DTO
    var call protocol.HostRequest
    json.Unmarshal(rawBytes, &call)
    
    //... Perform your custom business logic...
    
    return 0 // 0 indicates success to the microkernel
}
```

## Step 3: Using Host Capabilities

If your plugin needs to interact with the host (e.g., to emit telemetry or save a file), you must import a host capability. The host safely exposes these capabilities via Wasm imports.

```go
// Import the event publisher from the host application
//go:wasmimport host_env publish_event
func publish_event_host(offset uint32, length uint32)

// EmitEvent is a helper utilizing the SDK to safely pass data back to the host
func EmitEvent(event protocol.EventDTO) {
	bytes, _ := json.Marshal(event)
	
	// The SDK handles packing the pointer and length for the Wasm ABI
	offset, length := abi.EncodeFatPointer(bytes)
	publish_event_host(offset, length)
}
```

## Step 4: Local Integration Testing

You can fully test your Wasm plugin without running the actual host application. The `wasm-microkernel` SDK provides a `plugintest` harness powered by `wazero`.

Create a `main_test.go` file:

```go
package main

import (
    "testing"
    "github.com/your-org/wasm-microkernel/plugintest"
)

func TestPluginExecution(t *testing.T) {
    // 1. Initialize the SDK's isolated test harness
    harness := plugintest.NewHarness(t, "plugin.wasm")
    
    // 2. Mock the host capabilities your plugin requires
    harness.MockHostFunction("host_env", "publish_event", func(offset, length uint32) {
        // Intercept the event for assertions
    })
    
    // 3. Invoke your exported Wasm function
    exitCode := harness.CallExport("Execute", mockPayloadBytes)
    if exitCode!= 0 {
        t.Fatalf("Plugin execution failed")
    }
}
```

## Step 5: Compilation and Distribution

To compile your plugin, you must target the `wasip1` OS and use the `c-shared` build mode. This instructs the Go linker to skip generating a standard `_start` function and instead generate the special `_initialize` function required for the WASI Reactor pattern.

```bash
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o my-plugin.wasm
```

**Distribution via OCI Registry:**

The SDK supports dynamically fetching plugins from OCI-compliant container registries. This is the recommended approach as it provides built-in versioning and access control.

Use the `oras` CLI to push your compiled Wasm binary:

```bash 
oras push ghcr.io/your-org/my-custom-plugin:v1.0.0 \
  my-plugin.wasm:application/vnd.module.wasm.content.layer.v1+wasm
```

End users can now configure the host application to load your plugin simply by providing the OCI URL.
