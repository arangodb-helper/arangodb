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

ALPINE_IMAGE ?= alpine:3.11

DOCKERCLI ?= $(shell which docker)
GOBUILDLINKTARGET := ../../../..

BUILDDIR ?= $(ROOTDIR)

GOBUILDDIR := $(BUILDDIR)/.gobuild
SRCDIR := $(SCRIPTDIR)
CACHEVOL := $(PROJECT)-gocache
BINDIR := $(BUILDDIR)/bin

ORGPATH := github.com/arangodb-helper
ORGDIR := $(GOBUILDDIR)/src/$(ORGPATH)
REPONAME := $(PROJECT)
REPODIR := $(ORGDIR)/$(REPONAME)
REPOPATH := $(ORGPATH)/$(REPONAME)

GOPATH := $(GOBUILDDIR)
GOVERSION := 1.13.6
GOIMAGE ?= golang:$(GOVERSION)

GOOS ?= linux
GOARCH ?= amd64

ifeq ("$(GOOS)", "windows")
	GOEXE := .exe
endif

ARANGODB ?= arangodb/arangodb:latest
DOCKERNAMESPACE ?= arangodb

ifdef TRAVIS
	IP := $(shell hostname -I | cut -d ' ' -f 1)
endif

TEST_TIMEOUT := 1h

BINNAME := arangodb$(GOEXE)
TESTNAME := test$(GOEXE)
BIN := $(BINDIR)/$(GOOS)/$(GOARCH)/$(BINNAME)
TESTBIN := $(BINDIR)/$(GOOS)/$(GOARCH)/$(TESTNAME)
RELEASE := $(GOBUILDDIR)/bin/release
GHRELEASE := $(GOBUILDDIR)/bin/github-release

GO_IGNORED:=vendor .gobuild

GO_SOURCES_QUERY := find $(SRCDIR) -name '*.go' -type f $(foreach IGNORED,$(GO_IGNORED),-not -path '$(SRCDIR)/$(IGNORED)/*' )
GO_SOURCES := $(shell $(GO_SOURCES_QUERY) | sort | uniq)
GO_SOURCES_PACKAGES := $(shell $(GO_SOURCES_QUERY) -exec dirname {} \; | sort | uniq)

SOURCES := $(shell find $(SRCDIR) -name '*.go' -not -path './test/*')
TEST_SOURCES := $(shell find $(SRCDIR)/test -name '*.go')

DOCKER_IMAGE := $(GOIMAGE)

ifeq ($(DOCKERCLI),)
BUILD_BIN := $(BIN)
TEST_BIN := $(TESTBIN)
RELEASE_BIN := $(RELEASE)
GHRELEASE_BIN := $(GHRELEASE)

DOCKER_CMD :=

pre:
	@if ! go version | grep -q "go1.13"; then echo "GO in Version 1.13 required"; exit 1; fi

deps: pre

build: pre

%: export CGO_ENABLED := 0
%: export GOARCH := $(GOARCH)
%: export GOOS := $(GOOS)
%: export GOPATH := $(GOPATH)
%: export GOCACHE := $(GOPATH)/.cache
%: export GO111MODULE := off

else
BUILD_BIN := /usr/code/bin/$(GOOS)/$(GOARCH)/$(BINNAME)
TEST_BIN := /usr/code/bin/$(GOOS)/$(GOARCH)/$(TESTNAME)
RELEASE_BIN := /usr/code/.gobuild/bin/release
GHRELEASE_BIN := /usr/code/.gobuild/bin/github-release

DOCKER_CMD = $(DOCKERCLI) run \
                --rm \
                -v $(SRCDIR):/usr/code \
                -u "$(shell id -u):$(shell id -g)" \
                -e GOCACHE=/usr/code/.gobuild/.cache \
                -e GOPATH=/usr/code/.gobuild \
                -e GOOS=$(GOOS) \
                -e GOARCH=$(GOARCH) \
                -e CGO_ENABLED=0 \
                -e TRAVIS=$(TRAVIS) \
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

build: vendor $(BIN)

build-test: vendor $(TESTBIN)

binaries: $(GHRELEASE)
	@${MAKE} -f $(MAKEFILE) -B GOOS=linux GOARCH=amd64 build
	@${MAKE} -f $(MAKEFILE) -B GOOS=linux GOARCH=arm64 build
	@${MAKE} -f $(MAKEFILE) -B GOOS=darwin GOARCH=amd64 build
	@${MAKE} -f $(MAKEFILE) -B GOOS=windows GOARCH=amd64 build

binaries-test: $(GHRELEASE)
	@${MAKE} -f $(MAKEFILE) -B GOOS=linux GOARCH=amd64 build-test
	@${MAKE} -f $(MAKEFILE) -B GOOS=linux GOARCH=arm64 build-test
	@${MAKE} -f $(MAKEFILE) -B GOOS=darwin GOARCH=amd64 build-test
	@${MAKE} -f $(MAKEFILE) -B GOOS=windows GOARCH=amd64 build-test

$(BIN): $(GOBUILDDIR) $(SOURCES)
	@mkdir -p $(BINDIR)
	$(DOCKER_CMD) go build -installsuffix netgo -tags netgo -ldflags "-X main.projectVersion=$(VERSION) -X main.projectBuild=$(COMMIT)" -o "$(BUILD_BIN)" .

$(TESTBIN): $(GOBUILDDIR) $(TEST_SOURCES) $(BIN)
	@mkdir -p $(BINDIR)
	$(DOCKER_CMD) go test -c -o "$(TEST_BIN)" ./test

docker: build
	$(DOCKERCLI) build -t arangodb/arangodb-starter --build-arg "IMAGE=$(ALPINE_IMAGE)" .

docker-push: docker
ifneq ($(DOCKERNAMESPACE), arangodb)
	docker tag arangodb/arangodb-starter $(DOCKERNAMESPACE)/arangodb-starter
endif
	docker push $(DOCKERNAMESPACE)/arangodb-starter

docker-push-version: docker
	docker tag arangodb/arangodb-starter arangodb/arangodb-starter:$(VERSION)
	docker tag arangodb/arangodb-starter arangodb/arangodb-starter:$(VERSION_MAJOR_MINOR)
	docker tag arangodb/arangodb-starter arangodb/arangodb-starter:$(VERSION_MAJOR)
	docker tag arangodb/arangodb-starter arangodb/arangodb-starter:latest
	docker push arangodb/arangodb-starter:$(VERSION)
	docker push arangodb/arangodb-starter:$(VERSION_MAJOR_MINOR)
	docker push arangodb/arangodb-starter:$(VERSION_MAJOR)
	docker push arangodb/arangodb-starter:latest

$(RELEASE): $(GOBUILDDIR) $(SOURCES) $(GHRELEASE)
	$(DOCKER_CMD) go build -o "$(RELEASE_BIN)" $(REPOPATH)/tools/release

$(GHRELEASE): $(GOBUILDDIR) 
	$(DOCKER_CMD) go build -o "$(GHRELEASE_BIN)" github.com/aktau/github-release

release-patch: $(RELEASE)
	$(RELEASE) -type=patch

release-minor: $(RELEASE)
	$(RELEASE) -type=minor

release-major: $(RELEASE)
	$(RELEASE) -type=major

TESTCONTAINER := arangodb-starter-test

# Run all integration tests
run-tests: run-tests-local-process run-tests-docker

run-tests-local-process: build-test build run-tests-local-process-run
run-tests-local-process-run: export TEST_MODES=localprocess
run-tests-local-process-run: export DOCKER_IMAGE=$(ARANGODB)
run-tests-local-process-run: export DOCKER_PARAMS:=-e "TEST_MODES=$(TEST_MODES)" -e "STARTER_MODES=$(STARTER_MODES)" -e "STARTER=/usr/code/bin/linux/amd64/arangodb" -e "ENTERPRISE=$(ENTERPRISE)"
run-tests-local-process-run:
	@-$(DOCKERCLI) rm -f -v $(TESTCONTAINER) &> /dev/null
	$(DOCKER_CMD) /usr/code/bin/linux/amd64/test -test.timeout $(TEST_TIMEOUT) -test.v $(TESTOPTIONS)

_run-tests: build-test build
	@TEST_MODES=$(TEST_MODES) STARTER_MODES=$(STARTER_MODES) STARTER=$(BIN) ENTERPRISE=$(ENTERPRISE) IP=$(IP) ARANGODB=$(ARANGODB) $(TESTBIN) -test.timeout $(TEST_TIMEOUT) -test.failfast -test.v $(TESTOPTIONS)

ifdef TRAVIS
run-tests-docker-pre: docker
	@$(DOCKERCLI) pull $(ARANGODB)

run-tests-docker: run-tests-docker-pre
endif

run-tests-docker: TEST_MODES=docker
run-tests-docker: docker _run-tests

# Run all integration tests on the local system
run-tests-local: export TEST_MODES=localprocess
run-tests-local: _run-tests

$(GOBUILDDIR):
	@mkdir -p "$(GOBUILDDIR)"

## LINT

GOLANGCI_ENABLED=deadcode gocyclo golint varcheck structcheck maligned errcheck \
                 ineffassign interfacer unconvert goconst \
                 megacheck

.PHONY: tools
tools:
	@echo ">> Fetching goimports"
	@go get -u golang.org/x/tools/cmd/goimports
	@echo ">> Fetching license check"
	@go get -u github.com/google/addlicense
	@echo ">> Fetching github release"
	@go get -u github.com/aktau/github-release


.PHONY: license
license:
	@echo ">> Verify license of files"
	@go run github.com/google/addlicense -f "./LICENSE.BOILERPLATE" $(GO_SOURCES)

.PHONY: license-verify
license-verify:
	@echo ">> Ensuring license of files"
	@go run github.com/google/addlicense -f "./LICENSE.BOILERPLATE" -check $(GO_SOURCES)

.PHONY: fmt
fmt:
	@echo ">> Ensuring style of files"
	@goimports -w $(GO_SOURCES)

.PHONY: fmt-verify
fmt-verify: license-verify
	@echo ">> Verify files style"
	@if [ X"$$(go goimports -l $(GO_SOURCES) | wc -l)" != X"0" ]; then echo ">> Style errors"; go goimports -l $(GO_SOURCES); exit 1; fi

.PHONY: linter
linter: fmt
	@golangci-lint run --no-config --issues-exit-code=1 --deadline=30m --disable-all \
	                  $(foreach MODE,$(GOLANGCI_ENABLED),--enable $(MODE) ) \
	                  --exclude-use-default=false \
	                  $(GO_SOURCES_PACKAGES)

.PHONY: vendor
vendor:
	@go mod vendor
