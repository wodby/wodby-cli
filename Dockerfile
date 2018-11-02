FROM docker:18.06.1-ce-dind

COPY ./bin/linux-amd64/wodby /usr/local/bin/wodby

RUN apk add --update bash git openssh-client

CMD [ "wodby" ]
