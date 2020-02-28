FROM docker:stable-git

COPY ./bin/linux-amd64/wodby /usr/local/bin/wodby

CMD [ "wodby" ]
