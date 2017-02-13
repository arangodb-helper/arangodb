PROJECT := ArangoDBStarter
SCRIPTDIR := $(shell pwd)
ROOTDIR := $(shell cd $(SCRIPTDIR) && pwd)
VERSION:= $(shell cat $(ROOTDIR)/VERSION)
COMMIT := $(shell git rev-parse --short HEAD)
DOCKERCLI := $(shell which docker)

GOBUILDDIR := $(SCRIPTDIR)/.gobuild
SRCDIR := $(SCRIPTDIR)
BINDIR := $(ROOTDIR)/bin

ORGPATH := github.com/neunhoef
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

ifndef DOCKERNAMESPACE
	DOCKERNAMESPACE := arangodb
endif

BINNAME := arangodb-$(GOOS)-$(GOARCH)
BIN := $(BINDIR)/$(BINNAME)

SOURCES := $(shell find $(SRCDIR) -name '*.go')

.PHONY: all clean deps docker build build-local

all: build

clean:
	rm -Rf $(BIN) $(GOBUILDDIR)

local:
ifneq ("$(DOCKERCLI)", "")
	@${MAKE} -B GOOS=$(shell go env GOHOSTOS) GOARCH=$(shell go env GOHOSTARCH) build-local
else
	@${MAKE} deps
	GOPATH=$(GOBUILDDIR) go build -o arangodb main.go
endif

build: $(BIN)

build-local: build 
	@ln -sf $(BIN) $(ROOTDIR)/arangodb

deps:
	@${MAKE} -B -s $(GOBUILDDIR)

$(GOBUILDDIR):
	@mkdir -p $(ORGDIR)
	@rm -f $(REPODIR) && ln -s ../../../.. $(REPODIR)

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
		go build -a -installsuffix netgo -tags netgo -ldflags "-X main.projectVersion=$(VERSION) -X main.projectBuild=$(COMMIT)" -o /usr/code/bin/$(BINNAME) $(REPOPATH)

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

release-patch: $(GOBUILDDIR)
	go run ./tools/release.go -type=patch 

release-minor: $(GOBUILDDIR)
	go run ./tools/release.go -type=minor

release-major: $(GOBUILDDIR)
	go run ./tools/release.go -type=major 
