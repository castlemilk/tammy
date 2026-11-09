#!/usr/bin/env bash
set -euo pipefail

# Cuttlefish's Linux VMs use the host architecture. Install the same pinned Go
# toolchain on either supported architecture and verify the official archive.
case "$(uname -m)" in
  aarch64|arm64)
    go_arch=arm64
    go_sha256=ef758ae7c6cf9267c9c0ef080b8965f453d89ab2d25d9eb22de4405925238768
    ;;
  x86_64|amd64)
    go_arch=amd64
    go_sha256=1153d3d50e0ac764b447adfe05c2bcf08e889d42a02e0fe0259bd47f6733ad7f
    ;;
  *) echo "CUTTLEFISH_LINUX_ARCH_UNSUPPORTED" >&2; exit 1 ;;
esac

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y --no-install-recommends \
  ca-certificates curl git build-essential pkg-config xauth xvfb \
  libasound2 libatk-bridge2.0-0 libatk1.0-0 libatspi2.0-0 libcups2 \
  libdbus-1-3 libdrm2 libgbm1 libgtk-3-0 libnspr4 libnss3 libx11-xcb1 \
  libxcomposite1 libxdamage1 libxfixes3 libxkbcommon0 libxrandr2

go_archive="$(mktemp)"
trap 'rm -f "$go_archive"' EXIT
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
  "https://go.dev/dl/go1.26.4.linux-${go_arch}.tar.gz" -o "$go_archive"
printf '%s  %s\n' "$go_sha256" "$go_archive" | sha256sum --check --status
tar -xzf "$go_archive" -C /usr/local
export PATH="/usr/local/go/bin:$PATH"
test "$(go version | awk '{print $3}')" = go1.26.4
corepack enable pnpm
corepack prepare pnpm@11.15.0 --activate
GOBIN=/usr/local/bin go install github.com/go-task/task/v3/cmd/task@v3.52.0
test "$(task --version)" = "3.52.0"

# Electron refuses to start as root. Run the existing scenarios as the image's
# unprivileged node user without weakening Chromium's sandbox settings.
subject_revision="${CUTTLE_GITHUB_SHA:-$(git rev-parse HEAD)}"
chown -R node:node "$PWD"
runuser -u node -- env HOME=/home/node CI=true \
  TAMMY_EVIDENCE_SUBJECT_REVISION="$subject_revision" \
  PATH="$PATH" bash -euo pipefail -c '
    pnpm install --frozen-lockfile
    task ci:contracts
    task ci:linux
  '
