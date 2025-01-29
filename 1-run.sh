#!/bin/bash

set -e

cd "${0%/*}"

go run ./cmd/evsniff "$@"
