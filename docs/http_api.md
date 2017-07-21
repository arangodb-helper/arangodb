# HTTP API 

[![GoDoc](https://godoc.org/github.com/arangodb-helper/arangodb/client?status.svg)](http://godoc.org/github.com/arangodb-helper/arangodb/client)

The starter exposes an HTTP(s) API that is used to query information of the starter cluster,
shutdown / remove starters etc.

Some part of the HTTP API is internal and is not supposed to be used by outside clients.

## Public API

### GET `/process`

Returns status information of all of the running processes.

A JSON object is returned with the following fields:

- `servers-started` A boolean that becomes true after all database servers 
  launched by this starter have been started.
- `servers` An array with a JSON object for each database server launch by   this starter. These JSON objects contain the following fields:

  - `type` Indicate type of database server `agent|coordinator|dbserver|single`
  - `ip` IP address the database server is running on.
  - `port` TCP port used by the database server.
  - `pid` Process ID of the database server (0 when database
    server is running in docker)
  - `container-id` Identifier of the docker container that is running 
    the database server.
  - `container-ip` IP address of the docker container that is running 
    the database server.
  - `is-secure` Boolean indicating the use of TLS for this 
    database server.

Status codes:
- 200 On success 

Example:

```json 
{
    "servers-started": true,
    "servers": [
        {
            "type": "coordinator",
            "ip": "192.168.23.10",
            "port": 8529,
            "pid": 12345,
            "container-id": "1234567889A",
            "container-ip": "172.17.0.2",
            "is-secure": true
        }
    ]
}
```

### GET `/logs/agent` 

Returns the contents of the agent log file as `text/plain` content.

Status codes:
- 200 On success 
- 404 When this starter has not launched an agent.
- 503 When starter is not yet ready to read logs.

### GET `/logs/dbserver` 

Returns the contents of the dbserver log file as `text/plain` content.

Status codes:
- 200 On success 
- 404 When this starter has not launched an dbserver.
- 503 When starter is not yet ready to read logs.

### GET `/logs/coordinator` 

Returns the contents of the coordinator log file as `text/plain` content.

Status codes:
- 200 On success 
- 404 When this starter has not launched an coordinator.
- 503 When starter is not yet ready to read logs.

### GET `/logs/single` 

Returns the contents of the single server log file as `text/plain` content.

Status codes:
- 200 On success 
- 404 When this starter has not launched an single server.
- 503 When starter is not yet ready to read logs.

### GET `/version` 

Returns a JSON object with the version information. 

The JSON object contains the following fields:

- `version` Semver compatible version of the starter.
- `build` Git hash of the starter.

Status codes:
- 200 On success 

Example:

```json
{
    "version": "0.8.0+git",
    "build":"e6dbb08"
}
```

### POST `/shutdown` 

Initiates a shutdown of the process and all servers started by it. 

If you passing a `mode=goodbye` query to the URL, the starter will
remove all database servers from the cluster and ask the starter 
master to be removed from the cluster configuration.

Currently a starter does not accept `mode=goodbye` when is has launched
an agent.

The request does not expect any input.

Returns `OK` as text/plain on success.

Status codes:
- 200 On success 
- 412 When this starter cannot be removed.
- 503 When starter is not yet ready to be removed.

## Internal API

### GET `/hello` 

Internal API used to join a master. Not for external use.

### POST `/goodbye` 

Internal API used to leave a master for good. Not for external use.

### POST `/cb/masterChanged`

Internal API used to notify a starter that the master URL has changed
in the agency.

## Error handling 

All API methods return an HTTP status code to indicate success or failure.

The following status codes are used.

- 200 Request succeeded 
- 307 Temporary redirect. Typically used to redirect to the master starter.
  Clients should redirect their request, with the same HTTP method and payload, 
  to an URL found in the `Location` header.
- 400 Bad request. Used to indicate that some of the requests parameters are incorrect.
- 412 Precondition failed. Used to indicate that the requests parameters are correct,
  but the state of the system is such that the request cannot be executed at this time.
- 503 Service unavailable. Used to indicate that at this time the request cannot be 
  fullfilled. Clients are expected to retry after a short period.
