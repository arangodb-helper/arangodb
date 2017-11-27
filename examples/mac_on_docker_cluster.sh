#!/bin/sh
#
# This exampe shows how to run an ArangoDB cluster all locally in docker on mac.
# 
# By default this script uses the latest released ArangoDB docker image.
# To use another image, set ARANGOIMAGE to the desired image, before calling this script.
#
# Note: This script does not use a volume mapping, so data is lost after stopping the starter!
# If you want to persist data, add a volume mapping for /data.
#

NSCONTAINER=arangodb-on-mac-ns 
STARTERCONTAINER=arangodb-on-mac
ARANGOIMAGE=${ARANGOIMAGE:=arangodb/arangodb:latest}

# Create network namespace container.
# Make sure to expose all ports you want here.
docker run -d --name=${NSCONTAINER} \
    -p 8528:8528 -p 8529:8529 -p 8534:8534 -p 8539:8539 \
    alpine:latest sleep 630720000

# Run the starter in a container using the network namespace of ${NSCONTAINER}
docker run -it --name=${STARTERCONTAINER} --rm \
    --net=container:${NSCONTAINER} \
    -v /var/run/docker.sock:/var/run/docker.sock \
    arangodb/arangodb-starter \
    --docker.image=${ARANGOIMAGE} \
    --starter.address=localhost \
    --starter.local

# Remove namespace container 
docker rm -vf ${NSCONTAINER}