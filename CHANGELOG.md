# Changes from version 0.7.0 to master

- Added pass through options. See README.
- Changed `--data.dir` option to `--starter.data-dir`

# Changes from version 0.6.0 to 0.7.0

- Added `--server.storage-engine` option, used to change the storage engine of the `arangod` instances (#48)
- Changed option naming scheme (see `arangodb --help` for all new names). Old names are still accepted.
- Renamed github repository from `github.com/arangodb-helper/ArangoDBStarter` to `github.com/arangodb-helper/arangodb`.
- When an `--ssl.keyfile` (or `--ssl.auto-key`) argument is given, the starter will serve it's API over TLS using the same certificate as the database server(s).
- Starter will detect the name of the docker container is it running in automatically (if running in docker and not set using `--docker.container`)
- Changed default master port from 4000 to 8528. That results in a coordinator/single server to be available on well known port 8529
- Added `--starter.mode=single` argument, used to start a single server database instead of a cluster (#28)
- Starter will check availability of TCP ports (both its own HTTP API & Arangod Servers) (#35)
- Docker container created by the started are given the label `created-by=arangodb-starter`
- Added `--starter.local` argument, used to start a local test cluster in a single starter process (#25)
- When an `arangod` server stops quickly and often, its most recent log output is shown
- Support `~` (home directory) in path arguments. E.g. `--data.dir=~/mydata/` (#30)
- Changed port offsets of servers. Coordinator -> 1, DBServer -> 2, Agent -> 3.

# Changes from version 0.4.1 to 0.5.0

- Added authentication support (#10)
- Added SSL support (#11)
- Fixed various IPv6 issues (#13)
- Attach starter to existing server processes (#6)
- Use same port offsets on peers running on different machines (#3)
