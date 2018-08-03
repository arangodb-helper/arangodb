# Run ArangoDB Starter with DC2DC locally

This tutorial shows how to run an ArangoDB Cluster using the Starter locally (twice) with datacenter
to datacenter replication between the two clusters.

Note: In this tutorial the two Clusters share the same _secrets_. This approach is NOT suitable for any kind of production use!

## Step 1: Create certificates & tokens

Firstly you need to create the certificates and tokens using the following commands:

```bash
export CERTDIR=<directories where to store certificates & tokens>
export IP=<IP address of this machine>
mkdir -p ${CERTDIR}

# Create TLS certificates
arangodb create tls ca --cert=${CERTDIR}/tls-ca.crt --key=${CERTDIR}/tls-ca.key
arangodb create tls keyfile --cacert=${CERTDIR}/tls-ca.crt --cakey=${CERTDIR}/tls-ca.key --keyfile=${CERTDIR}/cluster1/tls.keyfile --host=${IP} --host=localhost
arangodb create tls keyfile --cacert=${CERTDIR}/tls-ca.crt --cakey=${CERTDIR}/tls-ca.key --keyfile=${CERTDIR}/cluster2/tls.keyfile --host=${IP} --host=localhost

# Create client authentication certificates
arangodb create client-auth ca --cert=${CERTDIR}/client-auth-ca.crt --key=${CERTDIR}/client-auth-ca.key
arangodb create client-auth keyfile --cacert=${CERTDIR}/client-auth-ca.crt --cakey=${CERTDIR}/client-auth-ca.key --keyfile=${CERTDIR}/client-auth-ca.keyfile

# Create JWT secrets
arangodb create jwt-secret --secret=${CERTDIR}/cluster1/syncmaster.jwtsecret
arangodb create jwt-secret --secret=${CERTDIR}/cluster1/arangodb.jwtsecret
arangodb create jwt-secret --secret=${CERTDIR}/cluster2/syncmaster.jwtsecret
arangodb create jwt-secret --secret=${CERTDIR}/cluster2/arangodb.jwtsecret
```

## Step 2: Start first & second cluster

Now you can start the two Clusters:

```bash
export CERTDIR=<directories where to store certificates & tokens>
export IP=<IP address of this machine>
export DATADIR=<directories where to store database files>
mkdir -p ${DATADIR}

arangodb --starter.data-dir=/${DATADIR}/cluster1 \
    --starter.sync \
    --starter.local \
    --auth.jwt-secret=${CERTDIR}/cluster1/arangodb.jwtsecret \
    --sync.server.keyfile=${CERTDIR}/cluster1/tls.keyfile \
    --sync.server.client-cafile=${CERTDIR}/client-auth-ca.crt \
    --sync.master.jwt-secret=${CERTDIR}/cluster1/syncmaster.jwtsecret \
    --starter.address=${IP}

## In another terminal
export CERTDIR=<directories where to store certificates & tokens>
export IP=<IP address of this machine>
export DATADIR=<directories where to store database files>
mkdir -p ${DATADIR}

arangodb --starter.data-dir=/${DATADIR}/cluster2 \
    --starter.sync \
    --starter.local \
    --auth.jwt-secret=${CERTDIR}/cluster2/arangodb.jwtsecret \
    --sync.server.keyfile=${CERTDIR}/cluster2/tls.keyfile \
    --sync.server.client-cafile=${CERTDIR}/client-auth-ca.crt \
    --sync.master.jwt-secret=${CERTDIR}/cluster2/syncmaster.jwtsecret \
    --starter.address=${IP} \
    --starter.port=9528
```

Note that it is not uncommon for a syncmaster to restart, since the cluster is not yet ready when it is started.

## Step 3: Configure synchronization from cluster 1 to cluster 2 

Now that the two Clusters are up and running, you can start the replication with the following command:

```bash
export CERTDIR=<directories where to store certificates & tokens>
export IP=<IP address of this machine>

arangosync configure sync \
    --master.endpoint=https://${IP}:9542 \
    --master.keyfile=${CERTDIR}/client-auth-ca.keyfile \
    --source.endpoint=https://${IP}:8542 \
    --source.cacert=${CERTDIR}/tls-ca.crt \
    --auth.keyfile=${CERTDIR}/client-auth-ca.keyfile
```
The above command will produce an output similar to the following:

```
2018-08-03T12:41:43+02:00 |WARN| Accepting unverified master TLS certificate. Use --master.cacert. component=cli
Synchronization activated.
```

## Step 4: Check status of configuration

You can check the status of the synchronization with the following commands: 

```bash
# Check status of cluster 1
export CERTDIR=<directories where to store certificates & tokens>
export IP=<IP address of this machine>

arangosync get status \
    --master.endpoint=https://${IP}:8542 \
    --auth.keyfile=${CERTDIR}/client-auth-ca.keyfile \
    --verbose

# Check status of cluster 2
export CERTDIR=<directories where to store certificates & tokens>
export IP=<IP address of this machine>

arangosync get status \
    --master.endpoint=https://${IP}:9542 \
    --auth.keyfile=${CERTDIR}/client-auth-ca.keyfile \
    --verbose
```