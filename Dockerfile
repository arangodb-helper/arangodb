FROM alpine:3.4
MAINTAINER Max Neunhoeffer <max@arangodb.com>

COPY bin/linux/amd64/arangodb /app/

EXPOSE 4000 

VOLUME /data

# Data directory 
ENV DATA_DIR=/data 

# Signal running in docker 
ENV RUNNING_IN_DOCKER=true

# Docker image containing arangod.
ENV DOCKER_IMAGE=arangodb/arangodb:latest

ENTRYPOINT ["/app/arangodb"]
