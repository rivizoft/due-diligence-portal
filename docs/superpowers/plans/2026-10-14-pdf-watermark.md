# PDF Watermark Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Apply configured personal watermarks to PDF downloads and both viewing modes without exposing PDF bytes to view-only users.

**Architecture:** A shared preparation service produces private cached PDF artifacts; Poppler renders individual pages for view-only access. Authenticated handlers validate permissions before preparation and again before delivery. A new Svelte route opens immediately in another tab, then shows either a browser PDF viewer or lazy page images according to server permissions.

**Tech Stack:** Go/Echo/SQLite, pdfcpu (pinned compatible version), Poppler pdfinfo/pdftoppm, bundled Unicode font with redistribution license, Svelte 5, Vitest, Playwright.

**Spec:** `docs/superpowers/specs/2026-10-14-pdf-watermark-design.md`

## Global Constraints

- Original stored bytes never change; apply to current and historical PDF versions only.
- AccessDownload is required for every PDF-byte endpoint. AccessView permits only metadata and page images. No PDF fallback for view-only users.
- PDF watermark remains searchable/selectable; raster preview does not provide search or selection.
- Defaults: two concurrent heavy operations, eight queued distinct operations, TTL 15 minutes, total disk cache 512 MiB, preparation deadline 60 seconds, input PDF limit 100 MiB.
- PNG preview: maximum 1600 pixels on the long edge and 4 million pixels per page; client cannot choose arbitrary DPI.
- Render date once per request as UTC YYYY-MM-DD; replace all four documented variables literally.
- JWT in Authorization only; private responses have Cache-Control: private, no-store.
- No background job API, polling, distributed cache, streaming PDF viewer, or full-document prerender.
- A PDF renderer without cancellation keeps its worker slot until it actually finishes. A raster subprocess is killed on deadline.
- User-visible strings follow the existing English UI.

## Review Focus

1. User can lose download permission between opening a tab and fetching bytes: PDF handlers must recheck and deny, with no client fallback.
2. Archived/deleted documents and historical versions must not remain accessible through cached artifacts.
3. Malformed, encrypted, extremely large page dimensions, and signed PDFs must fail predictably or preserve the documented warning; raster resource usage stays bounded.
4. Configuration edits in the same second and UTC date rollover must never reuse obsolete personalized artifacts.
5. Closing a tab or a failed first waiter must not strand a worker, leak temporary files, or cancel remaining waiters.

## File structure

- `internal/service/pdf/`: focused types/config, templates, watermark engine, Poppler wrapper, artifact cache, and orchestration, each with unit tests.
- `internal/handler/document_delivery.go`: common authorized file delivery and preview handlers; existing document handler retains CRUD and route registration.
- `internal/repository/document_repository.go`: metadata-only version lookup before loading heavy bytes.
- `ui/src/lib/api/client.ts`: shared authenticated binary fetch; existing JSON API unchanged.
- `ui/src/lib/components/PdfPreview.svelte`: view-only image navigation/loading.
- `ui/src/routes/documents/[id]/preview/+page.svelte`: status, permissions, browser viewer or image preview.
- Existing document list/detail and watermark settings pages: buttons/loading states and representative settings preview.
- `Dockerfile`, `go.mod`, `go.sum`, README and user/API docs: runtime/dependencies/configuration.
- `internal/service/pdf/testdata/`: small real PDFs, bundled font and its license; avoid large binary fixtures.

### Task 1: PDF watermark engine and deployment requirements

**Files:** Create `internal/service/pdf/types.go`, `config.go`, `template.go`, `watermark.go` and corresponding tests/testdata; modify `go.mod`, `go.sum`, `Dockerfile`.

**Interfaces:**
- `Identity { ID, Email, Name string }`, `Source { DocumentID, DocumentName string; Version int; Checksum string; Size int64; MimeType string }`.
- `RenderText(template string, identity Identity, documentName string, at time.Time) (string, error)`.
- `Watermarker.Apply(ctx context.Context, inputPath, outputPath string, config domain.WatermarkConfig, text string) error`.
- `Config { Workers, QueueSize int; CacheBytes, MaxInputBytes int64; TTL, Timeout time.Duration; CacheDir string }`; `LoadConfig() (Config,error)` reads DD_PDF_* variables, rejecting invalid explicit values.

- [x] Write `TestRenderText` asserting repeated variables, literal Unicode/name punctuation, UTC rollover and rejection of unknown/unclosed variables. Write real-PDF `TestWatermarker` asserting unchanged source SHA, all pages marked, readable original text, positions/opacity/size/color, rotated/landscape pages and long Cyrillic text. Assert malformed/encrypted input returns an error without output.
- [x] Run `go test ./internal/service/pdf -run 'TestRenderText|TestWatermarker|TestConfig' -v`; confirm failing tests exercise absent behavior before implementation.
- [x] Pin pdfcpu after checking API/Go compatibility and license; bundle a redistributable Unicode font and license. Implement named interfaces using pdfcpu text watermark with embedded font; use per-page fitting for long text. Preserve text, rotation and source bytes; check context before/after calls. Config defaults exactly match Global Constraints; use a private os.MkdirTemp cache directory.
- [x] Add Poppler and the resource-limiter executable to Alpine runtime dependencies, copy embedded font into the Go build inputs. Document dependency versions/licenses. Verify no required CGO or write to installed font directories under UID 1001.
- [x] Run engine tests, inspect rendered sample pages with pdftoppm and check extracted text with pdftotext. Commit `feat: add PDF watermark engine`.

### Task 2: Bounded artifact cache and orchestration

**Files:** Create `internal/service/pdf/cache.go`, `service.go`, `cache_test.go`, `service_test.go`; modify version repository and repository tests.

**Interfaces:**
- Repository `GetVersionMetadata(ctx context.Context, documentID string, version int) (*domain.DocumentVersion,error)` returns FileData=nil, size/MIME/checksum from SQLite; `GetVersion` remains the content loader.
- `Artifact { Path string; Size int64; Release func() }` is leased while serving, so eviction cannot remove an in-use file.
- `Service.Prepare(ctx context.Context, source Source, identity Identity, config domain.WatermarkConfig, at time.Time, load func(context.Context)([]byte,error)) (Artifact,error)`.
- `Cache.Do(ctx context.Context, key string, build func(context.Context,string) error) (Artifact,error)`; `Cache.Close() error` clears temporary artifacts after active users exit. Typed errors: ErrBusy, ErrTimeout, ErrTooLarge, ErrUnsupported, ErrRender.

- [x] Write `TestCache` with fake clock and barrier-controlled builders: identical requests call builder once; distinct keys allow two builders and eight queued operations; next is ErrBusy; TTL expires at 15 minutes; LRU byte budget enforced; partial/error outputs removed; leased file survives eviction; release deletes uncached oversized output. Write `TestPrepareKeys` for identity, source checksum/version, settings edited within one second, document rename, UTC date and renderer version.
- [x] Write `TestPrepareCancellation`: cancelling one waiter leaves the other successful; last waiter cancels supported work; noncancellable engine does not free its slot early. Assert size rejection occurs before loader, and cache hit skips loader. Verify metadata-only repository lookup does not read FileData.
- [x] Run `go test ./internal/service/pdf ./internal/repository -run 'TestCache|TestPrepare|TestDocumentRepository.*Metadata' -v`; confirm expected failures.
- [x] Implement SHA-256 keys from canonical structured fields, atomic private file publication, leases, LRU/TTL byte accounting and per-key waiter lifecycle. Acquire bounded admission before loading input bytes, validate loaded checksum and actual size, remove inputs on every path. No nested worker-slot acquisition; no unlimited goroutine-backed queue.
- [x] Run above tests and `go test -race ./internal/service/pdf`; commit `feat: cache and bound PDF preparation`.

### Task 3: Lazy raster preview

**Files:** Create `internal/service/pdf/poppler.go`, `raster.go`, `poppler_test.go`, `raster_test.go`; update service types/config and Dockerfile as needed.

**Interfaces:**
- `PageInfo { Width, Height float64 }`; `PreviewInfo { Pages []PageInfo }`.
- `Rasterizer.Info(ctx context.Context,inputPath string) (PreviewInfo,error)`.
- `Rasterizer.Page(ctx context.Context,inputPath,outputPath string,page int) error` accepts 1-based page, fixed bounds from Global Constraints.
- `Service.Info(ctx context.Context,pdf Artifact) (PreviewInfo,error)` and `Service.Page(ctx context.Context,pdf Artifact,page int) (Artifact,error)` use the same cache and worker admission as Task 2, with separate stage acquisition.

- [x] Write integration tests using real fixtures: page count/rotated sizes, PNG bounds, watermark baked into output, invalid page/huge dimensions/encryption rejected. Fake command-runner tests assert no shell invocation, fixed arguments, capped stdout/stderr, deadline actually kills process and no partial PNG is cached. Write lazy tests asserting requesting page 1 never renders pages 2+.
- [x] Run `go test ./internal/service/pdf -run 'TestRaster|TestPoppler' -v`; confirm failure.
- [x] Implement pdfinfo metadata parsing and pdftoppm `-f N -l N -singlefile -scale-to 1600 -png`. Run via exec.CommandContext under prlimit: initial per-process address-space 512 MiB, CPU 60 seconds, file-size 64 MiB, bounded captured output; check supported flags on Alpine and Linux. Remove process/output on cancellation. Validate page count/index and image dimensions. Image keys include prepared artifact identity, page, resolution, format and renderer version. Metadata parsing is admitted as heavy work too.
- [x] Ensure cached PDF lease lasts until raster job finishes; no nested worker acquisition or unauthorized direct static file serving.
- [x] Run raster tests and service race tests, verify runtime UID 1001 smoke test. Commit `feat: render private PDF preview pages`.

### Task 4: Authorized delivery endpoints and settings validation

**Files:** Create `internal/handler/document_delivery.go`, `document_delivery_test.go`; modify `document_handler.go`, `watermark_handler.go`, their tests and `cmd/main.go`; modify `docs/API.md`.

**Interfaces:**
- Inject pdf service and watermark repository into document delivery through an explicit constructor, update all callers.
- GET `/documents/:id/preview` and `/documents/:id/versions/:version/preview`: PDF inline, AccessDownload.
- GET `/documents/:id/preview-info` and `/documents/:id/versions/:version/preview-info`: AccessView; JSON `{mode: "pdf"|"images", version_number: number, pages: PageInfo[]}`. PDF mode does not need raster metadata; pages is empty.
- GET `/documents/:id/preview-pages/:page` and historical equivalent `/documents/:id/versions/:version/preview-pages/:page`: AccessView, image/png.
- Existing download endpoints: AccessDownload, attachment, watermark-enabled PDF through Prepare; non-PDF and disabled PDF preserve bytes.

- [x] Write handler tests for all routes, both role/grant modes and historical versions: view-only gets only images/metadata; direct PDF routes denied even on cache hit; no anonymous access; archived/deleted documents cannot leak through cache; revoke grant during barrier-controlled preparation and assert denial. Assert no binary body after failures, private/no-store and correct content type/disposition.
- [x] Write settings tests: unsupported placeholders, invalid/nonfinite numeric fields, malformed color and empty enabled template return 400; all supported existing settings still round-trip. All placeholder substitution stays literal.
- [x] Run `go test ./internal/handler -run 'TestDocument.*Delivery|TestWatermark' -v`; confirm failures.
- [x] Implement shared access/version/config resolution followed by service calls and final authorization check. Derive identity from JWT. Validate metadata/checksum before loading. Return 404 for denied access as existing convention, 413 for oversized input, 422 for unsupported/render errors, 503 + Retry-After for busy queue, 504 for timeout; use JSON envelope. Audit successful PDF delivery as download and image-page delivery as preview, never successful download on error. No external artifact path in API responses.
- [x] Wire config/service creation and cleanup at startup/shutdown. Update watermark validation and API docs, including signed-PDF warning and date format.
- [x] Run all handler/repository tests. Commit `feat: enforce PDF delivery permissions and watermarks`.

### Task 5: New-tab preview and download feedback

**Files:** Modify `ui/src/lib/api/client.ts`, `client.test.ts`, document list/detail pages and watermark settings; create `ui/src/lib/components/PdfPreview.svelte`, `ui/src/routes/documents/[id]/preview/+page.svelte`; add preview utility/unit tests and `ui/tests/pdf-preview.spec.ts` following existing Playwright layout.

**Interfaces:**
- `api.binary(path: string, options?: {signal?: AbortSignal}): Promise<{blob: Blob; filename: string|null}>` shares JWT refresh with download; `api.download` reuses it.
- Preview route optional version query parameter, checked positive integer. Resolve preview-info first and pin resolved version for subsequent calls. `mode` comes from server; never guess from local role or switch to PDF when images fail.
- `PdfPreview` consumes document ID, pinned version and pages; fetches authenticated page PNGs lazily, prefetches at most one next page.
- Effective can_download permission exposed in existing document metadata/list response using current access helper; backend remains authoritative. Historical Open PDF buttons use historical MIME type.

- [x] Write binary-client tests for success, JSON error propagation, one-shot refresh, abort and no JWT in URL. Write UI/e2e tests for immediate new tab/status, standard viewer only in pdf mode, view-only requests limited to info/PNG, first-page status then per-page retry, lazy next-page requests, no PDF fallback, document download pending/error/retry and expired permissions. Assert removal of object URLs on replacement/unmount.
- [x] Run `npm test -- --run` in ui and targeted Playwright test against mocked API to confirm failures before feature code.
- [x] Implement binary helper and new route with no-opener link target=_blank. Keep shell around PDF iframe/object and release Blob URLs on cleanup. In image mode provide page navigation without download/print controls, show readable status/error text and aria-live; AbortController cancels requests on unmount. Browser viewer fallback download is available only in pdf mode.
- [x] Add Open PDF links in list, detail and historical rows; show Download only when allowed. Download buttons disable independently while receiving bytes and show a spinner/text. Keep request errors visible with retry.
- [x] Replace settings demonstration with page-shaped sample showing position, opacity/font/color and all variables; label as illustrative. Explain both delivery modes, PDF-only scope, signed-document caveat and inability to prevent screenshots.
- [x] Run `npm test`, `npm run check`, `npm run build`, `npm run lint` and preview e2e tests. Commit `feat: add PDF viewing modes and preparation feedback`.

### Task 6: Performance verification and release documentation

**Files:** Create `internal/service/pdf/benchmark_test.go`; update `README.md`, `docs/USER_GUIDE.md`, `docs/API.md`; create `docs/superpowers/pdf-watermark-verification.md` with actual measured results.

- [x] Add benchmarks for small multi-page PDF cold/warm Prepare, first-page render, sequential page cache and same-key vs distinct-user concurrency. Generate large fixture under work/ near 100 MiB; do not commit it. Include a many-page/high-complexity fixture since byte size alone does not predict cost.
- [x] Run Go tests `go test ./...`, `go test -race ./internal/service/pdf`, UI validation from Task 5 and `go test ./internal/service/pdf -bench . -benchmem`. Measure peak RSS and temporary disk use with capped concurrency; record environment, fixture characteristics and timings, including first-page latency and warm response.
- [x] Build runtime image and smoke-test both modes as nonroot after reading applicable cloud Docker guidance. Test timeout/queue overflow and worker recovery. Verify deployment includes renderer/resource limiter/font licenses and readonly filesystem compatibility except explicit temp/data directories. If environment blocks a check, report it as unverified rather than passed.
- [x] Inspect network responses in browser as view-only: no application/pdf or source PDF URL; inspect actual raster watermark and browser PDF watermark across rotated/Unicode fixture pages. Confirm download grant and revoked grant behavior, old version, disabled settings and screenshots limitation.
- [x] Document all DD_PDF_* defaults, runtime binaries, per-process caches, date format, full-file fetch limitation, view-only lack of search/selection, input cap and errors. Record measured limits and any material deployment caveats. Adjust defaults only with spec review if measurements require scope/behavior changes.
- [x] Commit `docs: document and verify PDF watermark delivery`; perform whole-change review before reporting completion.

## Execution verification

Implementation tasks executed inline. Alpine image build/UID 1001 verification is blocked by environment network policy; host nonroot production smoke and all other checks passed. See `docs/superpowers/pdf-watermark-verification.md` for evidence and limits.

## Plan self-review

Spec coverage: engine/settings (1,4,5), originals/history/permissions (2,4,5), bounded personalized cache (2,3), image-only preview and limits (3,4,5), feedback/cleanup/accessibility (5), deployment/audit/docs/performance (1,3,4,6). Review Focus cases are assigned tests in Tasks 1–5. Interface names and 1-based page numbering are consistent. Execution starts only after plan review and execution-method selection.
