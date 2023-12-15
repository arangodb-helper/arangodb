# ArangoDB Starter Changelog

## [master](https://github.com/arangodb-helper/arangodb/tree/master) (N/A)
- Redirect HTTP server warnings into log file, skip TLS handshake errors
- Update setup.json config on relaunch with current enumeration of running servers

## [v0.17.2](https://github.com/arangodb-helper/arangodb/tree/0.17.2) (2023-11-23)
- Bump Go version (1.21.4) and dependencies for CVE fixes
- Use proper semver tags for releases

## [0.17.1](https://github.com/arangodb-helper/arangodb/tree/0.17.1) (2023-09-07)
- Retry server upgrade if requested by exit code (EXIT_UPGRADE_REQUIRED)
- Fix possible deadlock and goroutine leak while waiting for child process termination
- Change INF level to WRN or ERR when child process terminated with non-zero
- Use proper semver tags (vX.X.X) for releases

## [0.17.0](https://github.com/arangodb-helper/arangodb/tree/0.17.0) (2023-08-07)
- Update go to 1.20.7 to fix known CVEs

## [0.16.0](https://github.com/arangodb-helper/arangodb/tree/0.16.0) (2023-07-21)
- Add sanity check for pass-through args usage
- Fix printing --starter.instance-up-timeout instead of hardcoded value
- Fix context handling in WaitUntilStarterReady for tests
- Show error when user tries to change value of "persistent" option
- Proper leader election and cluster upgrade without using observe and TTL features

## [0.15.8](https://github.com/arangodb-helper/arangodb/tree/0.15.8) (2023-06-02)
- Add passing ARANGODB_SERVER_DIR env variable when starting arangod instances
- Improve ArangoDB version detection
- Update containerd package
- Update go to 1.20.3, add vulncheck to pipeline

## [0.15.7](https://github.com/arangodb-helper/arangodb/tree/0.15.7) (2023-02-17)
- Improve deprecated notice for old passthrough flags
- Improve detection of arangod binary when running local installation (use --server.use-local-bin)
- Upgrade base Alpine image and Go dependencies to fix CVEs
- Remove `netgo` build tag for Windows to disable new Go resolver behaviour: https://github.com/golang/go/issues/57757
- Add `rocksdb.encryption-key-generator` CLI option
- Add docker container name auto-detection when running under cgroups v2 unified hierarchy mode

## [0.15.6](https://github.com/arangodb-helper/arangodb/tree/0.15.6) (2023-01-20)
- Fix restarting cluster with arangosync enabled
- Upgrade Go to v1.19.4 to fix CVEs
- Fix passing-through boolean flags to arangosync

## [0.15.5](https://github.com/arangodb-helper/arangodb/tree/0.15.5) (2022-09-19)
- Fix project CI: linter, tests, build, release
- Add pass-through options usage hint
- Return correct exit code when wrong CLI params supplied
- Cleanup Travis config and make it use the list of actual ArangoD versions
- Add documentation header to generated command files
- Add support for ArangoD exit codes. Do not try to restart instance if exit code is unrecoverable
- Show process output in case there was an error during process startup
- Deprecate --feature.jwt.rotation CLI option
- Add support for config file
- Upgrade to Go 1.17.13 and alpine3.16 for build image
- Unpin x/sys pkg and upgrade dependencies to mitigate CVEs

## [0.15.4](https://github.com/arangodb-helper/arangodb/tree/0.15.4) (2022-03-22)
- Use github.com/golang-jwt/jwt
- Upgrade Go version to 1.17.5

## [0.15.3](https://github.com/arangodb-helper/arangodb/tree/0.15.3) (2021-10-25)
- Fix use of --args.all together with --args.<group> (<group> will override all flags in all)

## [0.15.2](https://github.com/arangodb-helper/arangodb/tree/0.15.2) (2021-09-03)
- Disable Maintenance Mode after upgrade, before health check

## [0.15.1](https://github.com/arangodb-helper/arangodb/tree/0.15.1) (2021-07-29)
- Allow to pass environment variables to processes and standardize argument pass (--envs.<group>.<ENV>=<VALUE> and --args.<group>.<ARG>=<VALUE>)
- Extend JWT Generator functionality by additional fields
- Do not run ResignLeadership when upgrading from <= 3.6.14 or 
  3.7 with <= 3.7.12.

# ArangoDB Starter Changelog Before 0.15.0

## Changes from 0.14.12 to 0.14.13

- Fixed upgrade for active failover when there are nodes without single server.
- Upgrade Go version to 1.13 for TLS 1.3 support

## Changes from 0.14.11 to 0.14.12

- Fixed wrong error message.

## Changes from 0.14.5 to 0.14.11

- Fixed race condition in Single Server upgrade

## Changes from 0.14.4 to 0.14.5

- Implement special upgrade procedure for upgrading from 3.4.6
- Move to alpine 3.9 for Docker image

## Changes from 0.14.3 to 0.14.4

- Make tests more stable
- Wait for killed processes

## Changes from 0.14.0 to 0.14.3

- Get system dependent information
- Gochache enabled
- Updated travis go to 1.12
- Process termination is either Info or Error
- implemented info for starter.address
- TLS documentation fixes

## Changes from 0.13.10 to 0.14.0

- Operation with TLS verified docker containers implemented
- JWT secret is handed to arangod containers using
  server.jwt-secret-keyfile
- Amend starter upgrade procedure for .tar.gz distributions
- Building of ARM64 executables
- Polish documentation

## Changes from version 0.13.9 to 0.13.10

- Implement that in Docker mode the ArangoDB license key is passed on
  to sub-containers.

## Changes from version 0.13.8 to 0.13.9

- Fix finding the storage engine if the master does not run a dbserver.
- Fix --dbservers.* passthrough option for active-failover setup.
- Polish documentation.
- Fix tests by increasing a timeout.

## Changes from version 0.13.7 to 0.13.8

- Redo previous release because github was offline. No other changes.

## Changes from version 0.13.6 to 0.13.7

- Do not use --javascript.copy-installation for 3.3.19.

## Changes from version 0.13.5 to 0.13.6

- Do not use --javascript.copy-installation for 3.3.18 any more.
- Increase timeout when checking instances for readiness and make it
  configurable.

## Changes from version 0.13.3 to 0.13.5

- Starter cluster configuration no longer written to `setup.json` when
  it has not changed.
- Added advertised endpoint to coordinators and active failover servers.
- Give option --javascript.copy-installation to versions of ArangoDB
  which have it.

## Changes from version 0.13.2 to 0.13.3

- Fixed a bug in the RECOVERY procedure which was declined if a new
  address was used for the replacement starter.
- Add retries to make active/failover tests more stable.
- Updated go-drivers and go-certificates libraries.

## Changes from version 0.13.1 to 0.13.2

- Fixed potential error in database version check when running
  _Starter_ on docker.

## Changes from version 0.13.0 to 0.13.1

- Fixed dangling container used to check database version when running
  _Starter_ on docker.

## Changes from version 0.12.0 to 0.13.0

- Database upgrade procedure has changed.
  It is now much more automated and can be triggered using
  an `arangodb upgrade --starter.endpoint=...` command.
  See [Upgrading _Starter_ Deployments](https://www.arangodb.com/docs/stable/upgrading-starter.html)
  for details.
- Added `arangodb remove starter` command to remove a machine
  from a cluster in a controlled manor.
  See [ArangoDB Starter Removal Procedure](https://www.arangodb.com/docs/stable/administration-starter-removal.html)
  for details.
- Changed default value of `--server.storage-engine` from `mmfiles`
  to an empty string. For deployments using ArangoDB 3.3 or earlier
  the effective default is still `mmfiles`. For deployments using ArangoDB 3.4
  or higher, `rocksdb` is the default storage engine.

## Changes from version 0.11.3 to 0.12.0

- Starter now writes it log output to file, unless you set the `--log.file` option to `false`.
  In this change, a new logging component is used, that results in some changes in the
  way the log messages appear in the log output. Also coloring of the logs has been slightly
  changed.
  Other new command line options are `--log.console=<bool>` to enable/disable logging
  to the standard output and `--log.color=<bool>` to enable/disable coloring
  log output. By default log output is using color when there is a terminal attached
  to the standard input of the process and the OS is not Windows.
- Fixed `arangodb start` command. (#112)

## Changes from version 0.11.2 to 0.11.3

- Solved problem with agency supervision mode in database auto-upgrade API.

## Changes from version 0.11.1 to 0.11.2

- Solved problem connection to agency when using TLS & authentication.
- Changed TLS algorithm for `-ssl.auto-key` from RSA (2048 bits) to ECDSA (`P256` curve).
- Changed default ECDSA curve used by `arangodb create tls ...` from `P521` to `P256`.
- Log text showing the address the starter is listening on has been changed from "Listening on ..." to "ArangoDB Starter listening on ...".
- Solved problem where starter did not properly log a resilientsingle server ("Your resilient single server can now be accessed ...") when leadership challenge was still ongoing.

## Changes from version 0.11.0 to 0.11.1

- Fix port binding check when using `--starter.host`. It used to check against the `any` interface, now it checks against `--starter.host`.

## Changes from version 0.10.4 to 0.11.0

- For `activefailover` deployments, the message where to reach your database is now only shown for the current leader.
- Added SystemD example. See `examples/systemd/README.md`.
- Added `--log.dir` option to configure a custom directory to which all log files will be written.
- It is no longer allowed to use `log.file` as a passthrough option.
- Added `--starter.host` option, to bind the HTTP server to a specific network interface instead of the default `0.0.0.0`.
- Added `POST /database-auto-upgrade` support to perform a rolling upgrade of all servers (with single `--database.auto-upgrade` restart)
- Renamed mode option `resilientsingle` to `activefailover`. (`resilientsingle` is being supported as alias for a while)
- Added support for log file rotation for started server components.
- Added support for running datacenter to datacenter replication servers (`arangosync`) from the starter.
- Changed increment for TCP ports (used when running multiple starters on same machine (e.g `--starter.local`)) from 5 to 10.
  An increment of 10 is needed to run datacenter to datacenter replication servers.
  If you have an existing cluster that is using `--starter.local` and you also want to enable datacenter
  to datacenter replication, you must create a new cluster.
- Added support for environment variables to act as commandline arguments.
  E.g. `ARANGODB_STARTER_DATA_DIR=/tmp/foo arangodb` equals `arangodb --starter.data-dir=/tmp/foo`.

## Changes from version 0.10.3 to 0.10.4

- Using shard `http.Client` to reduce the number of used file descriptors.

## Changes from version 0.10.2 to 0.10.3

- Support building in a directory other than the source directory. Set `BUILDDIR` (#98).
- Testing a server instance now includes testing for the expected server role.
- On linux, also look for `arangod` in `/usr/local/sbin` (#93).
- Fixed potential for hang in starter behavior.

## Changes from version 0.10.1 to 0.10.2

- Starting with mode `resilientsingle` and `--starter.local` will no longer limit
  the number of single servers to 2.

## Changes from version 0.10.0 to 0.10.1

- Remove `server.threads` & `javascript.v8-contexts` from generated `arangod.conf` files.
  Both settings are nolonger needed.

## Changes from version 0.9.3 to 0.10.0

- Added support for `resilientsingle` mode. A configuration of 2 single servers that replicate and take over when needed.
- Removed `--cluster.my-local-info` option from commandline of `arangod` servers. It is obsolete.
- Fixed combination of `--starter.local` & `--cluster.agency-size=1` (do not use for production!)

## Changes from version 0.9.2 to 0.9.3

- Added `--version` option and `version` command to show version of the starter.

## Changes from version 0.9.1 to 0.9.2

- Added `--starter.disable-ipv6` option to cope with environments
  where IPv6 is actively disabled.

## Changes from version 0.9.0 to 0.9.1

- Update to go 1.9.0
- Fixed registration for callback (in agency) by unreachable local slaves.
- Fixed port allocation in case of using `--starter.address=127.0.0.1` with `--starter.local` (#79)

## Changes from version 0.8.2 to 0.9.0

- Added `--docker.imagePullPolicy` option
- Allow multiple `--starter.join` arguments.
- Fixed high CPU load (#75)

## Changes from version 0.8.1 to 0.8.2

- Avoid using port offsets when using different `--starter.port`s that cause no overlap of port ranges.
- Cluster configuration is updated to all starters at regular intervals after
  the starters have bootstrapped and reached a running state.
- After starters have bootstrapped, they elect a starter to be master over the cluster configuration.
  All changes (addition/removal) are forwarded to this master.
  When the master is gone for too long, a new master is elected.

## Changes from version 0.8.0 to 0.8.1

- Fixed cluster setup in case where starters use different `--starter.port`s (#68).
- The `--rocksdb.encryption-keyfile` is now passed through the database servers in the `arangod.conf` file (it was passed as command line argument before).
  If you use this setting in an existing cluster, make sure the manually add this setting to all `arangod.conf` files before restarting the starters.
- Added `--starter.debug-cluster` option that adds a trail of status codes to the log when starting servers. (intended mostly for internal testing)
- Made database image used in test configurable using `ARANGODB` make variable.
- Added `--docker.tty` option for controlling the TTY flag of started docker containers.
- In cluster mode the minimum agency size has been lowered to 1 (DO NOT USE IN PRODUCTION).

## Changes from version 0.7.2 to 0.8.0

- Added `start` command to run starter in detached mode.
- Added `stop` command to stop a running starter using its HTTP API.

## Changes from version 0.7.1 to 0.7.2

- Added path containing starter executable to search path for `arangod`.

## Changes from version 0.7.0 to 0.7.1

- Added `--rocksdb.encryption-keyfile` option.
- Added pass through options. See README.
- Changed `--data.dir` option to `--starter.data-dir`

## Changes from version 0.6.0 to 0.7.0

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

## Changes from version 0.4.1 to 0.5.0

- Added authentication support (#10)
- Added SSL support (#11)
- Fixed various IPv6 issues (#13)
- Attach starter to existing server processes (#6)
- Use same port offsets on peers running on different machines (#3)
