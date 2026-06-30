#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if ! command -v makensis >/dev/null 2>&1; then
  echo "makensis not found. Install NSIS first, for example:"
  echo "  sudo pacman -S nsis"
  echo "  sudo apt-get install nsis"
  exit 1
fi

if [ ! -f "release/VClaw/vclaw.exe" ]; then
  echo "release/VClaw/vclaw.exe not found."
  echo "Build and assemble the Windows release folder first."
  exit 1
fi

makensis release/VClawInstaller.nsi

echo
echo "Created: release/VClaw-Setup.exe"
