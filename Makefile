buildi:
	cd arangodb/ ; CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build .
	sudo docker build -t arangodb-starter .

buildExecutableForDocker:
	sudo docker build -t arangodb-starter-builder -f Dockerfile.builder .
	sudo docker run --rm --user=`id -u`:`id -g` -v `pwd`/arangodb:/arangodb arangodb-starter-builder
