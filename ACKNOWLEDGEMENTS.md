# Acknowledgements

## Architectural Inspiration & Go/WASM Integration

- **[Arcjet Engineering Blog: The Wasm Component Model and idiomatic codegen](https://blog.arcjet.com/the-wasm-component-model-and-idiomatic-codegen/)**: Their breakdown of the "factory/instance pattern" for WebAssembly modules and the trade-offs of generating idiomatic Go bindings was directly relevant to how we structured the `pkg/kernel` execution loop to optimize performance and instantiation.
    
- **[Radu Matei: Introduction to WebAssembly components](https://radu-matei.com/blog/intro-wasm-components/)**: One of the foundational deep-dives into defining a "process model" for WASM outside the browser. His early examples of using WIT to define API layers provided a clear blueprint for how the `axe-protocol` contract should behave.
    
- **[HashiCorp Plugin System](https://github.com/hashicorp/go-plugin)**: While Axe uses WASM rather than RPC, the design patterns for plugin discovery, handshaking, and lifecycle management are deeply rooted in the standards set by HashiCorp.
    

## The Component Model & WIT

- **[Fermyon: The WebAssembly Component Model](https://www.fermyon.com/blog/webassembly-component-model)**: Their conceptual breakdown mapping native executable concepts to WASM components (e.g., equating a Component Instance to a stateful Process) helped clarify the isolation boundaries needed for our plugin architecture.
    
- **[Cosmonic: Get WITtier with this handy guide to Wasm Interface Types](https://cosmonic.com/blog/engineering/wit-cheat-sheet)**: Bailey Hayes’s guide on WIT syntax and language-agnostic interface definition languages (IDLs) was the practical cheat sheet that helped us conceptualize the semantic ABI validation and the implementation of `watgo`.
