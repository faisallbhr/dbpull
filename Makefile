APP := dbpull
MODULE := $(shell go list -m)
BUILDINFO_PKG := $(MODULE)/internal/buildinfo
VERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X $(BUILDINFO_PKG).Version=$(VERSION) -X $(BUILDINFO_PKG).Commit=$(COMMIT) -X $(BUILDINFO_PKG).BuildDate=$(BUILD_DATE)
GO_BUILD := CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="$(LDFLAGS)"

.PHONY: fmt test vet build build-all release clean version

fmt:
	gofmt -w .

test:
	go test ./...

vet:
	go vet ./...

build:
	$(GO_BUILD) -o $(APP) .

build-all:
	VERSION=$(VERSION) COMMIT=$(COMMIT) BUILD_DATE=$(BUILD_DATE) ./scripts/build-all.sh

release: fmt test vet build-all

clean:
	rm -rf dist $(APP)

version:
	@echo $(VERSION)
