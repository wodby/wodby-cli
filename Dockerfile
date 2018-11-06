FROM docker:18.06.1-ce

COPY ./bin/linux-amd64/wodby /usr/local/bin/wodby

RUN set -ex; \
    \
	addgroup -g 1000 -S wodby; \
	adduser -u 1000 -D -S -s /bin/bash -G wodby wodby; \
    \
    apk add --update bash git openssh-client; \
    \
    rm -rf /var/cache/apk/*

USER wodby

CMD [ "wodby" ]
