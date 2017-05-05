Starting an ArangoDB cluster the easy way
=========================================
Downloading Releases
--------------------
You can download precompiled ArangoDBStarter binaries via [the github releases page](https://github.com/arangodb-helper/ArangoDBStarter/releases).

Building
--------

If you want to compile ArangoDBStarter yourselves just do

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

This will use port 4000 to wait for colleagues (3 are needed for a
resilient agency). On host B: (can be the same as A):

```
arangodb --join A
```

This will contact A on port 4000 and register. On host C: (can be same
as A or B):

```
arangodb --join A
```

This will contact A on port 4000 and register.

From the moment on when 3 have joined, each will fire up an agent, a 
coordinator and a dbserver and the cluster is up. Ports are shown on
the console.

Additional servers can be added in the same way.

If two or more of the `arangodb` instances run on the same machine,
one has to use the `--dataDir` option to let each use a different
directory.

The `arangodb` program will find the ArangoDB executable and the
other installation files automatically. If this fails, use the
`--arangod` and `--jsdir` options described below.

Running in Docker 
-----------------
You can run ArangoDBStarter using our ready made docker container. 

When using ArangoDBStarter in a Docker contairer it will also run all 
servers in a Docker using the `arangodb/arangodb` container.

When running in Docker it is important to care about the volume mappings on 
the container. Typically you will start the executable in docker with the following
commands.

```
export IP=<IP of docker host>
docker volume create arangodb1
docker run -it --name=adb1 --rm -p 4000:4000 \
    -v arangodb1:/data \
    -v /var/run/docker.sock:/var/run/docker.sock \
    arangodb/arangodb-starter \
    --dockerContainer=adb1 --ownAddress=$IP
```

The executable will show the commands needed to run the other instances.

Note that the commands above create a docker volume. If you're running on Linux 
it is also possible to use a host mapped volume. Make sure to map it on `/data`.

If you want to create the `arangodb/arangodb-starter` docker container yourselves
you can build it using:

```
make docker 
```

Starting a local test cluster
-----------------------------

If you want to start a local cluster quickly, use the `--local` flag. 
It will start all servers within the context of a single starter process.

```
arangodb --local
```

Note: When you restart the started, it remembers the original `--local` flag.

Starting a single server
------------------------

If you want to start a single database server, use `--mode=single`.

```
arangodb --mode=single
```

Starting a single server in Docker
----------------------------------

If you want to start a single database server running in a docker container,
use the normal docker arguments, combined with `--mode=single`.

```
export IP=<IP of docker host>
docker volume create arangodb
docker run -it --name=adb --rm -p 4000:4000 \
    -v arangodb:/data \
    -v /var/run/docker.sock:/var/run/docker.sock \
    arangodb/arangodb-starter \
    --dockerContainer=adb --ownAddress=$IP --mode=single
```

Common options 
--------------

* `--dataDir path`

`path` is the directory in which all data is stored. (default "./")

In the directory, there will be a single file `setup.json` used for
restarts and a directory for each instances that runs on this machine.
Different instances of `arangodb` must use different data directories.

* `--join addr`

join a cluster with master at address `addr` (default "")

* `--local` 

Start a local (test) cluster. Since all servers are running on a single machine 
this is really not intended for production setups.

* `--mode cluster|single`

Select what kind of database configuration you want. 
This can be a `cluster` configuration (which is the default), or a `single` server 
configuration.

Note that when running a `single` server configuration you will lose all 
high availability features that a cluster provides you.

* `--agencySize int`

number of agents in agency (default 3).

This number has to be positive and odd, and anything beyond 5 probably
does not make sense. The default 3 allows for the failure of one agent.

* `--ownAddress addr`

`addr` is the address under which this server is reachable from the
outside.

Usually, this option does not have to be specified. Only in the case
that `--agencySize` is set to 1 (see below), the master has to know
under which address it can be reached from the outside. If you specify
`localhost` here, then all instances must run on the local machine.

* `--docker image`

`image` is the name of a Docker image to run instead of the normal
executable. For each started instance a Docker container is launched.
Usually one would use the Docker image `arangodb/arangodb`.

* `--dockerContainer containerName`

`containerName` is the name of a Docker container that is used to run the
executable. This argument is required when running the executable in docker.

Authentication options
----------------------

The arango starter by default creates a cluster that uses no authentication.

To create a cluster that uses authentication, create a file containing a random JWT secret (single line)
and pass it through the `--jwtSecretFile` option.

For example:

```
echo "MakeThisSecretMuchStronger" > jwtSecret 
arangodb --jwtSecretFile=./jwtSecret
```

All starters used in the cluster must have the same JWT secret.

SSL options
-----------

The arango starter by default creates a cluster that uses no unencrypted connections (no SSL).

To create a cluster that uses encrypted connections, you can use an existing server key file 
or let the starter create one for you.

To use an existing server key file use the `--sslKeyFile` option like this:

```
arangodb --sslKeyFile=myServer.key
```

Go to the [SSL manual](https://docs.arangodb.com/3.1/Manual/Administration/Configuration/SSL.html) for more
information on how to create a server key file.

To let the starter created a self-signed server key file, use the `--sslAutoKeyFile` option like this:

```
arangodb --sslAutoKeyFile
```

All starters used to make a cluster must be using SSL or not.
You cannot have one starter using SSL and another not using SSL.

Note that all starters can use different server key files.

Additional SSL options:

* `--sslCAFile path`

Configure the servers to require a client certificate in their communication to the servers using the CA certificate in a file with given path.

* `--sslAutoServerName name` 

name of the server that will be used in the self-signed certificate created by the `--sslAutoKeyFile` option.

* `--sslAutoOrganization name` 

name of the server that will be used in the self-signed certificate created by the `--sslAutoKeyFile` option.

Esoteric options
----------------

* `--masterPort int`

port for arangodb master (default 4000).

This is the port used for communication of the `arangodb` instances
amongst each other.

* `--arangod path`

path to the `arangod` executable (default varies from platform to
platform, an executable is searched in various places).

This option only has to be specified if the standard search fails.

* `--jsDir path`

path to JS library directory (default varies from platform to platform,
this is coupled to the search for the executable).

This option only has to be specified if the standard search fails.

* `--startCoordinator bool`

This indicates whether or not a coordinator instance should be started 
(default true).

* `--startDBserver bool`

This indicates whether or not a DBserver instance should be started 
(default true).

* `--rr path`

path to rr executable to use if non-empty (default ""). Expert and
debugging only.

* `--verbose bool`

show more information (default false).

* `--uniquePortOffsets bool`

If set to true, all port offsets (of slaves) will be made globally unique.
By default (value is false), port offsets will be unique per slave address.

* `--dockerUser user`

`user` is an expression to be used for `docker run` with the `--user` 
option. One can give a user id or a user id and a group id, separated
by a colon. The purpose of this option is to limit the access rights
of the process in the Docker container.

* `--dockerEndpoint endpoint`

`endpoint` is the URL used to reach the docker host. This is needed to run 
the executable in docker. The default value is "unix:///var/run/docker.sock".

* `--dockerNetworkMode mode`

If `dockerNetworkMode` is set, all docker container will be started 
with the `--net=<mode>` option.

* `--dockerPrivileged bool`

If `dockerPrivileged` is set, all docker container will be started 
with the `--privileged` option turned on.

* `--dockerNetHost bool` (deprecated)

If `dockerNetHost` is set, all docker container will be started 
with the `--net=host` option.

This option is deprecated, use `--dockerNetworkMode=host` instead.

HTTP API
--------

- GET `/process` returns status information of all of the running processes.
- GET `/logs/agent` returns the contents of the agent log file.
- GET `/logs/dbserver` returns the contents of the dbserver log file.
- GET `/logs/coordinator` returns the contents of the coordinator log file.
- GET `/logs/single` returns the contents of the single server log file.
- GET `/version` returns a JSON object with the version & build information. 
- POST `/shutdown` initiates a shutdown of the process and all servers started by it. 
  (passing a `mode=goodbye` query to the URL makes the peer say goodbye to the master).
- GET `/hello` internal API used to join a master. Not for external use.
- POST `/goodbye` internal API used to leave a master for good. Not for external use.

Future plans
------------

* bundle this program with the usual distribution
* make port usage configurable

Technical explanation as to what happens
----------------------------------------

The procedure is essentially that the first instance of `arangodb` (aka
the "master") offers an HTTP service on port 4000 for peers to register.
Every instance that registers becomes a slave. As soon as there are
`agencySize` peers, every instance of `arangodb` starts up an agent (if
it is one of the first 3), a DBserver, and a coordinator. The necessary
command line options to link the `arangod` instances up are generated
automatically. The cluster bootstraps and can be used.

Whenever an `arangodb` instance shuts down, it shuts down the `arangod`
instances under its control as well. When the `arangodb` is started
again, it recalls the old configuration from the `setup.json` file in
its data directory, starts up its `arangod` instances again (with their
data) and they join the cluster.

All network addresses are discovered from the HTTP communication between
the `arangodb` instances. The ports used 4001(/4006/4011) for the coordinator, 
4002(/4007/4012) for the DBserver, 4003(/4008/4013) for the agent) 
need to be free. If more than one instance of an `arangodb` are started 
on the same machine, the second will increase all these port numbers by 5 and so on.

In case the executable is running in Docker, it will use the Docker 
API to retrieve the port number of the Docker host to which the 4000 port 
number is mapped. The containers started by the executable will all 
map the port they use to the exact same host port.

Feedback
--------

Feedback is very welcome in the form of github issues, pull requests
or simply emails to me:

  `Max Neunhöffer <max@arangodb.com>`

