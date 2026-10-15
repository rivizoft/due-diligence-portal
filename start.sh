#!/usr/bin/env bash
# Start the Due Diligence Portal with self-signed TLS.
# Reads JWT secret from secret.txt (manual generation: umask 077; openssl rand -hex 32 > secret.txt)
set -euo pipefail
umask 077

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SECRET_FILE="${SCRIPT_DIR}/secret.txt"

if [ -L "$SECRET_FILE" ] || { [ -e "$SECRET_FILE" ] && [ ! -f "$SECRET_FILE" ]; }; then
    echo "JWT secret must be a regular file, not a symlink or device." >&2
    exit 1
fi

if [ ! -f "$SECRET_FILE" ]; then
    echo "Generating JWT secret..."
    SECRET_TMP=$(mktemp "${SCRIPT_DIR}/.secret.XXXXXX")
    trap 'rm -f -- "$SECRET_TMP"' EXIT
    openssl rand -hex 32 > "$SECRET_TMP"
    # Publish a complete private file without overwriting a concurrent creator.
    if ! ln -- "$SECRET_TMP" "$SECRET_FILE" 2>/dev/null; then
        if [ -L "$SECRET_FILE" ] || [ ! -f "$SECRET_FILE" ]; then
            echo "Unable to create a safe JWT secret file." >&2
            exit 1
        fi
    fi
    rm -f -- "$SECRET_TMP"
    trap - EXIT
    echo "Secret written to $SECRET_FILE"
fi

chmod 600 -- "$SECRET_FILE"
DD_JWT_SECRET=$(tr -d '[:space:]' < "$SECRET_FILE")
export DD_JWT_SECRET

docker compose -f "${SCRIPT_DIR}/docker/docker-compose.yml" up -d

echo ""
echo "Portal starting at https://localhost"
echo "Admin password (first run only):"
sleep 3
docker logs dd-portal 2>&1 | grep -i "admin password" || echo "  (already created — check earlier logs)"
