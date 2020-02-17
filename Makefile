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
GOIMAGE := golang:$(GOVERSION)

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

TEST_TIMEOUT := 25m

BINNAME := arangodb$(GOEXE)
TESTNAME := test$(GOEXE)
BIN := $(BINDIR)/$(GOOS)/$(GOARCH)/$(BINNAME)
TESTBIN := $(BINDIR)/$(GOOS)/$(GOARCH)/$(TESTNAME)
RELEASE := $(GOBUILDDIR)/bin/release
GHRELEASE := $(GOBUILDDIR)/bin/github-release

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
                -u "$(shell id -u)" \
                -e GOCACHE=/usr/code/.gobuild/.cache \
                -e GOPATH=/usr/code/.gobuild \
                -e GOOS=$(GOOS) \
                -e GOARCH=$(GOARCH) \
                -e CGO_ENABLED=0 \
                $(DOCKER_PARAMS) \
                -w /usr/code/ \
                $(DOCKER_IMAGE)
endif

.PHONY: all clean deps docker build build-local

all: build

clean:
	rm -Rf $(BIN) $(BINDIR) $(GOBUILDDIR) $(ROOTDIR)/arangodb

local:
ifneq ("$(DOCKERCLI)", "")
	@${MAKE} -f $(MAKEFILE) -B GOOS=$(shell go env GOHOSTOS) GOARCH=$(shell go env GOHOSTARCH) build-local
else
	@${MAKE} -f $(MAKEFILE) deps
	GOPATH=$(GOBUILDDIR) go build -o $(BUILDDIR)/arangodb $(REPOPATH)
endif

build-local: build
	@ln -sf "$(BIN)" "$(ROOTDIR)/arangodb"

build: $(BIN)

build-test: $(TESTBIN)

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

deps:
	@${MAKE} -f $(MAKEFILE) -B SCRIPTDIR=$(SCRIPTDIR) BUILDDIR=$(BUILDDIR) -s $(GOBUILDDIR)

$(GOBUILDDIR):
	@mkdir -p $(ORGDIR)
	@mkdir -p $(GOBUILDDIR)/src/golang.org
	@mkdir -p $(GOBUILDDIR)/src/github.com/arangodb
	@rm -f $(REPODIR) && ln -s $(GOBUILDLINKTARGET) $(REPODIR)
	@rm -f $(GOBUILDDIR)/src/github.com/aktau && ln -s ../../../deps/github.com/aktau $(GOBUILDDIR)/src/github.com/aktau
	@rm -f $(GOBUILDDIR)/src/github.com/arangodb/go-driver && ln -s ../../../../deps/github.com/arangodb/go-driver $(GOBUILDDIR)/src/github.com/arangodb/go-driver
	@rm -f $(GOBUILDDIR)/src/github.com/arangodb/go-velocypack && ln -s ../../../../deps/github.com/arangodb/go-velocypack $(GOBUILDDIR)/src/github.com/arangodb/go-velocypack
	@rm -f $(GOBUILDDIR)/src/github.com/arangodb-helper/go-certificates && ln -s ../../../../deps/github.com/arangodb-helper/go-certificates $(GOBUILDDIR)/src/github.com/arangodb-helper/go-certificates
	@rm -f $(GOBUILDDIR)/src/github.com/cenkalti && ln -s ../../../deps/github.com/cenkalti $(GOBUILDDIR)/src/github.com/cenkalti
	@rm -f $(GOBUILDDIR)/src/github.com/coreos && ln -s ../../../deps/github.com/coreos $(GOBUILDDIR)/src/github.com/coreos
	@rm -f $(GOBUILDDIR)/src/github.com/dchest && ln -s ../../../deps/github.com/dchest $(GOBUILDDIR)/src/github.com/dchest
	@rm -f $(GOBUILDDIR)/src/github.com/dgrijalva && ln -s ../../../deps/github.com/dgrijalva $(GOBUILDDIR)/src/github.com/dgrijalva
	@rm -f $(GOBUILDDIR)/src/github.com/docker && ln -s ../../../deps/github.com/docker $(GOBUILDDIR)/src/github.com/docker
	@rm -f $(GOBUILDDIR)/src/github.com/dustin && ln -s ../../../deps/github.com/dustin $(GOBUILDDIR)/src/github.com/dustin
	@rm -f $(GOBUILDDIR)/src/github.com/fatih && ln -s ../../../deps/github.com/fatih $(GOBUILDDIR)/src/github.com/fatih
	@rm -f $(GOBUILDDIR)/src/github.com/fsouza && ln -s ../../../deps/github.com/fsouza $(GOBUILDDIR)/src/github.com/fsouza
	@rm -f $(GOBUILDDIR)/src/github.com/hashicorp && ln -s ../../../deps/github.com/hashicorp $(GOBUILDDIR)/src/github.com/hashicorp
	@rm -f $(GOBUILDDIR)/src/github.com/inconshreveable && ln -s ../../../deps/github.com/inconshreveable $(GOBUILDDIR)/src/github.com/inconshreveable
	@rm -f $(GOBUILDDIR)/src/github.com/mitchellh && ln -s ../../../deps/github.com/mitchellh $(GOBUILDDIR)/src/github.com/mitchellh
	@rm -f $(GOBUILDDIR)/src/github.com/Microsoft && ln -s ../../../deps/github.com/Microsoft $(GOBUILDDIR)/src/github.com/Microsoft
	@rm -f $(GOBUILDDIR)/src/github.com/kballard && ln -s ../../../deps/github.com/kballard $(GOBUILDDIR)/src/github.com/kballard
	@rm -f $(GOBUILDDIR)/src/github.com/pavel-v-chernykh && ln -s ../../../deps/github.com/pavel-v-chernykh $(GOBUILDDIR)/src/github.com/pavel-v-chernykh
	@rm -f $(GOBUILDDIR)/src/github.com/pkg && ln -s ../../../deps/github.com/pkg $(GOBUILDDIR)/src/github.com/pkg
	@rm -f $(GOBUILDDIR)/src/github.com/rs && ln -s ../../../deps/github.com/rs $(GOBUILDDIR)/src/github.com/rs
	@rm -f $(GOBUILDDIR)/src/github.com/shavac && ln -s ../../../deps/github.com/shavac $(GOBUILDDIR)/src/github.com/shavac
	@rm -f $(GOBUILDDIR)/src/github.com/spf13 && ln -s ../../../deps/github.com/spf13 $(GOBUILDDIR)/src/github.com/spf13
	@rm -f $(GOBUILDDIR)/src/github.com/ryanuber && ln -s ../../../deps/github.com/ryanuber $(GOBUILDDIR)/src/github.com/ryanuber
	@rm -f $(GOBUILDDIR)/src/github.com/voxelbrain && ln -s ../../../deps/github.com/voxelbrain $(GOBUILDDIR)/src/github.com/voxelbrain
	@rm -f $(GOBUILDDIR)/src/golang.org/x && ln -s ../../../deps/golang.org/x $(GOBUILDDIR)/src/golang.org/x
	$(DOCKER_CMD) go get github.com/arangodb/go-upgrade-rules

$(BIN): $(GOBUILDDIR) $(SOURCES)
	@mkdir -p $(BINDIR)
	$(DOCKER_CMD) go build -installsuffix netgo -tags netgo -ldflags "-X main.projectVersion=$(VERSION) -X main.projectBuild=$(COMMIT)" -o "$(BUILD_BIN)" $(REPOPATH)

$(TESTBIN): $(GOBUILDDIR) $(TEST_SOURCES) $(BIN)
	@mkdir -p $(BINDIR)
	$(DOCKER_CMD) go test -c -o "$(TEST_BIN)" $(REPOPATH)/test

docker: build
	$(DOCKERCLI) build -t arangodb/arangodb-starter .

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
	@TEST_MODES=$(TEST_MODES) STARTER_MODES=$(STARTER_MODES) STARTER=$(BIN) ENTERPRISE=$(ENTERPRISE) IP=$(IP) ARANGODB=$(ARANGODB) $(TESTBIN) -test.timeout $(TEST_TIMEOUT) -test.v $(TESTOPTIONS)

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

