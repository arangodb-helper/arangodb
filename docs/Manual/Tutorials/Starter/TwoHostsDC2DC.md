# Run ArangoDB Starter with DC2DC on two hosts

This tutorial shows how to run an ArangoDB Cluster using the Starter locally (twice) with datacenter
to datacenter replication between the two clusters on different hosts.

Note: In this tutorial the two Clusters share the same _secrets_. This approach is NOT suitable for any kind of production use!

## Step 1: Create certificates & tokens

Firstly you need to create the certificates and tokens using the following commands:

On Node1: 

```bash
export IP1=<node1 IP>
export IP2=<node2 IP>
export HOST1=<node1 hostname>
export HOST2=<node2 hostname>
export CERTDIR=<directories where to store certificates & tokens>
export DATADIR=<directory where to store ArangoDB files>
mkdir -p ${CERTDIR}
mkdir -p ${DATADIR}

# Create TLS certificates
arangodb create tls ca --cert=${CERTDIR}/tls-ca.crt --key=${CERTDIR}/tls-ca.key
arangodb create tls keyfile --cacert=${CERTDIR}/tls-ca.crt --cakey=${CERTDIR}/tls-ca.key --keyfile=${CERTDIR}/cluster1/tls.keyfile --host=$IP1 --host=$HOST1
arangodb create tls keyfile --cacert=${CERTDIR}/tls-ca.crt --cakey=${CERTDIR}/tls-ca.key --keyfile=${CERTDIR}/cluster2/tls.keyfile --host=$IP2 --host=$HOST2

# Create client authentication certificates
arangodb create client-auth ca --cert=${CERTDIR}/client-auth-ca.crt --key=${CERTDIR}/client-auth-ca.key
arangodb create client-auth keyfile --cacert=${CERTDIR}/client-auth-ca.crt --cakey=${CERTDIR}/client-auth-ca.key --keyfile=${CERTDIR}/client-auth-ca.keyfile

# Create JWT secrets
arangodb create jwt-secret --secret=${CERTDIR}/cluster1/syncmaster.jwtsecret
arangodb create jwt-secret --secret=${CERTDIR}/cluster1/arangodb.jwtsecret
arangodb create jwt-secret --secret=${CERTDIR}/cluster2/syncmaster.jwtsecret
arangodb create jwt-secret --secret=${CERTDIR}/cluster2/arangodb.jwtsecret
```

On Node2:

```bash
export IP1=<node1 IP>
export IP2=<node2 IP>
export HOST1=<node1 hostname>
export HOST2=<node2 hostname>
export CERTDIR=<directories where to store certificates & tokens>
export DATADIR=<directory where to store ArangoDB files>
mkdir -p ${CERTDIR}
mkdir -p ${DATADIR}
```

## Step 2: Start first & second cluster

Now you can start the two Clusters:

On Node1:

```bash
arangodb --starter.data-dir=${DATADIR}/cluster1 \
    --starter.sync \
    --starter.local \
    --auth.jwt-secret=${CERTDIR}/cluster1/arangodb.jwtsecret \
    --sync.server.keyfile=${CERTDIR}/cluster1/tls.keyfile \
    --sync.server.client-cafile=${CERTDIR}/client-auth-ca.crt \
    --sync.master.jwt-secret=${CERTDIR}/cluster1/syncmaster.jwtsecret
```

On Node2:

```
arangodb --starter.data-dir=arango/cluster2 \
    --starter.sync \
    --starter.local \
    --auth.jwt-secret=${CERTDIR}/cluster2/arangodb.jwtsecret \
    --sync.server.keyfile=${CERTDIR}/cluster2/tls.keyfile \
    --sync.server.client-cafile=${CERTDIR}/client-auth-ca.crt \
    --sync.master.jwt-secret=${CERTDIR}/cluster2/syncmaster.jwtsecret
```

Note that it is not uncommon for a syncmaster to restart, since the cluster is not yet ready when it is started.

## Step 3: Configure synchronization from cluster 1 to cluster 2 

Now that the two Clusters are up and running, you can start the replication with the following command:

```bash
export IP1=<node1 IP>
export IP2=<node2 IP>
export HOST1=<node1 hostname>
export HOST2=<node2 hostname>
export CERTDIR=<directories where to store certificates & tokens>

arangosync configure sync \
    --master.endpoint=https://$IP2:8542 \
    --master.keyfile=${CERTDIR}/client-auth-ca.keyfile \
    --source.endpoint=https://$IP1:8542 \
    --source.cacert=${CERTDIR}/tls-ca.crt \
    --auth.keyfile=${CERTDIR}/client-auth-ca.keyfile \
    --master.cacert=${CERTDIR}/tls-ca.crt
```

## Step 4: Check status of configuration

You can check the status of the synchronization with the following commands: 

```bash
# Check status of cluster 1
export IP1=<node1 IP>
export IP2=<node2 IP>
export HOST1=<node1 hostname>
export HOST2=<node2 hostname>
export CERTDIR=<directories where to store certificates & tokens>

arangosync get status \
    --master.endpoint=https://${IP1}:8542 \
    --auth.keyfile=${CERTDIR}/client-auth-ca.keyfile \
    --verbose

# Check status of cluster 2
export IP1=<node1 IP>
export IP2=<node2 IP>
export HOST1=<node1 hostname>
export HOST2=<node2 hostname>
export CERTDIR=<directories where to store certificates & tokens>

arangosync get status \
    --master.endpoint=https://${IP}:8542 \
    --auth.keyfile=${CERTDIR}/client-auth-ca.keyfile \
    --verbose
```
