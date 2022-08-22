# Starting an ArangoDB cluster or database the easy way

[![Docker Pulls](https://img.shields.io/docker/pulls/arangodb/arangodb-starter.svg)](https://hub.docker.com/r/arangodb/arangodb-starter/)
[![GoDoc](https://godoc.org/github.com/arangodb-helper/arangodb/client?status.svg)](http://godoc.org/github.com/arangodb-helper/arangodb/client)

## Downloading Releases

You can download precompiled `arangodb` binaries via [the github releases page](https://github.com/arangodb-helper/arangodb/releases).

Note: `arangodb` is also included in all current distributions of ArangoDB.

## Building

If you want to compile `arangodb` yourselves just do:

```bash
go get -u github.com/arangodb-helper/arangodb
```

This will result in a binary at `$GOPATH/bin/arangodb`.

For more advanced build options, clone this repository and do:

```bash
make local
```

and the executable is in `./bin` named after the current OS & architecture (e.g. `linux/amd64/arangodb`).
You can copy the binary anywhere in your PATH.
A link to the binary for the local OS & architecture is made to `./arangodb`.
This program will run on Linux, OSX or Windows.

Note: The standard build uses a docker container to run the build. If docker is not available
`make local` runs the [go compiler](https://golang.org/) directly and places the binary directly in the project directory.
In this case you need to install the `golang` package on your system (version 1.7 or higher).

## Building docker image

If you want to create the `arangodb/arangodb-starter` docker container yourselves
you can build it using:

```bash
make docker
```

## Starting a cluster

Install ArangoDB in the usual way as binary package. Then:

On host A:

```bash
arangodb
```

This will use port 8528 to wait for colleagues (3 are needed for a
resilient agency). On host B: (can be the same as A):

```bash
arangodb --starter.join A
```

This will contact A on port 8528 and register. On host C: (can be same
as A or B):

```bash
arangodb --starter.join A
```

This will contact A on port 8528 and register.

From the moment on when 3 have joined, each will fire up an Agent, a
Coordinator and a DB-Server and the cluster is up. Ports are shown on
the console, the starter uses the next few ports above the starter
port. That is, if one uses port 8528 for the starter, the Coordinator
will use 8529 (=8528+1), the DB-Server 8530 (=8528+2), and the Agent 8531
(=8528+3). See below under `--starter.port` for how to change the
starter default port.

Additional servers can be added in the same way.

## Using the config file to provide CLI options

Starter looks for `arangodb-starter.conf` in working directory by default.
You can specify the config file path using `--configuration` (`-c`)  CLI option:
```bash
arangodb --configuration=/etc/arangodb-starter.conf
```

`.conf` files for ArangoDB Starter are in a simple key-value pair format. 
Each option is specified on a separate line in the form:
```
key = value
```

It may look like this:
```
starter.id = my-starter-1
starter.endpoint = http://127.0.0.1:8528
```

Alternatively, a header section can be specified and options pertaining to
that section can be specified in a shorter form:
```
[starter]
id = my-starter-1
endpoint = http://127.0.0.1:8528
sync = true

[sync]
start-master = true
start-worker = true
monitoring.token = 8cd07f3a5ff98f2a78cfc366c13fb123eb8d29c1ca37c79df190425d5b9e424d
```

So you see, a command line option `‑‑section.param value` can be easily translated
to an option in a configuration file:
```
[section]
param = value
```

You also can use the config file to provide passthrough options:
```
starter.mode = cluster

[args]
all.default-language = es_419
dbservers.rocksdb.enable-statistics = true
```

## More usage info

See the [ArangoDB Starter Tutorial](https://www.arangodb.com/docs/stable/tutorials-starter.html).

## HTTP API

See [HTTP API](./docs/http_api.md).

## Future plans

- Allow starter with Agent to be removed from cluster
- Enable cluster to be updated in a controlled manner.

## Technical explanation as to what happens

The procedure is essentially that the first instance of `arangodb` (aka
the "master") offers an HTTP service on port 8528 for peers to register.
Every instance that registers becomes a slave. As soon as there are
`cluster-agency-size` peers, every instance of `arangodb` starts up an Agent (if
it is one of the first 3), a DB-Server, and a Coordinator. The necessary
command line options to link the `arangod` instances up are generated
automatically. The cluster bootstraps and can be used.

Whenever an `arangodb` instance shuts down, it shuts down the `arangod`
instances under its control as well. When the `arangodb` is started
again, it recalls the old configuration from the `setup.json` file in
its data directory, starts up its `arangod` instances again (with their
data) and they join the cluster.

All network addresses are discovered from the HTTP communication between
the `arangodb` instances. The ports used 8529(/8539/8549) for the Coordinator,
8530(/8540/8550) for the DB-Server, 8531(/8541/8551) for the Agent)
need to be free. If more than one instance of an `arangodb` are started
on the same machine, the second will increase all these port numbers by 10 and so on.

In case the executable is running in Docker, it will use the Docker
API to retrieve the port number of the Docker host to which the 8528 port
number is mapped. The containers started by the executable will all
map the port they use to the exact same host port.

## Feedback

Feedback is very welcome in the form of github issues, pull requests
or an email to `hackers@arangodb.com`.
