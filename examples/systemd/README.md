# Run ArangoDB Starter with SystemD

This example shows how you can run the ArangoDB starter in a systemd service.

The example will run the starter as user `arangodb` in group `arangodb`.
It requires an environment variable file called `/etc/arangodb.env`
with the following variables.

```text
STARTERENDPOINTS=<comma seperated list of endpoints of all starters (for --starter.join)>
CLUSTERSECRET=<JWT secret used by the cluster>
CLUSTERSECRETPATH=<full path of file containing the JWT secret used by the cluster>
```

To use this service, do the following on every machine on which the starter should run:

- Create the `/etc/arangodb.env` environment file as specified above.
- Copy the `arangodb.service` file to `/etc/systemd/system/arangodb.service`.
- Trigger a systemd reload using `sudo systemctl daemon-reload`.
- Enable the service using `sudo systemctl enable arangodb.service`.
- Start the service using `sudo systemctl start arangodb.service`.

Once started, you can view the logs of the starter using:

```bash
sudo journalctl -flu arangodb.service
```
