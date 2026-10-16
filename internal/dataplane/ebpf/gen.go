// Package ebpf — сгенерированная обвязка программ из bpf/. Файлы *_bpfel.*
// лежат в репозитории, чтобы Go собирался без clang; перегенерация:
// make bpf (в контейнере, см. build/Dockerfile.bpf).
package ebpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target bpfel -type ct_key -type ct_val -type event -type iface -type rule -type pair_key -type pair_val -type set_key -type block_key -type dnat_key -type dnat_val -type snat_rule -type snat_key -type snat_val -type bank_cfg -type counter -type addr Rendol ../../../bpf/rendol.c -- -O2 -g -Wall -Wno-unused-variable -I../../../bpf -I/usr/include/x86_64-linux-gnu
