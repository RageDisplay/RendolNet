#!/usr/bin/env bash
# mkiso.sh <дерево> <версия> <файл.iso>
# Гибридный образ: CD/DVD (BIOS и UEFI), запись на флешку побайтно (dd, Rufus DD)
# и копирование файлов на FAT-флешку (Rufus ISO, Ventoy). GRUB ищет свои файлы
# по файлу-метке версии, а не по UUID носителя.
set -euo pipefail

TREE="${1:?usage: mkiso.sh <tree> <version> <output.iso>}"
VERSION="${2:?version}"
ISO="${3:?output}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

MARK="/.rendolnet-$VERSION"
touch "$TREE$MARK"

mkdir -p "$TREE/boot/grub/i386-pc" "$TREE/boot/grub/x86_64-efi" "$TREE/EFI/BOOT"
cp /usr/lib/grub/i386-pc/*.mod /usr/lib/grub/i386-pc/*.lst "$TREE/boot/grub/i386-pc/"
cp /usr/lib/grub/x86_64-efi/*.mod /usr/lib/grub/x86_64-efi/*.lst "$TREE/boot/grub/x86_64-efi/"

cat > "$WORK/early.cfg" <<EOF
search --no-floppy --set=root --file $MARK
set prefix=(\$root)/boot/grub
EOF

FIND="search search_fs_file normal configfile part_msdos part_gpt fat iso9660"
# shellcheck disable=SC2086 # word-split module list
grub-mkimage -O i386-pc-eltorito -c "$WORK/early.cfg" -p /boot/grub \
  -o "$TREE/boot/grub/bios.img" biosdisk $FIND
# shellcheck disable=SC2086
grub-mkimage -O x86_64-efi -c "$WORK/early.cfg" -p /boot/grub \
  -o "$TREE/EFI/BOOT/BOOTX64.EFI" $FIND

size_kib=$(( $(stat -c %s "$TREE/EFI/BOOT/BOOTX64.EFI") / 1024 + 1024 ))
mkfs.vfat -C -n RN-EFI "$WORK/efi.img" "$size_kib" >/dev/null
mmd -i "$WORK/efi.img" ::/EFI ::/EFI/BOOT
mcopy -i "$WORK/efi.img" "$TREE/EFI/BOOT/BOOTX64.EFI" ::/EFI/BOOT/BOOTX64.EFI

cat > "$TREE/boot/grub/grub.cfg" <<EOF
insmod all_video
insmod serial
set timeout=5
set default=0
serial --unit=0 --speed=115200
terminal_input console serial
terminal_output console serial
menuentry "Install RendolNet $VERSION" {
  linux /live/vmlinuz boot=live components quiet console=ttyS0,115200 console=tty1
  initrd /live/initrd.img
}
menuentry "Install RendolNet $VERSION (autoinstall from RNAUTO volume)" {
  linux /live/vmlinuz boot=live components quiet rn.autoinstall=1 console=ttyS0,115200 console=tty1
  initrd /live/initrd.img
}
menuentry "Install RendolNet $VERSION (safe graphics)" {
  linux /live/vmlinuz boot=live components nomodeset console=tty1
  initrd /live/initrd.img
}
EOF

xorriso -as mkisofs -r -J -joliet-long -iso-level 3 -V RENDOLNET \
  --grub2-mbr /usr/lib/grub/i386-pc/boot_hybrid.img \
  -partition_offset 16 --mbr-force-bootable \
  -append_partition 2 0xef "$WORK/efi.img" -appended_part_as_gpt \
  -c /boot/grub/boot.cat \
  -b /boot/grub/bios.img -no-emul-boot -boot-load-size 4 -boot-info-table --grub2-boot-info \
  -eltorito-alt-boot -e --interval:appended_partition_2:all:: -no-emul-boot \
  -o "$ISO" "$TREE" 2>"$WORK/xorriso.log" || { cat "$WORK/xorriso.log" >&2; exit 1; }
