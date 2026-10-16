#!/usr/bin/env bash
# Собирает ISO внутри iso/Dockerfile.builder. Запускается из scripts/build-iso.sh.
#
#   /src  репозиторий (только чтение)
#   /out  bundle/ (распакованный пакет), k3s/ (k3s и его образы), сюда же ISO
set -euo pipefail

VERSION="${1:?build.sh <версия>}"
OUT=/out
WORK=/work
TREE="$WORK/iso"
ISO="$OUT/rendolnet-$VERSION-amd64.iso"

info() { echo "==> $*"; }
[ -f "$OUT/bundle/manifest.json" ] || { echo "нет $OUT/bundle" >&2; exit 1; }
[ -x "$OUT/k3s/k3s" ] || { echo "нет $OUT/k3s/k3s" >&2; exit 1; }

PACKAGES=(
  systemd-sysv systemd-timesyncd udev dbus kmod iproute2 iputils-ping ca-certificates curl less nano
  openssh-server console-setup keyboard-configuration locales initramfs-tools zstd
  linux-image-amd64 firmware-realtek firmware-bnx2 firmware-bnx2x
  gdisk parted dosfstools e2fsprogs squashfs-tools util-linux
  grub-efi-amd64-bin grub-pc-bin grub2-common efibootmgr
  live-boot live-boot-initramfs-tools
  nftables conntrack ethtool tcpdump
  unattended-upgrades
  qemu-guest-agent open-vm-tools hyperv-daemons
)

info "корневая система (Debian 13, mmdebstrap)"
rm -rf "$TREE"
mkdir -p "$TREE/live" "$TREE/boot/grub" /cache
MAIN="${RN_APT_MIRROR:-http://deb.debian.org/debian}"
SEC="${RN_APT_SECURITY_MIRROR:-http://deb.debian.org/debian-security}"
SOURCES=("deb $MAIN trixie main contrib non-free-firmware"
  "deb $MAIN trixie-updates main contrib non-free-firmware"
  "deb $SEC trixie-security main contrib non-free-firmware")
info "пакеты из $MAIN"

# Хуки в одинарных кавычках: "$1" раскрывает mmdebstrap (корень chroot).
# shellcheck disable=SC2016
mmdebstrap --mode=root --variant=apt --format=squashfs --aptopt='Acquire::Retries "5"' \
  --include="$(IFS=,; echo "${PACKAGES[*]}")" \
  --setup-hook='mkdir -p "$1/var/cache/apt/archives"' \
  --setup-hook='sync-in /cache /var/cache/apt/archives' \
  --essential-hook='mkdir -p "$1/etc/default"' \
  --customize-hook='sync-out /var/cache/apt/archives /cache' \
  --customize-hook='copy-in /src/iso/rootfs/etc /' \
  --customize-hook="copy-in $OUT/bundle/host/usr/local/sbin /usr/local" \
  --customize-hook="copy-in $OUT/k3s/k3s /usr/local/bin" \
  --customize-hook='set -e
    B=/out/bundle
    mkdir -p "$1/var/lib/rancher/k3s/agent/images" "$1/var/lib/rancher/k3s/server/manifests" "$1/var/lib/rendolnet/host"
    cp /out/k3s/k3s-airgap-images-amd64.tar.zst "$1/var/lib/rancher/k3s/agent/images/"
    cp "$B/images.tar.zst" "$1/var/lib/rancher/k3s/agent/images/rendolnet-images.tar.zst"
    cp "$B/manifests/rendolnet.yaml" "$1/var/lib/rancher/k3s/server/manifests/rendolnet.yaml"
    sed -n "s/.*\"version\": *\"\([^\"]*\)\".*/\1/p" "$B/manifest.json" > "$1/var/lib/rendolnet/host/version"
    chmod 0755 "$1"/usr/local/sbin/rendol-* "$1/usr/local/sbin/rendolctl" "$1/usr/local/bin/k3s"
    ln -sf k3s "$1/usr/local/bin/kubectl"
    ln -sf k3s "$1/usr/local/bin/ctr"
    touch "$1/etc/rendolnet-live"
    chroot "$1" systemctl enable systemd-networkd
    chroot "$1" systemctl disable ssh.service || true
    echo "en_US.UTF-8 UTF-8" > "$1/etc/locale.gen"; echo "ru_RU.UTF-8 UTF-8" >> "$1/etc/locale.gen"
    chroot "$1" locale-gen >/dev/null
    echo "LANG=C.UTF-8" > "$1/etc/default/locale"
    sed -i "s/^CODESET=.*/CODESET=\"CyrSlav\"/; s/^FONTFACE=.*/FONTFACE=\"Terminus\"/" "$1/etc/default/console-setup" || true
    echo rendolnet > "$1/etc/hostname"
    printf "[Match]\nType=ether\n\n[Network]\nDHCP=yes\n" > "$1/etc/systemd/network/99-live.network"
    chroot "$1" passwd -l root
    chroot "$1" update-initramfs -u -k all
    rm -f "$1"/var/cache/apt/archives/*.deb
    rm -rf "$1"/var/lib/apt/lists/*
    # на узле всегда публичные источники Debian, каким бы ни было зеркало сборки
    rm -f "$1/etc/apt/sources.list"
    printf "%s\n" "Types: deb" "URIs: http://deb.debian.org/debian" "Suites: trixie trixie-updates" \
      "Components: main contrib non-free-firmware" "Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg" "" \
      "Types: deb" "URIs: http://deb.debian.org/debian-security" "Suites: trixie-security" \
      "Components: main contrib non-free-firmware" "Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg" \
      > "$1/etc/apt/sources.list.d/debian.sources"' \
  --customize-hook='cp "$1"/boot/vmlinuz-* /work/iso/live/vmlinuz; cp "$1"/boot/initrd.img-* /work/iso/live/initrd.img' \
  trixie "$TREE/live/filesystem.squashfs" "${SOURCES[@]}"

info "ISO"
bash /src/iso/mkiso.sh "$TREE" "$VERSION" "$ISO"
(cd "$OUT" && sha256sum "$(basename "$ISO")" > "$(basename "$ISO").sha256")
ls -lh "$ISO"
