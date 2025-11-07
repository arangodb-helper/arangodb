# SSL Certificate Rotation in ArangoDB Starter

## Overview

This document explains how to update SSL/TLS certificates in ArangoDB clusters managed by the ArangoDB Starter.

## Quick Recommendation

** For most production deployments, use Option 1 (Graceful Restart).**

It provides the best balance of simplicity, reliability, and safety with only 30-60 seconds of planned downtime.

---

## Option Comparison

| Option | Downtime | Complexity | Reliability | Best For |
|--------|----------|------------|-------------|----------|
| **Option 1**: Graceful Restart | 30-60s | Low |  100% | **Most production use** |
| **Option 2**: Config File Update | 30-60s | High |  Error-prone | Path changes only |
| **Option 3**: Hot Reload API | None | Medium | Requires verification | Zero-downtime SLA |

---

# Option 1: Graceful Restart (Recommended)

## Why This Option

- **Simple**: Only 5 straightforward steps
- **Reliable**: Always successful when followed correctly
- **Safe**: No manual configuration file editing
- **Quick**: 30-60 seconds planned downtime
- **Clean**: Fresh configuration state
- **Easy Rollback**: Restore file and restart

## Procedure

### Step 1: Prepare New Certificate

```bash
# Generate or obtain your new certificate
openssl req -x509 -newkey rsa:4096 \
    -keyout /tmp/new-key.pem \
    -out /tmp/new-cert.pem \
    -days 365 -nodes \
    -subj "/CN=your-hostname/O=YourOrganization"

# Combine into ArangoDB keyfile format (certificate + private key)
cat /tmp/new-cert.pem /tmp/new-key.pem > /tmp/new-server.keyfile
chmod 600 /tmp/new-server.keyfile

# Verify the certificate
openssl x509 -in /tmp/new-server.keyfile -noout -subject -dates

# Backup current certificate
cp /path/to/current/server.keyfile \
   /path/to/current/server.keyfile.backup-$(date +%Y%m%d)
```

### Step 2: Replace Certificate at Same Path

```bash
# Replace the certificate file (cluster still running with old cert in memory)
cp /tmp/new-server.keyfile /path/to/current/server.keyfile

# Verify replacement
openssl x509 -in /path/to/current/server.keyfile -noout -subject
```

### Step 3: Graceful Cluster Shutdown

```bash
# Shutdown each node gracefully
# Adjust NODE and PORT for your environment
curl -k -X POST https://${NODE}:${STARTER_PORT}/shutdown

# Wait for clean shutdown
sleep 15

# Verify all stopped
ps aux | grep arangod | grep -v grep  # Should be empty
```
### Step 4: Restart Cluster

Restart using the **exact same commands** as original startup:

```bash
export STARTER=/usr/local/bin/arangodb

$STARTER \
    --ssl.keyfile=/path/to/current/server.keyfile \
    --starter.data-dir=/path/to/data-dir \
    --starter.port=${PORT} \
    --log.console=true
```

Wait for startup completion (~30 seconds):
```
Your cluster can now be accessed with a browser at `https://hostname:8529`
```

### Step 5: Verify New Certificate

```bash
# Check each server type (adjust NODE and PORT for your environment)
echo | openssl s_client -connect ${NODE}:${PORT} 2>/dev/null | \
    openssl x509 -noout -subject -dates

# Verify cluster health
curl -k -u root: https://${NODE}:8529/_admin/cluster/health
# Should return JSON with "Status": "GOOD"
```

**Default Ports**: Coordinator: 8529, DBServer: 8530, Agent: 8531

---

## Production Automation Script

```bash
#!/bin/bash
# rotate-certificate.sh - Automated certificate rotation
# Usage: ./rotate-certificate.sh /path/to/new/certificate.keyfile

set -e

# Configuration - Adjust for your environment
NODES=("node1.example.com" "node2.example.com" "node3.example.com")
STARTER_PORT="8528"
CERT_PATH="/path/to/production/server.keyfile"
DATA_DIRS=("/path/to/data-node1" "/path/to/data-node2" "/path/to/data-node3")

NEW_CERT="$1"

echo "=== ArangoDB Certificate Rotation ==="

# Backup
BACKUP="${CERT_PATH}.backup-$(date +%Y%m%d-%H%M%S)"
cp "$CERT_PATH" "$BACKUP"
echo "Backed up to: $BACKUP"

# Replace certificate
cp "$NEW_CERT" "$CERT_PATH"
chmod 600 "$CERT_PATH"
echo "Certificate replaced"

# Shutdown all nodes
echo "Shutting down cluster..."
for NODE in "${NODES[@]}"; do
    curl -k -X POST https://${NODE}:${STARTER_PORT}/shutdown || true
done
sleep 15

# Delete setup.json on all nodes
for DIR in "${DATA_DIRS[@]}"; do
    rm -f "${DIR}/setup.json"
done
echo "setup.json files deleted"

echo "Ready to restart cluster. Press Enter after restart..."
read

# Verify
echo "Verifying new certificate..."
for NODE in "${NODES[@]}"; do
    echo "$NODE:"
    echo | openssl s_client -connect ${NODE}:8529 2>/dev/null | \
        openssl x509 -noout -subject
done

echo "Certificate rotation complete"
```

---

# Option 2: Configuration File Update

##  Not Recommended - Use Only for Path Changes

This option requires manually editing multiple configuration files and is error-prone. Use only when the certificate path must change.

### Procedure

#### Step 1: Create New Certificate at Different Path

```bash
mkdir -p /new/certificate/path

openssl req -x509 -newkey rsa:4096 \
    -keyout /new/certificate/path/key.pem \
    -out /new/certificate/path/cert.pem \
    -days 730 -nodes \
    -subj "/CN=your-hostname/O=YourOrganization"

cat /new/certificate/path/cert.pem \
    /new/certificate/path/key.pem \
    > /new/certificate/path/server.keyfile

chmod 600 /new/certificate/path/server.keyfile
```

#### Step 2: Update setup.json Files

** Cluster is still running - be careful.**

```python
#!/usr/bin/env python3
# update-setup-json.py
import json, sys, shutil

setup_file, new_cert_path = sys.argv[1], sys.argv[2]

with open(setup_file, 'r') as f:
    config = json.load(f)

shutil.copy(setup_file, setup_file + '.backup')
config['ssl-keyfile'] = new_cert_path

with open(setup_file, 'w') as f:
    json.dump(config, f, indent=2)

print(f"Updated {setup_file}")
```

```bash
# Run for each node's setup.json
python3 update-setup-json.py /path/to/data-dir/setup.json /new/path/cert.keyfile

# Verify
grep "ssl-keyfile" /path/to/data-dir/setup.json
```

#### Step 3: Update arangod.conf Files

**Important**: Config uses `keyfile = ...` under `[ssl]` section.

```bash
#!/bin/bash
# update-arangod-conf.sh
CONF_FILE="$1"
NEW_PATH="$2"

[ ! -f "$CONF_FILE" ] && echo "Skip: $CONF_FILE" && exit 0

cp "$CONF_FILE" "${CONF_FILE}.backup"
sed -i "s|^keyfile.*|keyfile = ${NEW_PATH}|g" "$CONF_FILE"
echo "Updated: $CONF_FILE"
```

```bash
# Update all server instances (adjust paths for your environment)
./update-arangod-conf.sh /path/to/data-dir/coordinator*/arangod.conf /new/path/cert.keyfile
./update-arangod-conf.sh /path/to/data-dir/dbserver*/arangod.conf /new/path/cert.keyfile
./update-arangod-conf.sh /path/to/data-dir/agent*/arangod.conf /new/path/cert.keyfile
```

**Note**: Directory names include port numbers (e.g., `coordinator8529`)

#### Step 4: Restart Cluster

```bash
# Shutdown
curl -k -X POST https://${NODE}:${STARTER_PORT}/shutdown
sleep 15

# Restart with original commands (starter uses paths from config files)
$STARTER --ssl.keyfile=/old/path/cert.keyfile ...
```

#### Step 5: Verify New Certificate

```bash
echo | openssl s_client -connect ${NODE}:8529 2>/dev/null | \
    openssl x509 -noout -subject -dates
```

---

# Option 3: Hot Reload via API

## Zero Downtime with Connection Refresh

### When to Use

- Zero downtime is absolutely critical (strict SLA)
- Cannot schedule maintenance window
- Have monitoring to verify reload success

**For most deployments**: 30-60 second downtime of Option 1 is preferable due to simplicity.

### How It Works

The `/_admin/server/tls` API endpoint reloads the SSL certificate from disk without restarting. However:

-  New TLS connections immediately use the new certificate
-  Existing TLS connections cache the old certificate until they close

### Procedure

#### Step 1: Prepare and Replace Certificate

```bash
# Generate new certificate
openssl req -x509 -newkey rsa:4096 \
    -keyout /tmp/new-key.pem \
    -out /tmp/new-cert.pem \
    -days 365 -nodes \
    -subj "/CN=your-hostname/O=YourOrganization"

cat /tmp/new-cert.pem /tmp/new-key.pem > /tmp/new-server.keyfile
chmod 600 /tmp/new-server.keyfile

# Backup and replace (cluster stays running)
cp /path/to/current/server.keyfile \
   /path/to/current/server.keyfile.backup-$(date +%Y%m%d)

cp /tmp/new-server.keyfile /path/to/current/server.keyfile
```

**Note**: Certificate path must stay the same. Path changes not supported for hot reload.

#### Step 2: Trigger Hot Reload

```bash
# Reload all server types on each node
# Adjust NODE and PORTs for your environment
curl -k -u root: -X POST https://${NODE}:8529/_admin/server/tls  # Coordinator
curl -k -u root: -X POST https://${NODE}:8530/_admin/server/tls  # DBServer
curl -k -u root: -X POST https://${NODE}:8531/_admin/server/tls  # Agent

# Expected response: {"error":false,"code":200}
sleep 5
```

#### Step 3: Verify with Fresh Connections

**Important**: Must force new TLS connections:

```bash
# Force fresh connection to see new certificate
timeout 2 openssl s_client -connect ${NODE}:${STARTER_PORT}</dev/null 2>/dev/null | \
    openssl x509 -noout -subject -dates
```

#### Step 4: Verify Cluster Health

```bash
curl -k -u root: https://${NODE}:${STARTER_PORT}/_admin/cluster/health
```

#### Step 5: Fallback if Needed

If verification fails:

```bash
# Fall back to Option 1
curl -k -X POST https://${NODE}:${STARTER_PORT}/shutdown
sleep 15
# Restart cluster
```

### Understanding Connection Caching

After calling `/_admin/server/tls`:

1. Server immediately reloads certificate from disk 
2. New TLS connections use new certificate 
3. Existing TLS connections continue with cached old certificate 

This is standard TLS behavior. Applications will gradually reconnect and pick up new certificate.

---

# Summary

## For Most Deployments: Use Option 1

**Graceful Restart** is recommended for:
- Regular certificate renewals
- Emergency rotations (expiring cert)
- Production updates

**Why:**
- Simple and reliable (5 steps, 100% success)
- Minimal downtime (30-60 seconds)
- No manual config editing
- Easy rollback

## For Special Cases

**Use Option 3 (Hot Reload)** only when:
- Zero downtime SLA absolutely requires it
- You have verification and monitoring
- You understand connection caching

**Use Option 2 (Config Update)** only when:
- Certificate path must change
- Simplified variant won't work

## Key Points

1. **Option 1 is recommended** for 95% of certificate rotations
2. Brief downtime (30-60s) is acceptable and worth the simplicity
3. Hot reload works but adds complexity
4. Manual config editing should be avoided when possible
5. Always backup certificates before rotation
6. Always verify after rotation
