buildi:
	cd arangodb/ ; CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build .
	sudo docker build -t arangodb-starter .
