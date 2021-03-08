-include .env

PKG = github.com/wodby/wodby-cli

REPO = wodby/wodby-cli
NAME = wodby-cli

GOOS ?= linux
GOARCH ?= amd64
VERSION ?= 2.0
TAG ?= $(VERSION)

LD_FLAGS = "-s -w -X $(PKG)/pkg/version.VERSION=$(VERSION)"

PLATFORM ?= linux/amd64

default: build

.PHONY: build buildx-build buildx-push test shell package

build:
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -ldflags $(LD_FLAGS) -o bin/wodby $(PKG)/cmd/wodby

build-image:
	docker build -t $(REPO):$(TAG) ./

buildx-build:
	docker buildx build \
		--platform $(PLATFORM) \
		--build-arg VERSION=$(VERSION) \
		-t $(REPO):$(TAG) ./

buildx-push:
	docker buildx build \
		--platform $(PLATFORM) \
		--build-arg VERSION=$(VERSION) \
		--push \
		-t $(REPO):$(TAG) ./

test:
	@bin/wodby version | grep $(VERSION)

shell:
	docker run --rm --name $(NAME) $(PARAMS) -ti $(REPO):$(TAG) /bin/bash

package:
	mkdir -p dist
	tar cvzf dist/wodby-$(GOOS)-$(GOARCH).tar.gz -C bin wodby