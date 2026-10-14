#!/usr/bin/env bash
# Use after building the final portal image. This checks actual Alpine libraries,
# fonts and a readonly UID 1001 runtime, independent of host Poppler/font fallbacks.
set -euo pipefail
image="${1:-portal:latest}"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
scratch="$(mktemp -d "${TMPDIR:-/tmp}/portal-pdf-runtime.XXXXXX")"
trap 'rm -rf "$scratch"' EXIT
chmod 755 "$scratch"
cd "$repo_root"
CGO_ENABLED=0 go test -c -o "$scratch/pdf.test" ./internal/service/pdf
chmod 755 "$scratch/pdf.test"
cp -R internal/service/pdf/testdata "$scratch/testdata"
chmod 755 "$scratch/testdata"
chmod 644 "$scratch/testdata"/*.pdf
docker run --rm --read-only --user 1001:1001 \
  --tmpfs /tmp:rw,noexec,nosuid,size=512m,mode=1777 \
  --mount "type=bind,src=$scratch/pdf.test,dst=/checks/pdf.test,readonly" \
  --mount "type=bind,src=$scratch/testdata,dst=/checks/testdata,readonly" \
  --workdir /checks --entrypoint /checks/pdf.test "$image" \
  -test.run '^TestPopplerOriginalTextWithAndWithoutWatermark$' -test.v
