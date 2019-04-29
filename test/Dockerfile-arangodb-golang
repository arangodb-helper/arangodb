ARG from
FROM ${from}

RUN uname -a

# If you change this, make matching changes in docker_install_go.sh
# to reflect the hashes.
ENV GOLANG_VERSION 1.12.4

ADD test/docker_install_go.sh /bin
RUN /bin/docker_install_go.sh

ENV GOPATH /go
ENV PATH $GOPATH/bin:/usr/local/go/bin:$PATH

RUN mkdir -p "$GOPATH/src" "$GOPATH/bin" && chmod -R 777 "$GOPATH"
