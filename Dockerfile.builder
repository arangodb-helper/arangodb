FROM arangodb:latest
MAINTAINER Max Neunhoeffer <max@arangodb.com>

# install go
WORKDIR /usr/local
RUN curl -Ok https://storage.googleapis.com/golang/go1.7.4.linux-amd64.tar.gz
RUN tar -xvf go1.7.4.linux-amd64.tar.gz
COPY buildInDocker.sh /

ENTRYPOINT ["/buildInDocker.sh"]
