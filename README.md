Starting an ArangoDB cluster the easy way
=========================================

Building
--------

Just do

    cd arangodb
    go build

and the executable is in `arangodb/arangodb`. You can install it
anywhere in your path. This program will run on Linux, OSX or Windows.

Starting a cluster
------------------

Install ArangoDB in the usual way as binary package. Then:

On host A:

    arangodb

This will use port 4000 to wait for colleagues (3 are needed for a
resilient agency). On host B: (can be the same as A):

    arangodb --join A

This will contact A:4000 and register. On host C: (can be same as A or B):

    arangodb --join A

will contact A:4000 and register.

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

Future plans
------------

* allow to run Docker images for the instances on Linux
* deploy this program as a Docker image
* bundle this program with the usual distribution
* make port usage configurable
* support SSL
* support authentication

Technical explanation as to what happens
----------------------------------------

The procedure is essentially that the first instance of `arangodb` (aka
the "master" offers an HTTP service on port 4000 for peers to register.
Every instance that registeres becomes a slave. As soon as there are
`agencySize` peers, every instance of `arangodb` starts up an agent (if
it is one of the first 3), a DBserver, and a coordinator. The necessary
command line options to link the `arangod` instances up are generated
automatically. The cluster bootstraps and can be used.

Whenever an `arangodb` instances shuts down, it shuts down the `arangod`
instances under its control as well. When the `arangodb` is started
again, it recalls the old configuration from the `setup.json` file in
its data directory, starts up its `arangod` instances again (with their
data) and they join the cluster.

All network addresses are discovered from the HTTP communication between
the `arangodb` instances. The ports used (4001 for the agent, 8530 for
the coordinator, 8629 for the DBserver) need to be free. If more than
one instance of an `arangodb` are started on the same machine, the
second will increase all these port numbers by 1 and so on.
