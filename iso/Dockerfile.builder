# Сборщик ISO. Без --privileged и без устройств: CAP_SYS_ADMIN нужен только
# mmdebstrap, чтобы смонтировать /proc и /sys внутри chroot.
FROM debian:trixie
ARG APT_MIRROR=
ARG APT_SECURITY_MIRROR=
RUN echo 'Acquire::Retries "5";' > /etc/apt/apt.conf.d/80retries \
 && S=/etc/apt/sources.list.d/debian.sources \
 && sed -i -e "s#^URIs: http://deb.debian.org/debian\$#URIs: ${APT_MIRROR:-http://deb.debian.org/debian}#" \
           -e "s#^URIs: http://deb.debian.org/debian-security\$#URIs: ${APT_SECURITY_MIRROR:-http://deb.debian.org/debian-security}#" $S \
 && apt-get update \
 && apt-get install -y --no-install-recommends \
      mmdebstrap uidmap squashfs-tools squashfs-tools-ng xorriso mtools dosfstools \
      grub-pc-bin grub-efi-amd64-bin grub-common ca-certificates zstd \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /work
ENTRYPOINT ["/bin/bash", "/src/iso/build.sh"]
