//go:build wasip1

// Package main is the "ping" test plugin used by host integration tests.
// It is intentionally minimal: it echoes its input wrapped in a deterministic
// envelope, exercises one host capability (LogMsg), and returns an error on a
// known sentinel input. Build with:
//
//      GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared \
//          -o host/testdata/ping.wasm ./host/testdata/ping
package main

import (
        "fmt"
        "strings"

        "github.com/gavmor/wasm-microkernel/guest"
)

func init() {
        guest.Register(func(reqJSON string) (string, error) {
                switch {
                case reqJSON == "crash":
                        return "", fmt.Errorf("simulated plugin error")
                case reqJSON == "":
                        return `{"status":"pong","echo":null}`, nil
                case strings.HasPrefix(reqJSON, "POST "):
                        // Format: "POST <url> <body>" — exercises guest.HttpPost so the
                        // host's http_post import + framed-response path is covered by
                        // integration tests. The host points the URL at httptest.Server.
                        rest := strings.TrimPrefix(reqJSON, "POST ")
                        sp := strings.IndexByte(rest, ' ')
                        if sp < 0 {
                                return "", fmt.Errorf("malformed POST request: %q", reqJSON)
                        }
                        url, body := rest[:sp], rest[sp+1:]
                        resp, err := guest.HttpPost(url, body)
                        if err != nil {
                                return "", fmt.Errorf("http_post: %w", err)
                        }
                        return resp, nil
                }

                guest.LogMsg("ping received: " + reqJSON)
                return `{"status":"pong","echo":` + reqJSON + `}`, nil
        })
}

// Required by the Go toolchain; never executed in reactor mode.
func main() {}
