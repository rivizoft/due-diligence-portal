# Deployment

## Quick Start

```bash
# Generate a JWT secret
export DD_JWT_SECRET=$(openssl rand -hex 32)

# Start with self-signed TLS (default)
docker compose -f docker/docker-compose.yml up -d

# Or HTTP-only behind a load balancer
docker compose -f docker/docker-compose.http.yml up -d

# Or with your own TLS certificate
mkdir -p certs && cp your-cert.crt certs/tls.crt && cp your-key.key certs/tls.key
docker compose -f docker/docker-compose.tls.yml up -d
```

The admin account is created on first boot. Check logs for the generated password:

```bash
docker logs dd-portal 2>&1 | grep "admin password"
```

Or set it explicitly: `DD_ADMIN_PASSWORD=your-password`

## TLS Modes

| Mode | Env Var | Ports | Use Case |
| --- | --- | --- | --- |
| `self-signed` (default) | `DD_TLS_MODE=self-signed` | 443 (HTTPS) + 80 (redirect) | Development, internal |
| `custom` | `DD_TLS_MODE=custom` | 443 (HTTPS) + 80 (redirect) | Production with your cert |
| `none` | `DD_TLS_MODE=none` | 80 (HTTP) | Behind load balancer |

## Data Persistence

Metadata and default document originals are stored in SQLite at `/data/portal.db`.
When enabled through Admin > Storage, new originals are stored in a private S3
bucket; existing SQLite documents remain in the database.
Mount a Docker volume to persist across container restarts:

```yaml
volumes:
  - portal-data:/data
```

The volume contains:

```
/data/
  portal.db       # SQLite metadata, local document BLOBs, encrypted S3 settings
  portal.db-wal   # Write-ahead log
  portal.db-shm   # Shared memory
  storage.key     # Automatically generated S3 credential encryption key (0600)
  certs/           # Self-signed TLS certs (only in self-signed mode)
```

## Docker Image

Published to GitHub Container Registry on every push to `main`:

```bash
docker pull ghcr.io/witfoo/due-diligence-portal:latest
```

Tags:
- `latest` -- latest main branch build
- `main` -- same as latest
- `v1.0.0` -- release versions (created via `git tag v1.0.0 && git push --tags`)

## Health Checks

```bash
curl http://localhost:8080/health    # {"status":"healthy"}
curl http://localhost:8080/ready     # {"status":"ready","checks":{"sqlite":"ok"}}
curl http://localhost:8080/version   # {"version":"...","commit":"..."}
```

## Email Configuration

To enable email notifications (invites, Q&A alerts):

```yaml
environment:
  DD_SMTP_ENABLED: "true"
  DD_SMTP_HOST: smtp.example.com
  DD_SMTP_PORT: "587"
  DD_SMTP_USERNAME: user@example.com
  DD_SMTP_PASSWORD: your-password
  DD_SMTP_FROM: portal@example.com
```

## Backup

SQLite backup while the portal is running:

```bash
docker exec dd-portal sqlite3 /data/portal.db ".backup /data/backup.db"
docker cp dd-portal:/data/backup.db ./backup-$(date +%Y%m%d).db
```

If S3 is configured, also preserve `/data/storage.key` in a restricted backup
and back up the bucket objects. Restore the original key with its matching
database; do not generate a replacement key for existing encrypted settings.
Losing it prevents S3 reads and cleanup, while SQLite documents remain usable.
For a consistent backup across DB and object state, pause document writes and
cleanup (stop the portal), or use coordinated snapshots with bucket versioning
and retention. A database backup alone no longer contains S3 originals.

## S3 Storage

Connect storage entirely in **Admin > Storage**. There are no S3 configuration
environment variables. The server uses explicit saved credentials; it does not
discover AWS profiles, instance roles or metadata-service credentials.

1. Create a private bucket with public access blocked.
2. Create a dedicated key with object read, write and delete permissions. For
   AWS, use a policy like this, substituting your bucket and selected prefix:

   ```json
   {
     "Version": "2012-10-17",
     "Statement": [{
       "Effect": "Allow",
       "Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"],
       "Resource": "arn:aws:s3:::company-documents/documents/*"
     }]
   }
   ```

3. Enter Bucket, Region, Access Key ID and Secret Access Key. Leave Endpoint URL
   empty for AWS; for an S3-compatible service use its HTTPS endpoint and enable
   path-style addressing if the provider requires it. Use `documents/` as the
   Key prefix for the policy example above. KMS-encrypted buckets may also need
   key-specific KMS permissions according to their encryption configuration.
4. Test the connection, enable S3 for new uploads and save. Both operations
   verify Put/Get/Delete using a temporary object under the selected prefix.

The encryption key is generated automatically beside the configured database;
the standard `/data` volume already persists it. Use TLS for admin access and
restrict access to the volume and backups: possession of the database and
`storage.key` together permits decrypting saved credentials.

Disabling S3 redirects new uploads to SQLite; existing S3 versions continue to
use the saved connection. Leave both key fields empty to preserve the current
credentials; enter both to rotate them. Bucket, endpoint, region and addressing
cannot change while S3 versions or pending cleanup jobs exist. Prefix changes
affect only new objects. Credentials must retain access to all existing objects,
including those under older prefixes.

Downloads and PDF rendering run through the backend with the current access
rules. Do not make the bucket public; browser CORS is unnecessary. Permit
backend HTTPS egress to the configured S3 endpoint. Startup and SQLite reads
work while S3 is offline, but S3 uploads and reads return a controlled error.

Logical version deletion atomically queues physical object deletion; a worker
retries failures after restart with backoff. Archiving keeps all originals, and
the last version cannot be deleted. `DeleteObject` on a versioned bucket can
create a delete marker; provider versioning, retention and object-lock policies
determine when historical object bytes are physically removed.

An SQL failure after upload triggers compensating object deletion. A crash
between PutObject and SQL commit can leave an orphan object; this integration
does not promise a distributed transaction or scan the entire bucket. Failed
temporary-probe cleanup is reported by Test Connection and may require manual
cleanup. Monitor provider storage/audit tools for those cases. Existing upload
size limits and in-memory processing remain in effect; uploads are not resumable.
