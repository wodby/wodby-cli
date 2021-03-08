FROM --platform=$BUILDPLATFORM golang:alpine as build

ARG VERSION
ARG TARGETOS
ARG TARGETARCH

WORKDIR $GOPATH/src/wodby/wodby-cli
COPY . .

ENV VERSION="${VERSION:-dev}"
ENV GOOS=$TARGETOS
ENV GOARCH=$TARGETARCH

RUN set -ex; \
    go build -ldflags "-s -w -X github.com/wodby/wodby-cli/pkg/version.VERSION=${VERSION}" -o /out/wodby github.com/wodby/wodby-cli/cmd/wodby; \
    /out/wodby version | grep $VERSION

FROM docker:20.10.3

COPY --from=build /out/wodby /usr/local/bin/wodby

RUN set -ex; \
    apk add --update bash git openssh-client; \
    rm -rf /var/cache/apk/*

CMD [ "wodby" ]