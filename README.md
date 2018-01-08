Starting an ArangoDB cluster the easy way
=========================================

[![GoDoc](https://godoc.org/github.com/arangodb-helper/arangodb/client?status.svg)](http://godoc.org/github.com/arangodb-helper/arangodb/client)

Downloading Releases
--------------------
You can download precompiled `arangodb` binaries via [the github releases page](https://github.com/arangodb-helper/arangodb/releases).

Building
--------

If you want to compile `arangodb` yourselves just do:

```
go get -u github.com/arangodb-helper/arangodb
```

This will result in a binary at `$GOPATH/bin/arangodb`.

For more advanced build options, clone this repository and do:

```
make local
```

and the executable is in `./bin` named after the current OS & architecture (e.g. `arangodb-linux-amd64`).
You can copy the binary anywhere in your PATH.
A link to the binary for the local OS & architecture is made to `./arangodb`.
This program will run on Linux, OSX or Windows.

Note: The standard build uses a docker container to run the build. If docker is not available 
`make local` runs the [go compiler](https://golang.org/) directly and places the binary directly in the project directory.
In this case you need to install the `golang` package on your system (version 1.7 or higher).

Starting a cluster
------------------

Install ArangoDB in the usual way as binary package. Then:

On host A:

```
arangodb
```

This will use port 8528 to wait for colleagues (3 are needed for a
resilient agency). On host B: (can be the same as A):

```
arangodb --starter.join A
```

This will contact A on port 8528 and register. On host C: (can be same
as A or B):

```
arangodb --starter.join A
```

This will contact A on port 8528 and register.

From the moment on when 3 have joined, each will fire up an agent, a 
coordinator and a dbserver and the cluster is up. Ports are shown on
the console, the starter uses the next few ports above the starter
port. That is, if one uses port 8528 for the starter, the coordinator
will use 8529 (=8528+1), the dbserver 8530 (=8528+2), and the agent 8531
(=8528+3). See below under `--starter.port` for how to change the
starter default port.

Additional servers can be added in the same way.

If two or more of the `arangodb` instances run on the same machine,
one has to use the `--starter.data-dir` option to let each use a different
directory.

The `arangodb` program will find the ArangoDB executable and the
other installation files automatically. If this fails, use the
`--server.arangod` and `--server.js-dir` options described below.

Running in Docker 
-----------------
You can run `arangodb` using our ready made docker container. 

When using `arangodb` in a Docker container it will also run all 
servers in a docker using the `arangodb/arangodb:latest` docker image.
If you wish to run a specific docker image for the servers, specify it using
the `--docker.image` argument.

When running in docker it is important to care about the volume mappings on 
the container. Typically you will start the executable in docker with the following
commands.

```
export IP=<IP of docker host>
docker volume create arangodb1
docker run -it --name=adb1 --rm -p 8528:8528 \
    -v arangodb1:/data \
    -v /var/run/docker.sock:/var/run/docker.sock \
    arangodb/arangodb-starter \
    --starter.address=$IP
```

The executable will show the commands needed to run the other instances.

Note that the commands above create a docker volume. If you're running on Linux 
it is also possible to use a host mapped volume. Make sure to map it on `/data`.

If you want to create the `arangodb/arangodb-starter` docker container yourselves
you can build it using:

```
make docker 
```

Using multiple join arguments
-----------------------------

It is allowed to use multiple `--starter.join` arguments. 
This eases scripting.

For example:

On host A:

```
arangodb --starter.join A,B,C
```

On host B:

```
arangodb --starter.join A,B,C
```

On host C:

```
arangodb --starter.join A,B,C
```

This starts a cluster where the starter on host A is chosen to be master during the bootstrap phase.

Note: `arangodb --starter.join A,B,C` is equal to `arangodb --starter.join A --starter.join B --starter.join C`.

During the bootstrap phase of the cluster, the starters will all choose the "master" starter 
based on list of given `starter.join` arguments.

The "master" starter is chosen as follows:

- If there are no `starter.join` arguments, the starter becomes a master.
- If there are multiple `starter.join` arguments, these arguments are sorted. If a starter is the first 
  in this sorted list, it becomes a starter.
- In all other cases, the starter becomes a slave.

Note: Once the bootstrap phase is over (all arangod servers have started and are running), the bootstrap 
phase ends and the starters use the Arango agency to elect a master for the runtime phase.

Starting a local test cluster
-----------------------------

If you want to start a local cluster quickly, use the `--starter.local` flag. 
It will start all servers within the context of a single starter process.

```
arangodb --starter.local
```

Note: When you restart the started, it remembers the original `--starter.local` flag.

Starting a single server
------------------------

If you want to start a single database server, use `--starter.mode=single`.

```
arangodb --starter.mode=single
```

Starting a single server in Docker
----------------------------------

If you want to start a single database server running in a docker container,
use the normal docker arguments, combined with `--starter.mode=single`.

```
export IP=<IP of docker host>
docker volume create arangodb
docker run -it --name=adb --rm -p 8528:8528 \
    -v arangodb:/data \
    -v /var/run/docker.sock:/var/run/docker.sock \
    arangodb/arangodb-starter \
    --starter.address=$IP \
    --starter.mode=single
```

Starting a resilient single server pair
---------------------------------------

If you want to start a resilient single database server, use `--starter.mode=resilientsingle`.
In this mode a 3 machine agency is started and 2 single servers that perform 
asynchronous replication an failover if needed.

```
arangodb --starter.mode=resilientsingle --starter.join A,B,C
```

Run this on machine A, B & C.

The starter will decide on which 2 machines to run a single server instance.
To override this decision (only valid while bootstrapping), add a 
`--cluster.start-single=false` to the machine where the single server 
instance should NOT be scheduled.

Starting a resilient single server pair in Docker
-------------------------------------------------

If you want to start a resilient single database server running in docker containers,
use the normal docker arguments, combined with `--starter.mode=resilientsingle`.

```
export IP=<IP of docker host>
docker volume create arangodb
docker run -it --name=adb --rm -p 8528:8528 \
    -v arangodb:/data \
    -v /var/run/docker.sock:/var/run/docker.sock \
    arangodb/arangodb-starter \
    --starter.address=$IP \
    --starter.mode=resilientsingle \
    --starter.join=A,B,C
```

Run this on machine A, B & C.

The starter will decide on which 2 machines to run a single server instance.
To override this decision (only valid while bootstrapping), add a 
`--cluster.start-single=false` to the machine where the single server 
instance should NOT be scheduled.

Starting a local test resilient single sever pair
-------------------------------------------------

If you want to start a local resilient server pair quickly, use the `--starter.local` flag. 
It will start all servers within the context of a single starter process.

```
arangodb --starter.local --starter.mode=resilientsingle
```

Note: When you restart the started, it remembers the original `--starter.local` flag.


Starting & stopping in detached mode 
------------------------------------

If you want the starter to detach and run as a background process, use the `start` 
command. This is typically used by developers running tests only.

```
arangodb start --starter.local=true [--starter.wait]
```

This command will make the starter run another starter process in the background 
(that starts all ArangoDB servers), wait for it's HTTP API to be available and
then exit. The starter that was started in the background will keep running until you stop it.

The `--starter.wait` option makes the `start` command wait until all ArangoDB server 
are really up, before ending the master process.

To stop a starter use this command.

```
arangodb stop
```

Make sure to match the arguments given to start the starter (`--starter.port` & `--ssl.*`).

Common options 
--------------

* `--starter.data-dir=path`

`path` is the directory in which all data is stored. (default "./")

In the directory, there will be a single file `setup.json` used for
restarts and a directory for each instances that runs on this machine.
Different instances of `arangodb` must use different data directories.

* `--starter.join=address`

Join a cluster with master at address `address` (default "").
Address can be an host address or name, followed with an optional port.

E.g. these are valid arguments.
```
--starter.join=localhost 
--starter.join=localhost:5678
--starter.join=192.168.23.1:8528
--starter.join=192.168.23.1
```

* `--starter.local` 

Start a local (test) cluster. Since all servers are running on a single machine 
this is really not intended for production setups.

* `--starter.mode=cluster|single|resilientsingle`

Select what kind of database configuration you want. 
This can be a `cluster` configuration (which is the default), 
a `single` server configuration or a `resilientsingle` configuration with 
2 single services configured to take over when needed.

Note that when running a `single` server configuration you will lose all 
high availability features that a cluster provides you.

* `--cluster.agency-size=int`

number of agents in agency (default 3).

This number has to be positive and odd, and anything beyond 5 probably
does not make sense. The default 3 allows for the failure of one agent.

* `--starter.address=addr`

`addr` is the address under which this server is reachable from the
outside.

Usually, this option does not have to be specified. Only in the case
that `--cluster.agency-size` is set to 1 (see below), the master has to know
under which address it can be reached from the outside. If you specify
`localhost` here, then all instances must run on the local machine.

* `--docker.image=image`

`image` is the name of a Docker image to run instead of the normal
executable. For each started instance a Docker container is launched.
Usually one would use the Docker image `arangodb/arangodb`.

* `--docker.container=containerName`

`containerName` is the name of a Docker container that is used to run the
executable. If you do not provide this argument but run the starter inside 
a docker container, the starter will auto-detect its container name.

Authentication options
----------------------

The arango starter by default creates a cluster that uses no authentication.

To create a cluster that uses authentication, create a file containing a random JWT secret (single line)
and pass it through the `--auth.jwt-secret-path` option.

For example:

```
echo "MakeThisSecretMuchStronger" > jwtSecret 
arangodb --auth.jwt-secret=./jwtSecret
```

All starters used in the cluster must have the same JWT secret.

SSL options
-----------

The arango starter by default creates a cluster that uses no unencrypted connections (no SSL).

To create a cluster that uses encrypted connections, you can use an existing server key file (.pem format)
or let the starter create one for you.

To use an existing server key file use the `--ssl.keyfile` option like this:

```
arangodb --ssl.keyfile=myServer.pem
```

Go to the [SSL manual](https://docs.arangodb.com/3.1/Manual/Administration/Configuration/SSL.html) for more
information on how to create a server key file.

To let the starter created a self-signed server key file, use the `--ssl.auto-key` option like this:

```
arangodb --ssl.auto-key
```

All starters used to make a cluster must be using SSL or not.
You cannot have one starter using SSL and another not using SSL.

If you start a starter using SSL, it's own HTTP server (see API) will also
use SSL.

Note that all starters can use different server key files.

Additional SSL options:

* `--ssl.cafile=path`

Configure the servers to require a client certificate in their communication to the servers using the CA certificate in a file with given path.

* `--ssl.auto-server-name=name` 

name of the server that will be used in the self-signed certificate created by the `--ssl.auto-key` option.

* `--ssl.auto-organization=name` 

name of the server that will be used in the self-signed certificate created by the `--ssl.auto-key` option.

Other database options
----------------------

Options for `arangod` that are not supported by the starter can still be passed to
the database servers using a pass through option.
Every option that start with a pass through prefix is passed through to the commandline
of one or more server instances.

* `--all.<section>.<key>=<value>` is pass as `--<section>.<key>=<value>` to all servers started by this starter.
* `--coordinators.<section>.<key>=<value>` is passed as `--<section>.<key>=<value>` to all coordinators started by this starter.
* `--dbservers.<section>.<key>=<value>` is passed as `--<section>.<key>=<value>` to all dbservers started by this starter.
* `--agents.<section>.<key>=<value>` is passed as `--<section>.<key>=<value>` to all agents started by this starter.

Some options are essential to the function of the starter. Therefore these options cannot be passed through like this.

Example:

To activate HTTP request logging at debug level for all coordinators, use a command like this.

```
arangodb --coordinators.log.level=requests=debug
```

Esoteric options
----------------

* `--version`

show the version of the starter.

* `--starter.port=int`

port for arangodb master (default 8528). See below under "Technical
explanation as to what happens" for a description of how the ports of
the other servers are derived from this number.

This is the port used for communication of the `arangodb` instances
amongst each other.

* `--starter.disable-ipv6=bool` 

if disabled, the starter will configure the `arangod` servers 
to bind to address `0.0.0.0` (all IPv4 interfaces) 
instead of binding to `[::]` (all IPv4 and all IPv6 interfaces).

This is useful when IPv6 has actively been disabled on your machine.

* `--server.arangod=path`

path to the `arangod` executable (default varies from platform to
platform, an executable is searched in various places).

This option only has to be specified if the standard search fails.

* `--server.js-dir=path`

path to JS library directory (default varies from platform to platform,
this is coupled to the search for the executable).

This option only has to be specified if the standard search fails.

* `--server.storage-engine=mmfiles|rocksdb` 

Sets the storage engine used by the `arangod` servers. 
The value `rocksdb` is only allowed on `arangod` version 3.2 and up.

* `--cluster.start-coordinator=bool`

This indicates whether or not a coordinator instance should be started 
(default true).

* `--cluster.start-dbserver=bool`

This indicates whether or not a DB server instance should be started 
(default true).

* `--server.rr=path`

path to rr executable to use if non-empty (default ""). Expert and
debugging only.

* `--log.verbose=bool`

show more information (default false).

* `--log.rotate-files-to-keep=int`

set the number of old log files to keep when rotating log files of server components.

* `--log.rotate-interval=duration`

set the interval between rotations of log files of server components (default `24h`).
Use a value of `0` to disable automatic log rotation.

Note: The starter will always perform log rotation when it receives a `HUP` signal.

* `--starter.unique-port-offsets=bool`

If set to true, all port offsets (of slaves) will be made globally unique.
By default (value is false), port offsets will be unique per slave address.

* `--docker.user=user`

`user` is an expression to be used for `docker run` with the `--user` 
option. One can give a user id or a user id and a group id, separated
by a colon. The purpose of this option is to limit the access rights
of the process in the Docker container.

* `--docker.endpoint=endpoint`

`endpoint` is the URL used to reach the docker host. This is needed to run 
the executable in docker. The default value is "unix:///var/run/docker.sock".

* `--docker.imagePullPolicy=Always|IfNotPresent|Never` 

`docker.imagePullPolicy` determines if the docker image is being pull from the docker hub.
If set to `Always`, the image is always pulled and an error causes the starter to fail.
If set to `IfNotPresent`, the image is not pull if it is always available locally.
If set to `Never`, the image is never pulled (when it is not available locally an error occurs).
The default value is `Always` is the `docker.image` has the `:latest` tag or `IfNotPresent` otherwise.

* `--docker.net-mode=mode`

If `docker.net-mode` is set, all docker container will be started 
with the `--net=<mode>` option.

* `--docker.privileged=bool`

If `docker.privileged` is set, all docker containers will be started 
with the `--privileged` option turned on.

* `--docker.tty=bool` 

If `docker.tty` is set, all docker containers will be started with a TTY.
If the starter itself is running in a docker container without a TTY 
this option is overwritten to `false`.

* `--starter.debug-cluster=bool`

IF `starter.debug-cluster` is set, the start will record the status codes it receives
upon "server ready" requests to the log. This option is mainly intended for internal testing.

HTTP API
--------

See [HTTP API](./docs/http_api.md).

Future plans
------------

* Allow starter with agent to be removed from cluster
* Enable cluster to be updated in a controlled manor.
* make port usage configurable

Technical explanation as to what happens
----------------------------------------

The procedure is essentially that the first instance of `arangodb` (aka
the "master") offers an HTTP service on port 8528 for peers to register.
Every instance that registers becomes a slave. As soon as there are
`cluster-agency-size` peers, every instance of `arangodb` starts up an agent (if
it is one of the first 3), a DBserver, and a coordinator. The necessary
command line options to link the `arangod` instances up are generated
automatically. The cluster bootstraps and can be used.

Whenever an `arangodb` instance shuts down, it shuts down the `arangod`
instances under its control as well. When the `arangodb` is started
again, it recalls the old configuration from the `setup.json` file in
its data directory, starts up its `arangod` instances again (with their
data) and they join the cluster.

All network addresses are discovered from the HTTP communication between
the `arangodb` instances. The ports used 8529(/8534/8539) for the coordinator, 
8530(/8535/8540) for the DBserver, 8531(/8536/8537) for the agent) 
need to be free. If more than one instance of an `arangodb` are started 
on the same machine, the second will increase all these port numbers by 5 and so on.

In case the executable is running in Docker, it will use the Docker 
API to retrieve the port number of the Docker host to which the 8528 port 
number is mapped. The containers started by the executable will all 
map the port they use to the exact same host port.

Feedback
--------

Feedback is very welcome in the form of github issues, pull requests
or simply emails to me:

  `Max Neunhöffer <max@arangodb.com>`

