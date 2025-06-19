# The commands used in this Makefile expect to be interpreted by bash.
SHELL := /bin/bash

TESTS=$(shell go list ./... | grep -v /vendor/ | grep -v github-release-notes)

git_commit := $(shell git rev-parse --short HEAD)
git_branch := $(shell git symbolic-ref -q --short HEAD)
git_upstream := $(shell git config --get remote.origin.url)
export GIT_BRANCH = $(git_branch)
export GIT_UPSTREAM = $(git_upstream)

export GITDRS_VERSION=0.1.0

# LAST_PR_NUMBER is used by the release notes builder to generate notes
# based on pull requests (PR) up until the last release.
export LAST_PR_NUMBER = 1

VERSION_LDFLAGS=\
 -X "github.com/bmeg/git-drs/version.BuildDate=$(shell date)" \
 -X "github.com/bmeg/git-drs/version.GitCommit=$(git_commit)" \
 -X "github.com/bmeg/git-drs/version.GitBranch=$(git_branch)" \
 -X "github.com/bmeg/git-drs/version.GitUpstream=$(git_upstream)" \
 -X "github.com/bmeg/git-drs/version.Version=$(GITDRS_VERSION)"

export CGO_ENABLED=0

# Build the code
install:
	@mkdir -p version
	@touch version/version.go
	@go install -ldflags '$(VERSION_LDFLAGS)' .

# Build the code
build:
	@touch version/version.go
	@go build -ldflags '$(VERSION_LDFLAGS)' -buildvcs=false .

lint-depends:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.50.1
	go install golang.org/x/tools/cmd/goimports

# Run code style and other checks
lint:
	@golangci-lint run --timeout 3m --disable-all \
	    --enable=vet \
	    --enable=golint \
	    --enable=gofmt \
	    --enable=goimports \
	    --enable=misspell \
	    ./...

# Run all tests
test:
	@go test $(TESTS)

test-verbose:
	@go test -v $(TESTS)

# Build binaries for all OS/Architectures
snapshot: release-dep
	@goreleaser \
		--clean \
		--snapshot

# Create a release on Github using GoReleaser
release:
	@go get github.com/buchanae/github-release-notes
	@goreleaser \
		--clean \
		--release-notes <(github-release-notes -org bmeg -repo git-drs -stop-at ${LAST_PR_NUMBER})

# Install dependencies for release
# https://goreleaser.com/install/#linux-packages
release-dep:
	@go install github.com/goreleaser/goreleaser
	@go install github.com/buchanae/github-release-notes

# Make everything usually needed to prepare for a pull request
full: proto install tidy lint test website webdash

# Remove build/development files.
clean:
	@rm -rf ./bin ./pkg ./test_tmp ./build ./buildtools

.PHONY: proto proto-lint website docker webdash build debug
