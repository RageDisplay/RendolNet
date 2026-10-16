package installer

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type NIC struct {
	Name    string
	MAC     string
	Driver  string
	Carrier bool
	Speed   int
}

// NICs перечисляет физические сетевые карты: у виртуальных интерфейсов
// (lo, мосты, туннели) нет каталога device в sysfs.
func NICs(sysClassNet string) []NIC {
	entries, _ := os.ReadDir(sysClassNet)
	var out []NIC
	for _, e := range entries {
		dir := filepath.Join(sysClassNet, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "device")); err != nil {
			continue
		}
		if t := read(filepath.Join(dir, "type")); t != "1" { // ARPHRD_ETHER
			continue
		}
		n := NIC{Name: e.Name(), MAC: read(filepath.Join(dir, "address")), Carrier: read(filepath.Join(dir, "carrier")) == "1"}
		if link, err := os.Readlink(filepath.Join(dir, "device", "driver")); err == nil {
			n.Driver = filepath.Base(link)
		}
		n.Speed, _ = strconv.Atoi(read(filepath.Join(dir, "speed")))
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func read(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
