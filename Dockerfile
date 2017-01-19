FROM arangodb:latest
MAINTAINER Max Neunhoeffer <max@arangodb.com>
COPY arangodb/arangodb /
EXPOSE 4000 4001 4002 4003 8530 8531 8532 8629 8630 8631
ENTRYPOINT ["/arangodb"]
