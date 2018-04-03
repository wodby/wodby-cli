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
        DOCKER_BIN = 1
    endif
endif

default: build

.PHONY: build docker-build test push shell release

build:
	@CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build \
			-installsuffix cgo -ldflags "-s -w -X $(PKG)/pkg/version.VERSION=$(VERSION)" \
			-o bin/$(GOOS)-$(GOARCH)/$(APP) \
			$(PKG)/cmd/$(APP)

test:
	echo "OK"

docker-build:
    ifeq ($(DOCKER_BIN),1)
	docker build -t $(REPO):$(TAG) ./
    endif

docker-push:
    ifeq ($(DOCKER_BIN),1)
	docker push $(REPO):$(TAG)
    endif

package:
	tar czf bin/$(APP)-$(GOOS)-$(GOARCH).tar.gz -C bin/$(GOOS)-$(GOARCH) wodby

release: build docker-build docker-push
