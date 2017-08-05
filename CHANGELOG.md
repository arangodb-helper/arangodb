# Changes from version 0.8.2 to master 

- Added `--docker.pull` option

# Changes from version 0.8.1 to master 

- Avoid using port offsets when using different `--starter.port`s that cause no overlap of port ranges.
- Cluster configuration is updated to all starters at regular intervals after 
  the starters have bootstrapped and reached a running state.
- After starters have bootstrapped, they elect a starter to be master over the cluster configuration.
  All changes (addition/removal) are forwarded to this master. 
  When the master is gone for too long, a new master is elected.

# Changes from version 0.8.0 to master 

- Fixed cluster setup in case where starters use different `--starter.port`s (#68).
- The `--rocksdb.encryption-keyfile` is now passed through the database servers in the `arangod.conf` file (it was passed as command line argument before).
  If you use this setting in an existing cluster, make sure the manually add this setting to all `arangod.conf` files before restarting the starters.
- Added `--starter.debug-cluster` option that adds a trail of status codes to the log when starting servers. (intended mostly for internal testing)
- Made database image used in test configurable using `ARANGODB` make variable.
- Added `--docker.tty` option for controlling the TTY flag of started docker containers.
- In cluster mode the minimum agency size has been lowered to 1 (DO NOT USE IN PRODUCTION). 

# Changes from version 0.7.2 to 0.8.0

- Added `start` command to run starter in detached mode.
- Added `stop` command to stop a running starter using its HTTP API.

# Changes from version 0.7.1 to 0.7.2

- Added path containing starter executable to search path for `arangod`.

# Changes from version 0.7.0 to 0.7.1

- Added `--rocksdb.encryption-keyfile` option.
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
