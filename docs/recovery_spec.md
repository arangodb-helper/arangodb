# ArangoDB Starter Recovery Procedure

This procedure is intended to recover a cluster (that was started with the ArangoDB Starter) when a machine
of that cluster is broken without the possibility to recover it (e.g. complete HD failure).
In the procedure is does not matter if a replacement machine uses the old or a new IP address.

To recover from this, you must first creates a new (replacement) machine with ArangoDB (including starter) installed.
Then create a file called `RECOVERY` in the data directory on the starter.
This file must contain the IP address and port of the starter that has been broken (and will be replaced with this new machine).

E.g.

```bash
echo "192.168.1.25:8528" > $DATADIR/RECOVERY
```

After creating the `RECOVERY` file, start the starter using all the normal command line arguments.

The starter will now:
1) Talk to the remaining starters to find the ID of the starter it replaces and use that ID to join the remaining starters.
2) Talk to the remaining agents to find the ID of the agent is replaces and adjust the commandline arguments of the agent (it will start) to use that ID.
   This is skipped in the starter was not running an agent.

After that the cluster will now recover automatically.
It will however have 1 more coordinators & dbservers than expected.
Exactly 1 coordinator and 1 dbserver will be listed "red" in the web UI of the database.
They will have to be removed manually using the web UI of the database.
