#!/bin/bash

set -e

cd "${0%/*}"

go install ./cmd/evsniff/...
