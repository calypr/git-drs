#!/usr/bin/env bash

set -euo pipefail

rtk env GOCACHE=/tmp/git-drs-poteto-vet-cache go vet ./...
rtk env GOCACHE=/tmp/git-drs-poteto-deadcode-cache deadcode ./...
rtk proxy rg -n '^var \(' --glob '*.go' --glob '!**/*_test.go'
rtk proxy rg -n '(^|[^[:alnum:]_])_ = [[:alnum:]_.]+\(' --glob '*.go' --glob '!**/*_test.go'
