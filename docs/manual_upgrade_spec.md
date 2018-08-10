# Upgrading 3.3 -> 3.4 without service interruption

Assume a 3 node cluster started by the starter, with `arangodb` systemd
services.

 1. Stop all DC2DC replication involving this cluster (`arangosync stop sync`)

 2. Upgrade the software on all machines, note that this does not restart
    any service.

 3. Make sure starters are not restarted automatically by systemd:
    Edit `/etc/systemd/system/arangodb.service` to say

        Restart=no

    (instead of `Restart=on-failure`) and tell systemd:

        sudo systemctl daemon-reload

    This is necessary because we will need a controlled downtime for starters.

 4. Upgrade the agents (one machine at a time):
    
     - kill -9 on `arangodb` (starter) process
     - kill agent process
     - use command in `arangod_command.txt` with the addition of

        --database.auto-upgrade=true

       this will upgrade the database for that agent
     - restart agent by

        sudo systemctl start arangodb

     - verify that the agency is working correctly with 3 agents:

        curl http://<host>:8531/_api/agency/config | jq .

       one must see all three agents in sync

 5. Switch agency supervision to maintenance mode:

        curl http://<server>:8529/_admin/cluster/... -X PUT -d '{...}'

 6. Upgrade dbservers (one machine at a time):

     - kill -9 in `arangodb` (starter) process
     - kill dbserver process
     - use command in `arangod_command.txt` with the addition of

        --database.auto-upgrade=true

       this will upgrade the database for that dbserver
     - restart dbserver by

        sudo systemctl start arangodb

 7. Switch agency supervision to normal mode:

        curl http://<server>:8529/_admin/cluster/... -X PUT -d '{...}'

 8. Upgrade coordinators (one machine at a time):

     - kill -9 in `arangodb` (starter) process
     - kill coordinator process
     - use command in `arangod_command.txt` with the addition of

        --database.auto-upgrade=true

       this will upgrade the database for that coordinator
     - restart coordinator by

        sudo systemctl start arangodb

 9. Make sure starters are again restarted automatically by systemd:
    Edit `/etc/systemd/system/arangodb.service` to say

        Restart=on-failure

    (instead of `Restart=no`) and tell systemd:

        sudo systemctl daemon-reload

10. Kill all `arangosync` processes one after another.

11. Reestablish DC2DC replications involving this cluster.

