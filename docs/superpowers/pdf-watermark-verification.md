# PDF watermark verification — 2026-10-14

Implemented spec: `specs/2026-10-14-pdf-watermark-design.md`.

## Checks

- Go 1.26.8: `go test ./...` passed; `go vet ./...` passed.
- Service `go test -race ./internal/service/pdf` passed, including deduplication, waiter cancellation, noncancellable deadline and artifact leases.
- UI: 32 Vitest tests passed; svelte-check: 0 errors/0 warnings; ESLint and production build passed.
- Six Chromium scenarios passed: image-only new tab/lazy loading; browser PDF mode; preparation error/retry; download pending/error/retry; image-page error with no PDF fallback; Blob URL release on scroll/unmount.
- Live nonroot server/API smoke used real JWTs, real SQLite, uploaded PDF and actual Poppler rendering. View-only grants yielded PNG, all four PDF-byte routes denied access, and upgrading the grant enabled PDF mode. Watermark text included the authenticated email and date; original text stayed extractable.
- Production browser smoke passed using the built static UI and real backend/JWT, including actual watermarked PNG/PDF rendering and CSP headers; the test user completed the existing NDA gate.
- Full Docker build attempted with the session CA mounted only for networked steps. It was blocked fetching `https://dl-cdn.alpinelinux.org/alpine/v3.21/...`: the environment network policy denies that host. Alpine runtime package installation and UID 1001 image smoke remain unverified. Host tools and nonroot UID 1000 execution passed. This is a verification limitation, not a successful container build.

## Measurements

Linux amd64, Xeon Platinum 8573C, GOMAXPROCS=4; direct service benchmarks, excluding HTTP/SQLite fetch and browser transfer. Small fixture: 1,480 bytes, two pages including a rotated page. Near-limit fixture: 103,810,587 bytes (two pages plus a large unused uncompressed stream; tests byte handling, not visual complexity). Many-page fixture: 520,468 bytes, 500 pages with 100 vector lines per page. Samples: five iterations small, one large/many-page; timings are indicative, not an SLA. Peak RSS is the maximum observed child process RSS from a standalone benchmark binary, not a sum of simultaneous process RSS.

| Operation | Small PDF | Near-limit PDF | 500 pages |
|---|---:|---:|---:|
| Cold watermarked PDF | 31.9 ms | 1.344 s | 1.485 s |
| Cached PDF lookup | 0.018 ms | 0.088 ms | 0.074 ms |
| Cold PDF + first PNG page | 0.537 s | 3.002 s | 1.727 s |
| Cached PNG lookup | 0.029 ms | 0.099 ms | 0.729 ms |
| Peak child RSS for benchmark run | 26.3 MiB | 222.8 MiB | 71.6 MiB |

Concurrent same-key cache lookups and distinct-user preparation completed under the two-worker limit. Deterministic tests separately prove identical cold requests run one generation, queue overflow returns busy, and cancellation does not free a still-running worker. The cache byte-budget/lease tests verify cache accounting and temporary-file cleanup; no hard whole-process disk/memory cap is claimed. Cache hits avoid SQLite BLOB loads in delivery. The fixed default limits were retained; measurements do not justify increasing them.

## Limitations

Image previews add server work but render only requested pages. PNG text cannot be selected/searched, and fixed resolution limits zoom quality. Signed PDFs may lose signature validity after watermarking. Watermark percent escapes contain invisible separators to prevent pdfcpu's built-in `%p`/`%t` interpolation; displayed percent text remains literal. Download-byte authorization cannot revoke data already received by a browser. View-only users can save images or take screenshots.
