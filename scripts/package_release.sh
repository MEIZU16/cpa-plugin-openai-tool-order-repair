#!/usr/bin/env bash
set -euo pipefail

PLUGIN_ID="openai-tool-order-repair"
VERSION="${1:-0.1.0}"
TARGET_GOOS="${2:-linux}"
TARGET_GOARCH="${3:-amd64}"

case "$TARGET_GOOS" in
  darwin)
    LIB_NAME="${PLUGIN_ID}.dylib"
    ;;
  linux)
    LIB_NAME="${PLUGIN_ID}.so"
    ;;
  windows)
    LIB_NAME="${PLUGIN_ID}.dll"
    ;;
  *)
    echo "unsupported goos: ${TARGET_GOOS}" >&2
    exit 1
    ;;
esac

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${ROOT_DIR}/dist"
WORK_DIR="${DIST_DIR}/${PLUGIN_ID}_${VERSION}_${TARGET_GOOS}_${TARGET_GOARCH}"
ZIP_NAME="${PLUGIN_ID}_${VERSION}_${TARGET_GOOS}_${TARGET_GOARCH}.zip"
ZIP_PATH="${DIST_DIR}/${ZIP_NAME}"

rm -rf "$WORK_DIR"
mkdir -p "$WORK_DIR" "$DIST_DIR"

(
  cd "$ROOT_DIR"
  CGO_ENABLED=1 GOOS="$TARGET_GOOS" GOARCH="$TARGET_GOARCH" \
    go build -buildmode=c-shared -o "${WORK_DIR}/${LIB_NAME}" .
)

rm -f "$ZIP_PATH"
python3 - "$WORK_DIR" "$LIB_NAME" "$ZIP_PATH" <<'PY'
import pathlib
import sys
import zipfile

work_dir = pathlib.Path(sys.argv[1])
lib_name = sys.argv[2]
zip_path = pathlib.Path(sys.argv[3])

with zipfile.ZipFile(zip_path, "w", compression=zipfile.ZIP_DEFLATED) as zf:
    zf.write(work_dir / lib_name, arcname=lib_name)
PY

(
  cd "$DIST_DIR"
  sha256sum "$ZIP_NAME" > checksums.txt
)

echo "created ${ZIP_PATH}"
echo "created ${DIST_DIR}/checksums.txt"
