PROJECT := ArangoDBStarter
SCRIPTDIR := $(shell pwd)
ROOTDIR := $(shell cd $(SCRIPTDIR) && pwd)
VERSION:= $(shell cat $(ROOTDIR)/VERSION)
COMMIT := $(shell git rev-parse --short HEAD)
DOCKERCLI := $(shell which docker)

GOBUILDDIR := $(SCRIPTDIR)/.gobuild
SRCDIR := $(SCRIPTDIR)
BINDIR := $(ROOTDIR)/bin

ORGPATH := github.com/arangodb
ORGDIR := $(GOBUILDDIR)/src/$(ORGPATH)
REPONAME := $(PROJECT)
REPODIR := $(ORGDIR)/$(REPONAME)
REPOPATH := $(ORGPATH)/$(REPONAME)

GOPATH := $(GOBUILDDIR)
GOVERSION := 1.7.4-alpine

ifndef GOOS
	GOOS := linux
endif
ifndef GOARCH
	GOARCH := amd64
endif
ifeq ("$(GOOS)", "windows")
	GOEXE := .exe
endif

ifndef DOCKERNAMESPACE
	DOCKERNAMESPACE := arangodb
endif

BINNAME := arangodb$(GOEXE)
BIN := $(BINDIR)/$(GOOS)/$(GOARCH)/$(BINNAME)
RELEASE := $(GOBUILDDIR)/bin/release 
GHRELEASE := $(GOBUILDDIR)/bin/github-release 

SOURCES := $(shell find $(SRCDIR) -name '*.go')

.PHONY: all clean deps docker build build-local

all: build

clean:
	rm -Rf $(BIN) $(BINDIR) $(GOBUILDDIR) $(ROOTDIR)/arangodb

local:
ifneq ("$(DOCKERCLI)", "")
	@${MAKE} -B GOOS=$(shell go env GOHOSTOS) GOARCH=$(shell go env GOHOSTARCH) build-local
else
	@${MAKE} deps
	GOPATH=$(GOBUILDDIR) go build -o arangodb $(REPOPATH)
endif

build: $(BIN)

build-local: build 
	@ln -sf $(BIN) $(ROOTDIR)/arangodb

binaries: 
	@${MAKE} -B GOOS=linux GOARCH=amd64 build
	@${MAKE} -B GOOS=darwin GOARCH=amd64 build
	@${MAKE} -B GOOS=windows GOARCH=amd64 build

deps:
	@${MAKE} -B -s $(GOBUILDDIR)

$(GOBUILDDIR):
	@mkdir -p $(ORGDIR)
	@rm -f $(REPODIR) && ln -s ../../../.. $(REPODIR)
	@rm -f $(GOBUILDDIR)/src/github.com/aktau && ln -s ../../../vendor/github.com/aktau $(GOBUILDDIR)/src/github.com/aktau
	@rm -f $(GOBUILDDIR)/src/github.com/dustin && ln -s ../../../vendor/github.com/dustin $(GOBUILDDIR)/src/github.com/dustin
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
	docker push arangodb/arangodb-starter:$(VERSION)

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
