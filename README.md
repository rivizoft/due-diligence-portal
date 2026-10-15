# Due Diligence Portal

A self-hosted portal for companies raising capital to securely share due diligence
documents with investors. Packaged as a single Docker image with a Go API backend
and Svelte 5 frontend.

![Documents View](docs/screenshots/02-documents.png)

## Features

- **English and Russian** -- Browser language detection, persistent language switcher, localized dates and file sizes
- **Document management** -- Upload, categorize, version, and search documents with FTS5
- **Optional S3 storage** -- Connect a private AWS S3 or compatible bucket in Admin > Storage; no S3 environment variables
- **Granular permissions** -- Per-document and per-category access grants with expiration
- **Audit logging** -- Immutable log of every access, download, and change
- **NDA gates** -- Require electronic NDA signatures before document access
- **Q&A workflow** -- Structured investor questions with company responses
- **Activity analytics** -- Per-document and per-investor engagement tracking
- **White-label branding** -- Custom colors, logos, CSS, and watermarks
- **Email notifications** -- SMTP-based invites, Q&A alerts, and NDA notifications
- **Air-gapped deployment** -- Self-hosted assets; keep optional S3 and email integrations disabled for disconnected use

## Quick Start

```bash
# Generate a secret and start with self-signed TLS
export DD_JWT_SECRET=$(openssl rand -hex 32)
docker compose -f docker/docker-compose.yml up -d

# Check the logs for the admin password
docker logs dd-portal 2>&1 | grep "admin password"
```

Then open `https://localhost` (accept the self-signed certificate warning).

## Deployment Options

| Mode | Command | Use Case |
| --- | --- | --- |
| Self-signed TLS | `docker compose -f docker/docker-compose.yml up -d` | Development, internal |
| Custom TLS cert | `docker compose -f docker/docker-compose.tls.yml up -d` | Production |
| HTTP (behind LB) | `docker compose -f docker/docker-compose.http.yml up -d` | Load-balanced |

See [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) for full configuration.

## Architecture

```text
Browser --> Go Echo (8080/8443) --> SQLite
              |
              +--> /api/v1/*     REST API (JWT auth)
              +--> /*            SvelteKit static SPA
```

Single Docker image. Single process. Single SQLite database file.
Documents use SQLite BLOBs by default. Admin > Storage can enable S3 for new
documents and versions; metadata and existing SQLite originals stay in the database.
Private originals are always delivered through the portal's access checks.

To connect S3, create a private bucket and an access key with `s3:GetObject`,
`s3:PutObject` and `s3:DeleteObject` for the chosen bucket/prefix. Enter the
bucket, region and keys in Admin > Storage, test the connection, enable S3 and
save. For an S3-compatible service, set its HTTPS endpoint and optional
path-style addressing. Keys are encrypted using an automatically generated
`storage.key` beside the database. Back up that file, the database and bucket
objects together; see [deployment guidance](docs/DEPLOYMENT.md#s3-storage).

### Tech Stack

| Layer | Technology |
| --- | --- |
| Backend | Go 1.26, Echo v4 |
| Frontend | Svelte 5, SvelteKit, Carbon Design System |
| Database | SQLite (modernc.org/sqlite, pure Go) |
| Auth | JWT (HS256), bcrypt, invite-only registration |
| Search | SQLite FTS5 full-text search |
| Containerization | Multi-stage Docker (Alpine 3.21) |
| CI/CD | GitHub Actions, GitHub Container Registry |

### Roles

| Role | Can Do |
| --- | --- |
| **Admin** | Everything -- manage users, documents, categories, NDA templates, branding |
| **Company Member** | Upload and manage documents, respond to Q&A |
| **Investor** | View granted documents, ask questions, sign NDAs |

## Development

```bash
# Backend
go build -o portal ./cmd
DD_TLS_MODE=none DD_DB_PATH=./dev.db ./portal

# Frontend (separate terminal, proxies API to :8080)
cd ui && npm install && npm run dev

# Tests
go test ./...                        # 195 Go unit tests
cd ui && npm test                    # 10 Vitest unit tests
./scripts/testing/system-tests.sh    # 60 Docker-based API tests
./scripts/testing/e2e-tests.sh       # 20 Playwright E2E tests
```

## Documentation

| Document | Description |
| --- | --- |
| [docs/USER_GUIDE.md](docs/USER_GUIDE.md) | User manual with annotated screenshots |
| [docs/API.md](docs/API.md) | REST API reference (60+ endpoints) |
| [docs/ENVIRONMENT.md](docs/ENVIRONMENT.md) | All 20 environment variables |
| [docs/TESTING.md](docs/TESTING.md) | Test pyramid, scripts, patterns |
| [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) | Docker, TLS, backup, email |
| [CLAUDE.md](CLAUDE.md) | AI development guide |

## License

[Apache License 2.0](LICENSE)

### PDF watermarks and view-only preview

Enable PDF watermarks in Admin → Watermark. Delivery uses a private server-side
cache of personalized copies; uploaded originals are preserved. Open PDF starts
preparation in a new tab. Download shows preparation in the clicked button.
With download access, the tab uses the browser PDF viewer. With view-only access,
it requests server-rendered PNG pages, including the watermark, without exposing
the PDF file. Screenshots and saving visible images remain possible.

The runtime requires `pdfinfo`, `pdftoppm` (Poppler) and `prlimit` (util-linux);
the Dockerfile installs them and `font-dejavu` for source PDFs with unembedded fonts. The embedded DejaVu Sans font supports Latin and
Cyrillic; its redistribution notice is in `internal/service/pdf/assets/`.
pdfcpu is Apache-2.0; Poppler and util-linux are separately installed runtime
tools under their respective licenses. Advanced text shaping and arbitrary
scripts are not guaranteed by this font/renderer combination.

| Environment variable | Default | Meaning |
|---|---|---|
| `DD_PDF_WORKERS` | `2` | Maximum simultaneous heavy operations per process |
| `DD_PDF_QUEUE_SIZE` | `8` | Additional distinct queued operations |
| `DD_PDF_CACHE_BYTES` | `536870912` | LRU budget for cached PDF, metadata and PNG files |
| `DD_PDF_CACHE_TTL` | `15m` | Artifact lifetime |
| `DD_PDF_TIMEOUT` | `60s` | API preparation deadline and per-stage job deadline |
| `DD_PDF_MAX_INPUT_BYTES` | `104857600` | Input limit for watermarked PDF or image preview |
| `DD_PDF_CACHE_DIR` | system temp | Parent of the private temporary artifact directory |

Invalid explicit settings fail startup. Use a writable private temporary
filesystem; generated cache files are not served statically and are removed on
normal shutdown/eviction. Crash leftovers can be cleared while the server is
stopped. Caches and limits are per process, not distributed. LRU/TTL apply to
cached files: in-use/uncached files and transient input files can temporarily
exceed the cache budget. Limits do not constitute a whole-process memory cap;
The content service still loads an admitted original into memory, from SQLite or S3.

PNG pages load lazily, at most 1600 pixels on the long edge. Raster subprocesses
have fixed caps: 512 MiB address space, 60 CPU seconds, 64 MiB output, and are
killed on timeout. Preview metadata is capped at 10,000 pages and 14,400 points
per page dimension. The in-process PDF engine cannot be forcibly interrupted;
a timed-out request stops waiting, but its worker remains occupied until the
engine finishes. Do not increase input/worker limits without workload measurements.

In `DD_INVESTOR_ACCESS=all`, ungranted investors have download access, while
explicit view-only grants restrict downloads. Use `DD_INVESTOR_ACCESS=granted`
when access must always require a grant. Every binary route independently checks the current permission.

After building the final image, run `scripts/testing/pdf-runtime.sh IMAGE` to
check rasterized original text and watermark pixels as UID 1001 with a readonly
root filesystem. It needs the Go toolchain and Docker on the test host.
