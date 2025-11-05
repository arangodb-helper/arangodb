PROJECT := arangodb
ifndef SCRIPTDIR
	SCRIPTDIR := $(shell pwd)
endif
ROOTDIR := $(shell cd $(SCRIPTDIR) && pwd)
VERSION := $(shell cat $(ROOTDIR)/VERSION)
VERSION_MAJOR_MINOR_PATCH := $(shell echo $(VERSION) | cut -f 1 -d '+')
VERSION_MAJOR_MINOR := $(shell echo $(VERSION_MAJOR_MINOR_PATCH) | cut -f 1,2 -d '.')
VERSION_MAJOR := $(shell echo $(VERSION_MAJOR_MINOR) | cut -f 1 -d '.')
COMMIT := $(shell git rev-parse --short HEAD)
MAKEFILE := $(ROOTDIR)/Makefile

DOCKERNAMESPACE ?= arangodb
IMAGE_NAME := $(DOCKERNAMESPACE)/arangodb-starter

STARTER_TAGS := -t $(IMAGE_NAME):$(VERSION)
ifeq (, $(findstring -preview,$(VERSION)))
	STARTER_TAGS = -t $(IMAGE_NAME):$(VERSION) \
		-t $(IMAGE_NAME):$(VERSION_MAJOR_MINOR) \
		-t $(IMAGE_NAME):$(VERSION_MAJOR) \
		-t $(IMAGE_NAME):latest
endif

GOBUILDLINKTARGET := ../../../..

BUILDDIR ?= $(ROOTDIR)

GOBUILDDIR := $(BUILDDIR)/.gobuild
SRCDIR := $(SCRIPTDIR)
CACHEVOL := $(PROJECT)-gocache
BINDIR := $(BUILDDIR)/bin
RELEASEDIR:=$(BUILDDIR)/bin/release/$(VERSION)

ORGPATH := github.com/arangodb-helper
ORGDIR := $(GOBUILDDIR)/src/$(ORGPATH)
REPONAME := $(PROJECT)
REPODIR := $(ORGDIR)/$(REPONAME)
REPOPATH := $(ORGPATH)/$(REPONAME)

ALPINE_IMAGE ?= alpine:3.21

GOPATH := $(GOBUILDDIR)
GOVERSION := 1.24.9
GOIMAGE ?= golang:$(GOVERSION)-alpine3.21

GOOS ?= linux
GOARCH ?= amd64

ifeq ("$(GOOS)", "windows")
	GOEXE := .exe
endif

DOCKERCLI ?= $(shell which docker)
DOCKER_PLATFORMS ?= linux/amd64,linux/arm64
DOCKER_BUILD_CLI := $(DOCKERCLI) build --build-arg "IMAGE=$(ALPINE_IMAGE)" --platform $(DOCKER_PLATFORMS)

ARANGODB ?= arangodb/arangodb:latest

TEST_TIMEOUT := 1h

BINNAME := arangodb$(GOEXE)
TESTNAME := test$(GOEXE)
BIN := $(BINDIR)/$(GOOS)/$(GOARCH)/$(BINNAME)
RELEASEBIN:=$(RELEASEDIR)/arangodb-$(GOOS)-$(GOARCH)$(GOEXE)
TESTBIN := $(BINDIR)/$(GOOS)/$(GOARCH)/$(TESTNAME)
RELEASE := $(GOBUILDDIR)/bin/release

GO_IGNORED:=vendor .gobuild

GO_SOURCES_QUERY := find $(SRCDIR) -name '*.go' -type f $(foreach IGNORED,$(GO_IGNORED),-not -path '$(SRCDIR)/$(IGNORED)/*' )
GO_SOURCES := $(shell $(GO_SOURCES_QUERY) | sort | uniq)
TEST_SOURCES := $(shell find $(SRCDIR)/test -name '*.go')

DOCKER_IMAGE := $(GOIMAGE)

ifeq ($(DOCKERCLI),)
BUILD_BIN := $(BIN)
TEST_BIN := $(TESTBIN)
RELEASE_BIN := $(RELEASE)

DOCKER_CMD :=

pre:
	@if ! go version | grep -q "go$(GOVERSION)"; then echo "GO in Version $(GOVERSION) required"; exit 1; fi

deps: pre

build: pre

%: export CGO_ENABLED := 0
%: export GOARCH := $(GOARCH)
%: export GOOS := $(GOOS)
%: export GOPATH := $(GOPATH)
%: export GOCACHE := $(GOPATH)/.cache

else
BUILD_BIN := /usr/code/bin/$(GOOS)/$(GOARCH)/$(BINNAME)
TEST_BIN := /usr/code/bin/$(GOOS)/$(GOARCH)/$(TESTNAME)
RELEASE_BIN := /usr/code/.gobuild/bin/release

DOCKER_CMD = $(DOCKERCLI) run \
                --rm \
                --net=host --init \
                -v $(SRCDIR):/usr/code \
                -u "$(shell id -u):$(shell id -g)" \
                -e GOCACHE=/usr/code/.gobuild/.cache \
                -e GOPATH=/usr/code/.gobuild \
                -e GOOS=$(GOOS) \
                -e GOARCH=$(GOARCH) \
                -e CGO_ENABLED=0 \
                -e VERBOSE=$(VERBOSE) \
                $(DOCKER_PARAMS) \
                -w /usr/code/ \
                $(DOCKER_IMAGE)
endif

.PHONY: all clean deps docker build build-local

all: build

clean:
	rm -Rf $(BIN) $(BINDIR) $(ROOTDIR)/arangodb

local:
ifneq ("$(DOCKERCLI)", "")
	@${MAKE} -f $(MAKEFILE) -B GOOS=$(shell go env GOHOSTOS) GOARCH=$(shell go env GOHOSTARCH) build-local
else
	@${MAKE} -f $(MAKEFILE) deps
	GOPATH=$(GOBUILDDIR) go build -o $(BUILDDIR)/arangodb $(REPOPATH)
endif

build-local: build
	@ln -sf "$(BIN)" "$(ROOTDIR)/arangodb"

release: $(RELEASEBIN)

$(RELEASEBIN): vendor $(BIN)
	@mkdir -p "$(RELEASEDIR)"
	@cp "$(BIN)" "$(RELEASEBIN)"

build: vendor $(BIN)
	@echo ">> Build Bin $(BIN) done"

build-test: vendor $(TESTBIN)
	@echo ">> Build Tests Bin $(TESTBIN) done"

binary-linux:
	@${MAKE} -f $(MAKEFILE) -B GOOS=linux GOARCH=amd64 build
	@${MAKE} -f $(MAKEFILE) -B GOOS=linux GOARCH=arm64 build
	@${MAKE} -f $(MAKEFILE) -B GOOS=linux GOARCH=amd64 build-test
	@${MAKE} -f $(MAKEFILE) -B GOOS=linux GOARCH=arm64 build-test

binary-darwin:
	@${MAKE} -f $(MAKEFILE) -B GOOS=darwin GOARCH=amd64 build
	@${MAKE} -f $(MAKEFILE) -B GOOS=darwin GOARCH=arm64 build
	@${MAKE} -f $(MAKEFILE) -B GOOS=darwin GOARCH=amd64 build-test
	@${MAKE} -f $(MAKEFILE) -B GOOS=darwin GOARCH=arm64 build-test

binary-windows:
	@${MAKE} -f $(MAKEFILE) -B GOOS=windows GOARCH=amd64 build
	@${MAKE} -f $(MAKEFILE) -B GOOS=windows GOARCH=amd64 build-test

binaries: binary-linux binary-darwin binary-windows

releases:
	@${MAKE} -f $(MAKEFILE) -B GOOS=linux GOARCH=amd64 release
	@${MAKE} -f $(MAKEFILE) -B GOOS=linux GOARCH=arm64 release
	@${MAKE} -f $(MAKEFILE) -B GOOS=darwin GOARCH=amd64 release
	@${MAKE} -f $(MAKEFILE) -B GOOS=darwin GOARCH=arm64 release
	@${MAKE} -f $(MAKEFILE) -B GOOS=windows GOARCH=amd64 release
	@(cd "$(RELEASEDIR)"; sha256sum arangodb-* > SHA256SUMS; cat SHA256SUMS | sha256sum -c)

$(BIN): $(GOBUILDDIR) $(GO_SOURCES)
	@mkdir -p $(BINDIR)
	@-rm -f resource.syso
ifeq ($(GOOS),windows)
	@echo ">> Generating versioninfo syso file ..."
	@$(GOPATH)/bin/goversioninfo -64 -file-version=$(VERSION) -product-version=$(VERSION)
	$(DOCKER_CMD) go build -ldflags "-X main.projectVersion=$(VERSION) -X main.projectBuild=$(COMMIT)" -o "$(BUILD_BIN)" .
	@-rm -f resource.syso
else
	$(DOCKER_CMD) go build -installsuffix netgo -tags netgo -ldflags "-X main.projectVersion=$(VERSION) -X main.projectBuild=$(COMMIT)" -o "$(BUILD_BIN)" .
endif

$(TESTBIN): $(GOBUILDDIR) $(TEST_SOURCES) $(BIN)
	@mkdir -p $(BINDIR)
	$(DOCKER_CMD) go test -c -o "$(TEST_BIN)" ./test

docker: build
	@echo ">> Building Docker Image with buildx"
	$(DOCKER_BUILD_CLI) -t arangodb/arangodb-starter .

docker-push-version: docker
	$(DOCKER_BUILD_CLI) --push $(STARTER_TAGS) .

$(RELEASE): $(GOBUILDDIR) $(GO_SOURCES)
	$(DOCKER_CMD) go build -o "$(RELEASE_BIN)" $(REPOPATH)/tools/release

release-patch: $(RELEASE)
	$(RELEASE) -type=patch

release-minor: $(RELEASE)
	$(RELEASE) -type=minor

release-major: $(RELEASE)
	$(RELEASE) -type=major

prerelease-patch: $(RELEASE)
	$(RELEASE) -type=patch -prerelease

prerelease-minor: $(RELEASE)
	$(RELEASE) -type=minor -prerelease

prerelease-major: $(RELEASE)
	$(RELEASE) -type=major -prerelease

TESTCONTAINER := arangodb-starter-test

# Run all unit tests
run-unit-tests: $(GO_SOURCES)
	go test --count=1 -v \
		$(REPOPATH) \
		$(REPOPATH)/pkg/... \
		$(REPOPATH)/service/... \
		$(REPOPATH)/client/...

# Run all integration tests
run-tests: run-tests-local-process run-tests-docker

run-tests-local-process: binary-linux run-tests-local-process-run
run-tests-local-process-run: export TEST_MODES=localprocess
run-tests-local-process-run: export DOCKER_IMAGE=$(ARANGODB)
run-tests-local-process-run: export DOCKER_PARAMS:=-e "TEST_MODES=$(TEST_MODES)" -e "STARTER_MODES=$(STARTER_MODES)" -e "STARTER=/usr/code/bin/linux/$(GOARCH)/arangodb"
run-tests-local-process-run:
	@-$(DOCKERCLI) rm -f -v $(TESTCONTAINER) &> /dev/null
	$(DOCKER_CMD) /usr/code/bin/linux/$(GOARCH)/test -test.timeout $(TEST_TIMEOUT) -test.v $(TESTOPTIONS)

_run-tests: build-test build
	@TEST_MODES=$(TEST_MODES) STARTER_MODES=$(STARTER_MODES) STARTER=$(BIN) ARANGODB=$(ARANGODB) $(TESTBIN) -test.timeout $(TEST_TIMEOUT) -test.failfast -test.v $(TESTOPTIONS)

run-tests-docker: TEST_MODES=docker
run-tests-docker: docker _run-tests

# Run all integration tests on the local system
run-tests-local: export TEST_MODES=localprocess
run-tests-local: _run-tests

$(GOBUILDDIR):
	@mkdir -p "$(GOBUILDDIR)"

.PHONY: tools
tools:
	@echo ">> Fetching golangci-lint linter"
	@GOBIN=$(GOPATH)/bin go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.52.2
	@echo ">> Fetching gci"
	@GOBIN=$(GOPATH)/bin go install github.com/daixiang0/gci@v0.3.0
	@echo ">> Fetching goimports"
	@GOBIN=$(GOPATH)/bin go install golang.org/x/tools/cmd/goimports@v0.32.0
	@echo ">> Fetching license check"
	@GOBIN=$(GOPATH)/bin go install github.com/google/addlicense@6d92264d717064f28b32464f0f9693a5b4ef0239
	@echo ">> Fetching github release"
	@GOBIN=$(GOPATH)/bin go install github.com/aktau/github-release@v0.8.1
	@echo ">> Fetching govulncheck"
	@GOBIN=$(GOPATH)/bin go install golang.org/x/vuln/cmd/govulncheck@v1.1.3
	@echo ">> Fetching goversioninfo"
	@GOBIN=$(GOPATH)/bin go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.4.0

.PHONY: generate
generate:
	go generate

.PHONY: license
license:
	@echo ">> Verify license of files"
	@$(GOPATH)/bin/addlicense -f "./LICENSE.BOILERPLATE" $(GO_SOURCES)

.PHONY: license-verify
license-verify:
	@echo ">> Ensuring license of files"
	@$(GOPATH)/bin/addlicense -f "./LICENSE.BOILERPLATE" -check $(GO_SOURCES)

.PHONY: fmt
fmt:
	@echo ">> Ensuring style of files"
	@$(GOPATH)/bin/goimports -w $(GO_SOURCES)
	@$(GOPATH)/bin/gci write -s "standard" -s "default" -s "prefix(github.com/arangodb)" -s "prefix(github.com/arangodb-helper/arangodb)" $(GO_SOURCES)

.PHONY: fmt-verify
fmt-verify: license-verify
	@echo ">> Verify files style"
	@if [ X"$$($(GOPATH)/bin/goimports -l $(GO_SOURCES) | wc -l)" != X"0" ]; then echo ">> Style errors"; $(GOPATH)/bin/goimports -l $(GO_SOURCES); exit 1; fi

.PHONY: linter
linter: fmt
	$(GOPATH)/bin/golangci-lint run ./...

.PHONY: vulncheck
vulncheck:
	$(GOPATH)/bin/govulncheck ./...

.PHONY: vendor
vendor:
	@go mod vendor

.PHONY: init
init: vendor tools

.PHONY: check
check: license-verify fmt-verify run-unit-tests

local-release:
	@mkdir 