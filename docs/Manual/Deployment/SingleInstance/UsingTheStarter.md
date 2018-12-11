Using the ArangoDB Starter
==========================

This section describes how to start an ArangoDB _Single Instance_ using the tool
[_Starter_](../../Programs/Starter/README.md) (the _arangodb_ executable).

{% hint 'warning' %}
It is **strongly suggested** to **not** run the _ArangoDB Starter_ process
(_arangodb_) from a system _user_ with full privileges, e.g. _root_ under Linux.

We suggest creating a dedicated, limited-privileges _user_ for this purpose.
Please refer to the [Linux user used to run _arangodb_](../ArangoDBStarter/README.md#linux-user-used-to-run-arangodb)
section of the Starter Deployment page for further details.
{% endhint %}

Local Start
-----------

If you want to start a stand-alone instance of ArangoDB, use the `--starter.mode=single`
option of the _Starter_: 

```bash
arangodb --starter.mode=single
```

Using the ArangoDB Starter in Docker
------------------------------------

The _Starter_ can also be used to launch a stand-alone instance based on _Docker_
containers:

```bash
export IP=<IP of docker host>
docker volume create arangodb
docker run -it --name=adb --rm -p 8528:8528 \
    -v arangodb:/data \
    -v /var/run/docker.sock:/var/run/docker.sock \
    arangodb/arangodb-starter \
    --starter.address=$IP \
    --starter.mode=single 
```
