FROM wodby/alpine:3.7-2.0.0

COPY ./bin/linux-amd64/wodby /usr/local/bin/wodby

RUN apk add --update bash docker

CMD [ "wodby" ]
