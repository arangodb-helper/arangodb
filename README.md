Starting an ArangoDB cluster the easy way
=========================================

Building
--------

Just do

```
make local
```

and the executable is in `./bin` named after the current OS & architecture (e.g. `arangodb-linux-amd64`).
You can copy the binary anywhere in your PATH.
A link to the binary for the local OS & architecture is made to `./arangodb`.
This program will run on Linux, OSX or Windows.

Note: The standard build uses a docker container to run the build if. If docker is not available 
`make local` runs the [go compiler](https://golang.org/) directly and places the binary directly in the project directory.
In this case you need to install the `golang` package on your system.

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

The executable can be run inside Docker. In that case it will also run all 
servers in a Docker container. 

First make sure the docker images are build using:

```
make docker 
```

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

Common options 
--------------

* `--dataDir path`

`path` is the directory in which all data is stored. (default "./")

In the directory, there will be a single file `setup.json` used for
restarts and a directory for each instances that runs on this machine.
Different instances of `arangodb` must use different data directories.

* `--join addr`

join a cluster with master at address `addr` (default "")

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

* `--dockerUser user`

`user` is an expression to be used for `docker run` with the `--user` 
option. One can give a user id or a user id and a group id, separated
by a colon. The purpose of this option is to limit the access rights
of the process in the Docker container.

* `--dockerEndpoint endpoint`

`endpoint` is the URL used to reach the docker host. This is needed to run 
the executable in docker. The default value is "unix:///var/run/docker.sock".

* `--dockerNetHost bool`

If `dockerNetHost` is set, all docker container will be started 
with the `--net=host` option.

* `--dockerPrivileged bool`

If `dockerPrivileged` is set, all docker container will be started 
with the `--privileged` option turned on.

HTTP API
--------

- GET `/process` returns status information of all of the running processes.
- GET `/logs/agent` returns the contents of the agent log file.
- GET `/logs/dbserver` returns the contents of the dbserver log file.
- GET `/logs/coordinator` returns the contents of the coordinator log file.
- GET `/version` returns a JSON object with the version & build information. 
- POST `/shutdown` initiates a shutdown of the process and all servers started by it. 
  (passing a `mode=goodbye` query to the URL makes the peer say goodbye to the master).
- GET `/hello` internal API used to join a master. Not for external use.
- POST `/goodbye` internal API used to leave a master for good. Not for external use.

Future plans
------------

* bundle this program with the usual distribution
* make port usage configurable
* support SSL
* support authentication

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
the `arangodb` instances. The ports used 4001(/4006/4011) for the agent, 
4002(/4007/4012) for the coordinator, 4003(/4008/4013) for the DBserver) 
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

