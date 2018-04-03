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
	docker build -t $(REPO):$(TAG) ./

docker-push:
	docker push $(REPO):$(TAG)

package:
	tar czf bin/$(APP)-$(GOOS)-$(GOARCH).tar.gz -C bin/$(GOOS)-$(GOARCH) wodby

release: build docker-build docker-push
