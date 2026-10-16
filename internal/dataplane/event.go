package dataplane

import (
	"bytes"
	"encoding/binary"

	bpf "rendolnet/internal/dataplane/ebpf"
)

func decodeEvent(raw []byte, e *bpf.RendolEvent) error {
	return binary.Read(bytes.NewReader(raw), binary.NativeEndian, e)
}
