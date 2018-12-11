Using the ArangoDB Starter
==========================

This section describes how to start an _Active Failover_ setup using the tool
[_Starter_](../../Programs/Starter/README.md) (the _arangodb_ executable).

{% hint 'warning' %}
It is **strongly suggested** to **not** run the _ArangoDB Starter_ process
(_arangodb_) from a system _user_ with full privileges, e.g. _root_ under Linux.

We suggest to create a dedicated _user_ with limited privileges for this purpose.
Please refer to the [Linux user used to run _arangodb_](../ArangoDBStarter/README.md#linux-user-used-to-run-arangodb)
section of the Starter Deployment page for further details.
{% endhint %}

Local Tests
-----------

If you want to start a local _Active Failover_ setup quickly, use the `--starter.local`
option of the _Starter_. This will start all servers within the context of a single
starter process:

```bash
arangodb --starter.local --starter.mode=activefailover --starter.data-dir=./localdata
```

**Note:** When you restart the _Starter_, it remembers the original `--starter.local` flag.

Multiple Machines
-----------------

If you want to start an _Active Failover_ setup using the _Starter_, use the `--starter.mode=activefailover`
option of the _Starter_. A 3 "machine" _Agency_ is started as well as 3 single servers,
that perform asynchronous replication and failover:

```bash
arangodb --starter.mode=activefailover --server.storage-engine=rocksdb --starter.data-dir=./data --starter.join A,B,C
```

Run the above command on machine A, B & C.

An _Agent_ and a _Single Instance_ will be started on each machine. One of the _Single Instances_ will 
have the _leader_ role, the others two the _follower_ role. If you want to have only one _follower_,
add the option `--cluster.start-single=false` to the machine where the _Single Instance_ should not be
started (only possible while bootstrapping).

Once all the processes started by the _Starter_ are up and running, and joined the
_Active Failover_ setup (this may take a while depending on your system), the _Starter_ will inform
you where to connect the _Active Failover_ from a Browser, shell or your program.

For a full list of options of the _Starter_ please refer to the
[Starter Options](../../Programs/Starter/Options.md) page.

Using the ArangoDB Starter in Docker
------------------------------------

The _Starter_ can also be used to launch an Active Failover setup based on _Docker_
containers. To do this, you can use the normal Docker arguments, combined with
`--starter.mode=activefailover`:

```bash
export IP=<IP of docker host>
docker volume create arangodb
docker run -it --name=adb --rm -p 8528:8528 \
    -v arangodb:/data \
    -v /var/run/docker.sock:/var/run/docker.sock \
    arangodb/arangodb-starter \
    --starter.address=$IP \
    --starter.mode=activefailover \
    --starter.join=A,B,C
```

Run the above command on machine A, B & C.

The _Starter_ will decide on which 2 machines to run a single server instance.
To override this decision (only valid while bootstrapping), add a
`--cluster.start-single=false` to the machine where the single server
instance should _not_ be started.
