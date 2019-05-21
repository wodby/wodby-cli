FROM docker:18.09.6-ce

COPY ./bin/linux-amd64/wodby /usr/local/bin/wodby

RUN set -ex; \
    \
    apk add --update bash git openssh-client; \
    \
    rm -rf /var/cache/apk/*

CMD [ "wodby" ]
