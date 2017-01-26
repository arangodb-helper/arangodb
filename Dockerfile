FROM arangodb:latest
MAINTAINER Max Neunhoeffer <max@arangodb.com>

COPY bin/arangodb-linux-amd64 /app/

EXPOSE 4000 4001 4002 4003 4004
EXPOSE 5001 5002 5003 5004 5005
EXPOSE 8530 8531 8532 8533 8534
EXPOSE 8629 8630 8631 8632 8633

VOLUME /data

ENV DATA_DIR=/data 
ENV DOCKER_IMAGE=arangodb/arangodb:3.1

ENTRYPOINT ["/app/arangodb-linux-amd64"]
