-include .env

PKG = github.com/wodby/wodby-cli
APP = wodby

REPO = wodby/wodby-cli
NAME = wodby-cli

GOOS ?= linux
GOARCH ?= amd64

ifneq ($(VERSION),)
    TAG = $(VERSION)
else
    TAG = latest
endif

ifeq ($(GOOS),linux)
ifeq ($(GOARCH),amd64)
    LINUX_AMD64 = 1
endif
endif

LD_FLAGS = "-s -w -X $(PKG)/pkg/version.VERSION=$(VERSION)"

default: build

.PHONY: build test push shell package release

build:
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -ldflags $(LD_FLAGS) -o bin/$(GOOS)-$(GOARCH)/$(APP) $(PKG)/cmd/$(APP)

    ifeq ($(LINUX_AMD64),1)
	docker build -t $(REPO):$(TAG) ./
    endif

test:
	echo "OK"

push:
    ifeq ($(LINUX_AMD64),1)
	docker push $(REPO):$(TAG)
    endif

shell:
	docker run --rm --name $(NAME) $(PARAMS) -ti $(REPO):$(TAG) /bin/bash

package:
	tar czf bin/$(APP)-$(GOOS)-$(GOARCH).tar.gz -C bin/$(GOOS)-$(GOARCH) wodby

release: build push
