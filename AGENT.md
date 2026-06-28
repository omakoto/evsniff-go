# Agent Rules for evsniff-go

This file contains guidelines and behavioral constraints that AI coding assistants (such as Gemini and Claude) must adhere to when working on this project.

## Important Rules

*   **Always Run the Presubmit Script**: Prior to concluding any task or committing changes, you must run the `./9-presubmit.sh` script to ensure that:
    1. The codebase is properly formatted with `gofmt`.
    2. The application compiles successfully (`go build`).
    3. The compiler lints are clean (`go vet`).
    4. All integration tests pass cleanly under the Go race detector (`go test -v -race`).
