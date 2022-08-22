# Run ArangoDB Starter with DC2DC locally

This example shows how to run an ArangoDB cluster using the Starter locally (twice) with datacenter
to datacenter replication between the 2 clusters.

Note: This example shares secrets between clusters, so it is NOT suitable for any kind of production use!

## Step 1: Create certificates & tokens

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

```bash
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
export DATADIR=<directories where to store database files>

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

```bash
arangosync configure sync \
    --master.endpoint=https://${IP}:9542 \
    --master.keyfile=${CERTDIR}/client-auth-ca.keyfile \
    --source.endpoint=https://${IP}:8542 \
    --source.cacert=${CERTDIR}/tls-ca.crt \
    --auth.keyfile=${CERTDIR}/client-auth-ca.keyfile
```

## Step 4: Check status of configuration

```bash
# Check status of cluster 1
arangosync get status \
    --master.endpoint=https://${IP}:8542 \
    --auth.keyfile=${CERTDIR}/client-auth-ca.keyfile \
    --verbose

# Check status of cluster 2
arangosync get status \
    --master.endpoint=https://${IP}:9542 \
    --auth.keyfile=${CERTDIR}/client-auth-ca.keyfile \
    --verbose
```

## Step 5: Check the status of synchronization for each shard

Target DC endpoint should be specified:
```
arangosync check sync \
    --master.endpoint=https://${IP}:9542 \
    --auth.keyfile=${CERTDIR}/client-auth-ca.keyfile \
    --verbose
```
