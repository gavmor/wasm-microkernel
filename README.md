# Wasm Microkernel Go SDK

A domain-agnostic framework for building and testing extensible Go applications using WebAssembly.

## Tri-Module Architecture

This SDK is designed to be the foundation of a decoupled architecture:

1.  **Generic Engine (This SDK)**: Manages raw memory (fat pointers), Wasm execution (WASI Reactors), and testing harnesses. It knows nothing about your business logic.
2.  **Domain Contract (e.g., `axe-protocol`)**: A separate, lightweight repository containing your specific Go structs and interfaces.
3.  **Consumers**: Both your host application and its plugins import both the SDK and your domain contract.

## Key Packages

- `abi`: Memory translation bridge for reading/writing guest buffers and fat-pointer management.
- `host`: Utilities for capability injection and host module construction.
- `plugintest`: A generic test harness and ABI validator for plugin authors.

## Test-Driven Development (TDD)

Plugin authors use the `plugintest` package to drive out implementation by mocking host capabilities and exercising exports using raw bytes or serialized JSON from their domain contract.

---

*Build robust, secure, and performant extensions with the Wasm Microkernel Go SDK.*
