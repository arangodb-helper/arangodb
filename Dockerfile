ARG IMAGE=alpine:3.21
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
# Can be overridden via build arg ARANGODB_IMAGE
ARG ARANGODB_IMAGE=arangodb/arangodb:latest
ENV DOCKER_IMAGE=${ARANGODB_IMAGE}

ENTRYPOINT ["/app/arangodb"]
