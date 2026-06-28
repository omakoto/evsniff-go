#!/bin/bash
# Pre-submit script to verify code formatting, compilation, linting, and tests.
#
# This script runs:
# 1. gofmt check
# 2. go build
# 3. go vet
# 4. go test with race detector

set -e

# Change to the script's directory
cd "${0%/*}"

echo "=== Running gofmt check ==="
UNFORMATTED=$(gofmt -l .)
if [[ -n "$UNFORMATTED" ]]; then
    echo "The following files are not formatted properly:"
    echo "$UNFORMATTED"
    echo "Please format them with: gofmt -w ."
    exit 1
fi
echo "All files formatted correctly."

echo "=== Running go build ==="
go build ./...

echo "=== Running go vet ==="
go vet ./...

echo "=== Running go test ==="
go test -v -race ./...

echo "=== All checks passed successfully! ==="
