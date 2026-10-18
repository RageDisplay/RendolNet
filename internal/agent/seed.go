package agent

import (
	"fmt"
	"strings"
	"time"

	"rendolnet/internal/netcfg"
)

// ParseSeed разбирает строку вида "mgmt=eth0,wan=eth1,lan=eth2,lan_addr=192.168.1.1/24".
// Так задаётся стартовый конфиг при установке без ISO и на стенде. lan можно
// повторять, lan_addr относится к последнему lan.
func ParseSeed(s string) (netcfg.Seed, error) {
	seed := netcfg.Seed{Hostname: "rendolnet", WANDHCP: true}
	for _, kv := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(kv), "=")
		if !ok {
			return seed, fmt.Errorf("agent: в seed ожидается ключ=значение: %q", kv)
		}
		switch k {
		case "hostname":
			seed.Hostname = v
		case "mgmt":
			seed.Mgmt = v
		case "wan":
			seed.WAN = v
		case "lan":
			seed.LANs = append(seed.LANs, netcfg.SeedLAN{Name: v})
		case "lan_addr":
			if len(seed.LANs) == 0 {
				return seed, fmt.Errorf("agent: lan_addr без lan")
			}
			seed.LANs[len(seed.LANs)-1].Addr = v
		case "wan_addr":
			seed.WANAddr = v
			seed.WANDHCP = false
		case "gateway":
			seed.Gateway = v
		default:
			return seed, fmt.Errorf("agent: неизвестный ключ seed %q", k)
		}
	}
	return seed, nil
}

// SeedIfEmpty записывает стартовый конфиг, если подтверждённого ещё нет.
func SeedIfEmpty(stateDir string, seed netcfg.Seed) (bool, error) {
	st := store{dir: stateDir}
	if cur, err := st.load("confirmed.json"); err != nil || cur != nil {
		return false, err
	}
	c := netcfg.Default(seed)
	if err := netcfg.Validate(&c); err != nil {
		return false, err
	}
	return true, st.save("confirmed.json", &Applied{Config: c, Applied: time.Now()})
}
