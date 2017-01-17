FROM arangodb:latest

# install go
RUN curl -Ok https://storage.googleapis.com/golang/go1.7.4.linux-amd64.tar.gz
RUN tar -xvf go1.7.4.linux-amd64.tar.gz
RUN mv go /usr/local
RUN mkdir go
RUN mkdir go/src/
RUN mkdir go/bin/
RUN mkdir go/pkg/

ENV GOPATH=/go/src
ENV PATH=$PATH:/usr/local/go/bin

# clean golang install
RUN rm go1.7.4.linux-amd64.tar.gz

# move arangodb
COPY arangodb/*.go go/src/arangodb/
WORKDIR /go/src/arangodb/
RUN go build .
RUN mv arangodb /
WORKDIR /

# exposing all ports
EXPOSE 4000
EXPOSE 4001
EXPOSE 8529

# reset entrypoint
ENTRYPOINT []
ENTRYPOINT ["/arangodb"]
