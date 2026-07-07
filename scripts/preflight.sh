#!/usr/bin/env bash
# Goodman preflight ("doctor"): checks that this machine can build and run
# Goodman, and prints clear, actionable guidance for anything missing.
# Exit code 0 = ready to build; non-zero = something required is missing.
set -uo pipefail

# ---- pretty output ---------------------------------------------------------
if [[ -t 1 ]]; then
  G=$'\e[32m'; Y=$'\e[33m'; R=$'\e[31m'; B=$'\e[1m'; D=$'\e[2m'; X=$'\e[0m'
else
  G=""; Y=""; R=""; B=""; D=""; X=""
fi
ok()   { printf "  ${G}✓${X} %s\n" "$1"; }
warn() { printf "  ${Y}!${X} %s\n" "$1"; WARNED=1; }
bad()  { printf "  ${R}✗${X} %s\n" "$1"; FAILED=1; }
hint() { printf "      ${D}%s${X}\n" "$1"; }
hdr()  { printf "\n${B}%s${X}\n" "$1"; }

FAILED=0; WARNED=0

have() { command -v "$1" >/dev/null 2>&1; }
ver()  { "$@" 2>&1 | head -1; }

printf "${B}Goodman preflight${X}\n"
printf "${D}Checks whether this host can build and run Goodman.${X}\n"

# ---- platform --------------------------------------------------------------
hdr "Platform"
OS="$(uname -s)"; ARCH="$(uname -m)"
if [[ "$OS" == "Linux" ]]; then ok "OS: Linux"; else
  bad "OS: $OS — Goodman's sensor needs a real Linux kernel."
  hint "On macOS/Windows, provision a Linux VM (multipass/UTM/cloud) and work there."
fi
if [[ "$ARCH" == "x86_64" ]]; then ok "Arch: x86_64"; else
  warn "Arch: $ARCH — v1 targets x86-64; the eBPF object is built with -D__TARGET_ARCH_x86."
fi
[[ "$OS" == "Linux" ]] && ok "Kernel: $(uname -r)"

# ---- build toolchain -------------------------------------------------------
hdr "Build toolchain (needed for: make build)"
for t in go clang bpftool; do
  if have "$t"; then
    case "$t" in
      clang) ok "clang — $(ver clang --version)";;
      go)    ok "go — $(ver go version)";;
      *)     ok "$t — $(ver "$t" version 2>/dev/null | head -1)";;
    esac
  else
    bad "$t not found"
    case "$t" in
      go)      hint "Install Go 1.23+: https://go.dev/dl/  (or: sudo apt-get install -y golang)";;
      clang)   hint "sudo apt-get install -y clang llvm";;
      bpftool) hint "sudo apt-get install -y bpftool  (or linux-tools-\$(uname -r))";;
    esac
  fi
done
# libbpf headers are vendored, but the eBPF build also wants system dev headers
if [[ -e /usr/include/bpf/bpf_helpers.h ]] || [[ -e bpf/include/bpf/bpf_helpers.h ]]; then
  ok "libbpf headers available (vendored in bpf/include)"
else
  warn "libbpf headers not found system-wide (vendored copy is used, so this is usually fine)"
  hint "If the eBPF build fails: sudo apt-get install -y libbpf-dev libelf-dev"
fi

# ---- dashboard toolchain ---------------------------------------------------
hdr "Dashboard toolchain (needed for: make dashboard — the built UI is committed, so optional)"
if have node; then ok "node — $(ver node --version)"; else
  warn "node not found — only needed to rebuild the dashboard; the built UI is committed"
  hint "curl -fsSL https://deb.nodesource.com/setup_20.x | sudo -E bash - && sudo apt-get install -y nodejs"
fi
have npm && ok "npm — $(ver npm --version)" || true

# ---- kernel eBPF support ---------------------------------------------------
hdr "Kernel eBPF support (needed for: the live sensor / make e2e)"
if [[ "$OS" == "Linux" ]]; then
  if [[ -r /sys/kernel/btf/vmlinux ]]; then ok "BTF present: /sys/kernel/btf/vmlinux"; else
    bad "no /sys/kernel/btf/vmlinux — CO-RE needs kernel BTF"
    hint "Need a kernel built with CONFIG_DEBUG_INFO_BTF=y (most distros ≥5.8 have it)."
  fi
  CFG="/boot/config-$(uname -r)"
  reader="grep"; [[ -r /proc/config.gz ]] && { CFG="/proc/config.gz"; reader="zgrep"; }
  if [[ -r "$CFG" ]]; then
    for opt in CONFIG_BPF=y CONFIG_BPF_SYSCALL=y CONFIG_BPF_JIT=y; do
      if $reader -q "^$opt" "$CFG" 2>/dev/null; then ok "$opt"; else warn "$opt not set in $CFG"; fi
    done
  else
    warn "kernel config not readable ($CFG) — skipping CONFIG_* checks"
  fi
  # unprivileged bpf + capabilities
  UBD="$(cat /proc/sys/kernel/unprivileged_bpf_disabled 2>/dev/null || echo '?')"
  if [[ "$(id -u)" -eq 0 ]]; then
    ok "running as root — the sensor can load eBPF"
  elif [[ "$UBD" == "0" ]]; then
    ok "unprivileged BPF allowed (unprivileged_bpf_disabled=0)"
  else
    warn "unprivileged BPF disabled (=$UBD) — run the sensor with sudo/root, or 'sudo make e2e'"
    hint "make smoke needs no root and exercises the whole backend."
  fi
fi

# ---- optional: containers / k8s -------------------------------------------
hdr "Optional (deployment)"
for t in docker helm kubectl kind; do
  if have "$t"; then ok "$t — $(ver "$t" version 2>/dev/null | head -1)"; else
    warn "$t not found (only needed for: make docker / make kind-e2e / Helm install)"
  fi
done

# ---- verdict ---------------------------------------------------------------
hdr "Verdict"
if [[ "$FAILED" -eq 1 ]]; then
  printf "  ${R}${B}Not ready.${X} Install the ${R}✗${X} items above, then re-run ${B}make doctor${X}.\n\n"
  exit 1
elif [[ "$WARNED" -eq 1 ]]; then
  printf "  ${Y}${B}Ready to build.${X} Some ${Y}!${X} items limit optional features (see above).\n"
  printf "  Next: ${B}make build && make test && make smoke${X}\n\n"
  exit 0
else
  printf "  ${G}${B}All systems go.${X} Next: ${B}make build && make test && make smoke${X}\n"
  printf "  For the live eBPF demo: ${B}sudo make e2e${X}\n\n"
  exit 0
fi
