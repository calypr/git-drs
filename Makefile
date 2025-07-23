# The commands used in this Makefile expect to be interpreted by bash.
# Adapted from Funnel's Makefile:
# https://github.com/ohsu-comp-bio/funnel/blob/master/Makefile

SHELL := /bin/bash

TESTS=$(shell go list ./... | grep -v /vendor/)

git_commit := $(shell git rev-parse --short HEAD)
git_branch := $(shell git symbolic-ref -q --short HEAD)
git_upstream := $(shell git config --get remote.origin.url)
export GIT_BRANCH = $(git_branch)
export GIT_UPSTREAM = $(git_upstream)

# Determine if the current commit has a tag
git_tag := $(shell git describe --tags --exact-match --abbrev=0 2>/dev/null)

ifeq ($(git_tag),)
    version := unknown
else
    version := $(git_tag)
endif

VERSION_LDFLAGS=\
 -X "github.com/bmeg/git-drs/version.BuildDate=$(shell date)" \
 -X "github.com/bmeg/git-drs/version.GitCommit=$(git_commit)" \
 -X "github.com/bmeg/git-drs/version.GitBranch=$(git_branch)" \
 -X "github.com/bmeg/git-drs/version.GitUpstream=$(git_upstream)" \
 -X "github.com/bmeg/git-drs/version.Version=$(version)"

export CGO_ENABLED=0

# Build the code
install:
	@go install -ldflags '$(VERSION_LDFLAGS)' .

# Build the code
build:
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

# Make everything usually needed to prepare for a pull request
full: proto install tidy lint test website webdash

# Remove build/development files.
clean:
	@rm -rf ./bin ./pkg ./test_tmp ./build ./buildtools

.PHONY: proto proto-lint website docker webdash build debug
