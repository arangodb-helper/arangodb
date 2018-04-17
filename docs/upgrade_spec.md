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

If the deployment mode is `activefailover`, the Starters will:

- Perform a version check on all servers, ensuring it supports the upgrade procedure.
  TODO: Specify minimal patch version for 3.2, 3.3 & 3.4,
- Turning off supervision in the Agency and wait for it to be confirmed.
- Restarting one agent at a time with an additional `--database.auto-upgrade=true` argument.
  The agent will perform the auto-upgrade and then stop.
  After that the Starter will automatically restart it with its normal arguments.
- Restarting one resilient single server at a time with an additional `--database.auto-upgrade=true` argument.
  This server will perform the auto-upgrade and then stop.
  After that the Starter will automatically restart it with its normal arguments.
- Turning on supervision in the Agency and wait for it to be confirmed.

If the deployment mode is `cluster`, the Starters will:

- Perform a version check on all servers, ensuring it supports the upgrade procedure.
  TODO: Specify minimal patch version for 3.2, 3.3 & 3.4,
- Turning off supervision in the Agency and wait for it to be confirmed.
- Restarting one agent at a time with an additional `--database.auto-upgrade=true` argument.
  The agent will perform the auto-upgrade and then stop.
  After that the Starter will automatically restart it with its normal arguments.
- Restarting one dbserver at a time with an additional `--database.auto-upgrade=true` argument.
  This dbserver will perform the auto-upgrade and then stop.
  After that the Starter will automatically restart it with its normal arguments.
- Restarting one coordinator at a time with an additional `--database.auto-upgrade=true` argument.
  This coordinator will perform the auto-upgrade and then stop.
  After that the Starter will automatically restart it with its normal arguments.
- Turning on supervision in the Agency and wait for it to be confirmed.

Once all servers in the starter have upgraded, repeat the procedure for the next starter.