# ArangoDB Cluster Recovery After IP Address Change

## Context

After VM migration or network changes, ArangoDB **Agents persist peer IP addresses on disk** inside `setup.json`. Simply restarting the cluster with a new `--starter.join` value is **not sufficient**, because Agents restore their configuration from disk.

To recover the cluster **without data loss**, all persisted peer IP addresses must be updated to the new IPs.

This document covers **only the recovery steps**, assuming the cluster was already initialized earlier.

---

## Preconditions

* All ArangoDB Starters are **stopped**
* Data directories are intact
* New IP addresses are known

  * You can determine the new IPs using `ip addr show`, `hostname -I`, or your cloud provider console
* Environment variables are set:
  * `$DATA_DIR_X`
* Each data directory contains a `setup.json`

---

## Step 1: Backup Existing Configuration (Mandatory)

```bash
cp "$DATA_DIR_X/setup.json" "$DATA_DIR_X/setup.json.backup"
```

---

## Step 2: Verify Old IPs Are Still Present

```bash
grep -i "Address" "$DATA_DIR_X/setup.json"
```

You should still see **old IP addresses** at this stage.

---

# Approach A — Hard-coded Replacement Using `sed`

**Recommended for emergency recovery** where speed is critical.

> Risk Note: This approach performs raw string replacements and can corrupt JSON if the structure changes. Use only when `jq` or other JSON-aware tools are unavailable.

### Example IP Mapping

| Old IP    | New IP    |
| --------- | --------- |
| 127.0.0.1 | 127.0.1.1 |
| 127.0.0.2 | 127.0.1.2 |
| 127.0.0.3 | 127.0.1.3 |

> Note: Each `setup.json` contains **all cluster peers**, so **every file must be updated**.

---

## Update Member 1

```bash
sed -i 's/"127\.0\.0\.1"/"127.0.1.1"/g' "$DATA_DIR_X/setup.json"
sed -i 's/"127\.0\.0\.2"/"127.0.1.2"/g' "$DATA_DIR_X/setup.json"
sed -i 's/"127\.0\.0\.3"/"127.0.1.3"/g' "$DATA_DIR_X/setup.json"

# Handle localhost normalization (ports may vary; inspect setup.json before replacing)
sed -i 's/"localhost","Port":8628/"127.0.1.2","Port":8628/g' "$DATA_DIR_X/setup.json"
sed -i 's/"localhost","Port":8728/"127.0.1.3","Port":8728/g' "$DATA_DIR_X/setup.json"
```

Repeat this step for each member’s data directory.

---

## Verification (Required)

```bash
grep -o '"Address":"[^"]*"' "$DATA_DIR_X/setup.json"
```

Ensure **no old IPs remain**:

```bash
grep -i "127.0.0" "$DATA_DIR_X/setup.json" || echo "✓ All old IPs removed"
```

---

# Approach B — JSON-aware Update Using `jq`

**Recommended when `jq` is available** for safer, structure-aware updates.

> If `jq` is not available (e.g., minimal recovery environments), fall back to **Approach A** or use a small Python/Go JSON rewrite script.

---

## jq Update Function

```bash
update_setup_json_with_jq() {
  local file="$1"

  jq '
    walk(
      if type == "string" then
        gsub("127\\.0\\.0\\.1"; "127.0.1.1") |
        gsub("127\\.0\\.0\\.2"; "127.0.1.2") |
        gsub("127\\.0\\.0\\.3"; "127.0.1.3")
      else .
      end
    )
  ' "$file" > "${file}.tmp" && mv "${file}.tmp" "$file"
}
```

---

## Apply Update

```bash
update_setup_json_with_jq "$DATA_DIR_X/setup.json"
```

Repeat for each member’s data directory.

---

## Step 3: Restart Cluster Using New IPs

Start each ArangoDB Starter using the **new IP addresses** and correct `--starter.join` values.

Verify Agent endpoints:

```bash
ps aux | grep arangod | grep agency.endpoint | grep -v grep
```

Only **new IPs** should appear in the output.

### Additional Health Verification (Recommended)

```bash
curl -u root:<password> http://<coordinator-ip>:8529/_admin/cluster/health
```

Ensure all Agents, DBServers, and Coordinators report a healthy state.

---

## Expected Outcome

* Agents reconnect successfully using new IPs
* Coordinators and DBServers start normally
* Cluster becomes fully healthy
* No data loss occurs

> After recovery, consider validating data integrity using ArangoDB backup or consistency tools as part of post-incident checks.

---

## Rollback (If Needed)

```bash
mv "$DATA_DIR_X/setup.json.backup" "$DATA_DIR_X/setup.json"
```

Restart the cluster with the original configuration.

---

**End of Document**
