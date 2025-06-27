ARG IMAGE
FROM ${IMAGE}

ARG TARGETARCH
COPY bin/linux/${TARGETARCH}/arangodb /app/

EXPOSE 8528

VOLUME /data

# Data directory
ENV DATA_DIR=/data

# Signal running in docker
ENV RUNNING_IN_DOCKER=true

# Docker image containing arangod.
ENV DOCKER_IMAGE=arangodb/enterprise:latest

ENTRYPOINT ["/app/arangodb"]
