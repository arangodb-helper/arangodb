#!/bin/sh

if [ ! -z "$(which apt-get)" ]; then
# Install for debian
	# gcc for cgo
	apt-get update && apt-get install -y --no-install-recommends \
			g++ \
			gcc \
			libc6-dev \
			make \
			pkg-config \
			wget \
			procps \
		&& rm -rf /var/lib/apt/lists/*

	set -eux; \
		\
	# this "case" statement is generated via "update.sh"
		dpkgArch="$(dpkg --print-architecture)"; \
		case "${dpkgArch##*-}" in \
			amd64) goRelArch='linux-amd64'; goRelSha256='72d820dec546752e5a8303b33b009079c15c2390ce76d67cf514991646c6127b' ;; \
			armhf) goRelArch='linux-armv6l'; goRelSha256='feca4e920d5ca25001dc0823390df79bc7ea5b5b8c03483e5a2c54f164654936' ;; \
			arm64) goRelArch='linux-arm64'; goRelSha256='1e07a159414b5090d31166d1a06ee501762076ef21140dcd54cdcbe4e68a9c9b' ;; \
			i386) goRelArch='linux-386'; goRelSha256='acbe19d56123549faf747b4f61b730008b185a0e2145d220527d2383627dfe69' ;; \
			ppc64el) goRelArch='linux-ppc64le'; goRelSha256='91d0026bbed601c4aad332473ed02f9a460b31437cbc6f2a37a88c0376fc3a65' ;; \
			s390x) goRelArch='linux-s390x'; goRelSha256='e211a5abdacf843e16ac33a309d554403beb63959f96f9db70051f303035434b' ;; \
			*) goRelArch='src'; goRelSha256='589449ff6c3ccbff1d391d4e7ab5bb5d5643a5a41a04c99315e55c16bbf73ddc'; \
				echo >&2; echo >&2 "warning: current architecture ($dpkgArch) does not have a corresponding Go binary release; will be building from source"; echo >&2 ;; \
		esac; \
		\
		url="https://golang.org/dl/go${GOLANG_VERSION}.${goRelArch}.tar.gz"; \
		wget -O go.tgz "$url"; \
		echo "${goRelSha256} *go.tgz" | sha256sum -c -; \
		tar -C /usr/local -xzf go.tgz; \
		rm go.tgz; \
		\
		if [ "$goRelArch" = 'src' ]; then \
			echo >&2; \
			echo >&2 'error: UNIMPLEMENTED'; \
			echo >&2 'TODO install golang-any from jessie-backports for GOROOT_BOOTSTRAP (and uninstall after build)'; \
			echo >&2; \
			exit 1; \
		fi; \
		\
		export PATH="/usr/local/go/bin:$PATH"; \
		go version
else
# Install for alpine
	set -eux; \
		apk add --no-cache --virtual .build-deps \
			bash \
			gcc \
			musl-dev \
			openssl \
			go \
		; \
		export \
	# set GOROOT_BOOTSTRAP such that we can actually build Go
			GOROOT_BOOTSTRAP="$(go env GOROOT)" \
	# ... and set "cross-building" related vars to the installed system's values so that we create a build targeting the proper arch
	# (for example, if our build host is GOARCH=amd64, but our build env/image is GOARCH=386, our build needs GOARCH=386)
			GOOS="$(go env GOOS)" \
			GOARCH="$(go env GOARCH)" \
			GOHOSTOS="$(go env GOHOSTOS)" \
			GOHOSTARCH="$(go env GOHOSTARCH)" \
		; \
	# also explicitly set GO386 and GOARM if appropriate
	# https://github.com/docker-library/golang/issues/184
		apkArch="$(apk --print-arch)"; \
		case "$apkArch" in \
			armhf) export GOARM='6' ;; \
			x86) export GO386='387' ;; \
		esac; \
		\
		wget -O go.tgz "https://golang.org/dl/go$GOLANG_VERSION.src.tar.gz"; \
		sha256sum *go.tgz ; \
		echo '4affc3e610cd8182c47abbc5b0c0e4e3c6a2b945b55aaa2ba952964ad9df1467 *go.tgz' | sha256sum -c -; \
		tar -C /usr/local -xzf go.tgz; \
		rm go.tgz; \
		\
		cd /usr/local/go/src; \
		for p in /go-alpine-patches/*.patch; do \
			[ -f "$p" ] || continue; \
			patch -p2 -i "$p"; \
		done; \
		./make.bash; \
		\
		rm -rf /go-alpine-patches; \
		apk del .build-deps; \
		\
		export PATH="/usr/local/go/bin:$PATH"; \
		go version
fi
