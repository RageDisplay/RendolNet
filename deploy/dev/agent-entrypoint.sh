#!/bin/sh
# Стенд: два dummy-интерфейса вместо WAN и LAN, eth0 контейнера — управление.
set -e
mount -t bpf bpf /sys/fs/bpf 2>/dev/null || true
for spec in "wan0 203.0.113.2/24" "lan0 192.168.1.1/24"; do
  # shellcheck disable=SC2086 # нужна разбивка на имя и адрес
  set -- $spec
  ip link show "$1" >/dev/null 2>&1 || ip link add "$1" type dummy
  ip addr replace "$2" dev "$1"
  ip link set "$1" up
done
exec /app/rendol-agent
