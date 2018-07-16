# ArangoDB Starter Upgrade Procedure

This procedure is intended to upgrade a deployment (that was started with the ArangoDB Starter)
to a new minor version of ArangoDB.

The procedure is as follows:

- For all Starters:
  - Install the new ArangoDB version binaries (which includes the latest ArangoDB Starter binary)
  - Shutdown the Starter without stopping the ArangoDB Server process.
    To do so, execute a `kill -9 <pid=of-starter>`
  - Restart the Starter. When using a suppervisor like SystemD, this will happen automatically.

- Send an HTTP `POST` request with empty body to `/database-auto-upgrade` on one of the starters.

The Starter will respond to this request depending on the deployment mode.

If the deployment mode is `single`, the Starter will:

- Restart the single server with an additional `--database.auto-upgrade=true` argument.
  The server will perform the auto-upgrade and then stop.
  After that the Starter will automatically restart it with its normal arguments.

If the deployment mode is `activefailover` or `cluster` the Starters will:

- Perform a version check on all servers, ensuring it supports the upgrade procedure.
  TODO: Specify minimal patch version for 3.2, 3.3 & 3.4,
- Turning off supervision in the Agency and wait for it to be confirmed.
- Create an upgrade plan and store it in the agency.
  The plan consists of upgrade entries for all agents,
  followed by upgrade entries for all single servers (in case of `activefailover`),
  followed by upgrade entries for all dbservers (in case of `cluster`),
  followed by upgrade entries for all coordinators (in case of `cluster`),
  followed by an entry to re-enable agency supervision.

Every Starter will monitor the agency for an upgrade plan.
As soon as it detects an upgrade plan, it will inspect the first entry
of that plan.

If the first entry involves a server that is under control of this Starter,
it will restart the server once with an additional
`--database.auto-upgrade=true` argument.
This server will perform the auto-upgrade and then stop.
The Starter will wait for this server to terminate and restart it with all
the usual arguments.

Once the Starter has confirmed that the newly started server is up and running,
it will remove the first entry from the upgrade plan.

If the first entry of the upgrade plan is a re-enable agency supervision
item, the leader Starter will re-enable agency supervision and mark
the upgrade plan as ready.

## Upgrade state inspection

A new API will be added to inspect the current state of the upgrade process.

This API will be a `GET` request to `/database-auto-upgrade`, resulting in
a JSON object with the following fields:

- `ready` a boolean that is `true` when the plan has been finished succesfully,
  or `false` otherwise.
- `failed` a boolean that is `true` when the plan has resulted in an
  upgrade failure, or `false` otherwise.
- `reason` a string that describes the state of the upgrade plan in a
  human readable form.
- `servers_upgraded` an integer containing the number of servers that have
  been upgraded.
- `servers_remaining` an integer containing the number of servers that have
  not yet been upgraded.

## Failures

If the upgrade procedure of one of the servers fails (for whatever reason),
the Starter that performs that part of the procedure will mark the
first entry of the upgrade plan as failed.
No Starter will act on an upgrade plan entry that is marked failed.

A new API will be added to remove the failure flag from the first entry
of an upgrade plan, such that the procedure can be retried.

This API will be a `POST` request to `/database-auto-upgrade/retry`.
