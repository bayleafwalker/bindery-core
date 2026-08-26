#!/usr/bin/env bash
# Fetch the OpenTTD build that adapters/bindery-openttd-runtime is run against.
#
# The acceptance test drives a real game. This installs one into a cache
# directory owned by the user -- no package manager, no root, nothing on PATH --
# and prints the environment variable that points the test at it.
#
# Both downloads are pinned by the sha256 the OpenTTD project publishes in its
# own release manifests. The game binary's hash is what the run records as the
# session's content identity, so a silently different binary would be a
# silently different claim.
set -euo pipefail

OPENTTD_VERSION="${OPENTTD_VERSION:-15.3}"
OPENGFX_VERSION="${OPENGFX_VERSION:-8.0}"
OPENTTD_SHA256="f49eb25d61b00f8f4d332fee02b530ad75552d1efb8f2bb01e7ca5e6540fe059"
OPENGFX_SHA256="43a0c1dabf39cb865394f3a6cc36d4da5c10ecfaaf55652043104806810903be"

CACHE_DIR="${BINDERY_OPENTTD_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/bindery/openttd}"
INSTALL_DIR="${CACHE_DIR}/openttd-${OPENTTD_VERSION}-linux-generic-amd64"
BINARY="${INSTALL_DIR}/openttd"

verify() {
  local file="$1" want="$2" got
  got="$(sha256sum "$file" | cut -d' ' -f1)"
  if [[ "$got" != "$want" ]]; then
    echo "checksum mismatch for $file: got $got, want $want" >&2
    exit 1
  fi
}

if [[ -x "$BINARY" && -d "${INSTALL_DIR}/baseset/opengfx-${OPENGFX_VERSION}" ]]; then
  echo "already installed: $BINARY"
else
  mkdir -p "$CACHE_DIR"
  cd "$CACHE_DIR"

  game_archive="openttd-${OPENTTD_VERSION}-linux-generic-amd64.tar.xz"
  if [[ ! -f "$game_archive" ]]; then
    curl -fsSL -o "${game_archive}.part" \
      "https://cdn.openttd.org/openttd-releases/${OPENTTD_VERSION}/${game_archive}"
    mv "${game_archive}.part" "$game_archive"
  fi
  verify "$game_archive" "$OPENTTD_SHA256"
  rm -rf "$INSTALL_DIR"
  tar -xJf "$game_archive" -C "$CACHE_DIR"

  # The generic Linux build ships no usable base graphics set, and a dedicated
  # server refuses to start without one.
  graphics_archive="opengfx-${OPENGFX_VERSION}-all.zip"
  if [[ ! -f "$graphics_archive" ]]; then
    curl -fsSL -o "${graphics_archive}.part" \
      "https://cdn.openttd.org/opengfx-releases/${OPENGFX_VERSION}/${graphics_archive}"
    mv "${graphics_archive}.part" "$graphics_archive"
  fi
  verify "$graphics_archive" "$OPENGFX_SHA256"
  rm -rf "${CACHE_DIR}/opengfx"
  mkdir -p "${CACHE_DIR}/opengfx"
  unzip -oq "$graphics_archive" -d "${CACHE_DIR}/opengfx"
  tar -xf "${CACHE_DIR}/opengfx/opengfx-${OPENGFX_VERSION}.tar" -C "${CACHE_DIR}/opengfx"
  cp -r "${CACHE_DIR}/opengfx/opengfx-${OPENGFX_VERSION}" "${INSTALL_DIR}/baseset/"
fi

# Not piped into grep: grep exits on the first match, and the resulting SIGPIPE
# would fail this script under `set -o pipefail`.
help_output="$("$BINARY" --help 2>&1 || true)"
if [[ "$help_output" != *OpenGFX* ]]; then
  echo "installed OpenTTD does not see a usable graphics set" >&2
  exit 1
fi

echo "export BINDERY_OPENTTD_BIN=${BINARY}"
