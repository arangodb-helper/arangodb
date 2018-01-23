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

ifndef NODOCKER
	DOCKERCLI := $(shell which docker)
	GOBUILDLINKTARGET := ../../../..
else
	DOCKERCLI := 
	GOBUILDLINKTARGET := $(ROOTDIR)
endif

ifndef BUILDDIR
	BUILDDIR := $(ROOTDIR)
endif
GOBUILDDIR := $(BUILDDIR)/.gobuild
SRCDIR := $(SCRIPTDIR)
BINDIR := $(BUILDDIR)/bin

ORGPATH := github.com/arangodb-helper
ORGDIR := $(GOBUILDDIR)/src/$(ORGPATH)
REPONAME := $(PROJECT)
REPODIR := $(ORGDIR)/$(REPONAME)
REPOPATH := $(ORGPATH)/$(REPONAME)

GOPATH := $(GOBUILDDIR)
GOVERSION := 1.9.2-alpine

ifndef GOOS
	GOOS := linux
endif
ifndef GOARCH
	GOARCH := amd64
endif
ifeq ("$(GOOS)", "windows")
	GOEXE := .exe
endif

ifndef ARANGODB
	ARANGODB := arangodb/arangodb:latest
endif

ifndef DOCKERNAMESPACE
	DOCKERNAMESPACE := arangodb
endif

ifdef TRAVIS
	IP := $(shell hostname -I | cut -d ' ' -f 1)
	echo Using IP=$(IP)
endif

TEST_TIMEOUT := 20m

BINNAME := arangodb$(GOEXE)
BIN := $(BINDIR)/$(GOOS)/$(GOARCH)/$(BINNAME)
RELEASE := $(GOBUILDDIR)/bin/release 
GHRELEASE := $(GOBUILDDIR)/bin/github-release 

SOURCES := $(shell find $(SRCDIR) -name '*.go' -not -path './test/*')

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

build: $(BIN)

build-local: build 
	@ln -sf $(BIN) $(ROOTDIR)/arangodb

binaries: $(GHRELEASE)
	@${MAKE} -f $(MAKEFILE) -B GOOS=linux GOARCH=amd64 build
	@${MAKE} -f $(MAKEFILE) -B GOOS=darwin GOARCH=amd64 build
	@${MAKE} -f $(MAKEFILE) -B GOOS=windows GOARCH=amd64 build

deps:
	@${MAKE} -f $(MAKEFILE) -B SCRIPTDIR=$(SCRIPTDIR) BUILDDIR=$(BUILDDIR) -s $(GOBUILDDIR)

$(GOBUILDDIR):
	@mkdir -p $(ORGDIR)
	@rm -f $(REPODIR) && ln -s $(GOBUILDLINKTARGET) $(REPODIR)
	@rm -f $(GOBUILDDIR)/src/github.com/aktau && ln -s ../../../vendor/github.com/aktau $(GOBUILDDIR)/src/github.com/aktau
	@rm -f $(GOBUILDDIR)/src/github.com/dchest && ln -s ../../../vendor/github.com/dchest $(GOBUILDDIR)/src/github.com/dchest
	@rm -f $(GOBUILDDIR)/src/github.com/dustin && ln -s ../../../vendor/github.com/dustin $(GOBUILDDIR)/src/github.com/dustin
	@rm -f $(GOBUILDDIR)/src/github.com/kballard && ln -s ../../../vendor/github.com/kballard $(GOBUILDDIR)/src/github.com/kballard
	@rm -f $(GOBUILDDIR)/src/github.com/shavac && ln -s ../../../vendor/github.com/shavac $(GOBUILDDIR)/src/github.com/shavac
	@rm -f $(GOBUILDDIR)/src/github.com/voxelbrain && ln -s ../../../vendor/github.com/voxelbrain $(GOBUILDDIR)/src/github.com/voxelbrain

$(BIN): $(GOBUILDDIR) $(SOURCES)
	@mkdir -p $(BINDIR)
	docker run \
		--rm \
		-v $(SRCDIR):/usr/code \
		-e GOPATH=/usr/code/.gobuild \
		-e GOOS=$(GOOS) \
		-e GOARCH=$(GOARCH) \
		-e CGO_ENABLED=0 \
		-w /usr/code/ \
		golang:$(GOVERSION) \
		go build -a -installsuffix netgo -tags netgo -ldflags "-X main.projectVersion=$(VERSION) -X main.projectBuild=$(COMMIT)" -o /usr/code/bin/$(GOOS)/$(GOARCH)/$(BINNAME) $(REPOPATH)

docker: build
	docker build -t arangodb/arangodb-starter .

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
	GOPATH=$(GOBUILDDIR) go build -o $(RELEASE) $(REPOPATH)/tools/release

$(GHRELEASE): $(GOBUILDDIR) 
	GOPATH=$(GOBUILDDIR) go build -o $(GHRELEASE) github.com/aktau/github-release

release-patch: $(RELEASE)
	GOPATH=$(GOBUILDDIR) $(RELEASE) -type=patch 

release-minor: $(RELEASE)
	GOPATH=$(GOBUILDDIR) $(RELEASE) -type=minor

release-major: $(RELEASE)
	GOPATH=$(GOBUILDDIR) $(RELEASE) -type=major 

TESTCONTAINER := arangodb-starter-test

test-images:
	docker pull $(ARANGODB)
	docker build --build-arg "from=$(ARANGODB)" -t arangodb-golang -f test/Dockerfile-arangodb-golang .

# Run all integration tests
run-tests: run-tests-local-process run-tests-docker

run-tests-local-process: build test-images
	@-docker rm -f -v $(TESTCONTAINER) &> /dev/null
	docker run \
		--rm \
		--name=$(TESTCONTAINER) \
		-v $(ROOTDIR):/usr/code \
		-e GOPATH=/usr/code/.gobuild \
		-e DATA_DIR=/tmp \
		-e STARTER=/usr/code/bin/linux/amd64/arangodb \
		-e TEST_MODES=localprocess \
		-e STARTER_MODES=$(STARTER_MODES) \
		-e ENTERPRISE=$(ENTERPRISE) \
		-e TESTOPTIONS=$(TESTOPTIONS) \
		-e DEBUG_CLUSTER=$(DEBUG_CLUSTER) \
		-w /usr/code/ \
		arangodb-golang \
		go test -timeout $(TEST_TIMEOUT) $(TESTOPTIONS) -v $(REPOPATH)/test

run-tests-docker: docker
	mkdir -p $(GOBUILDDIR)/tmp
	GOPATH=$(GOBUILDDIR) TMPDIR=$(GOBUILDDIR)/tmp TEST_MODES=docker STARTER_MODES=$(STARTER_MODES) ENTERPRISE=$(ENTERPRISE) IP=$(IP) ARANGODB=$(ARANGODB) go test -timeout $(TEST_TIMEOUT) $(TESTOPTIONS) -v $(REPOPATH)/test

# Run all integration tests on the local system
run-tests-local: local
	GOPATH=$(GOBUILDDIR) TEST_MODES="localprocess,docker" STARTER_MODES=$(STARTER_MODES) STARTER=$(ROOTDIR)/arangodb go test -timeout $(TEST_TIMEOUT) $(TESTOPTIONS) -v $(REPOPATH)/test
