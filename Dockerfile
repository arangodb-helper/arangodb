MAINTAINER Max Neunhoeffer <max@arangodb.com>
FROM arangodb:latest
COPY arangodb/arangodb /
EXPOSE 4000
EXPOSE 4001
EXPOSE 8529
ENTRYPOINT ["/arangodb"]
