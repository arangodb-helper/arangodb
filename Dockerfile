FROM arangodb:latest
MAINTAINER Max Neunhoeffer <max@arangodb.com>

COPY bin/arangodb-linux-amd64 /app/

EXPOSE 4000 

VOLUME /data

ENV DATA_DIR=/data 
ENV DOCKER_IMAGE=arangodb/arangodb:3.1.9

ENTRYPOINT ["/app/arangodb-linux-amd64"]
