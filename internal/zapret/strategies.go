// Package zapret — стратегии обхода DPI для nfqws. Список собирается из
// репозитория Flowseal/zapret-discord-youtube скриптом
// scripts/import-zapret-strategies.sh и встраивается в бинарь.
package zapret

import (
	_ "embed"
	"encoding/json"
	"sort"
)

type Strategy struct {
	Name       string   `json:"name"`
	Source     string   `json:"source"`
	Args       []string `json:"args"`
	TCPPorts   []string `json:"tcp_ports"`
	UDPPorts   []string `json:"udp_ports"`
	Compatible bool     `json:"compatible"`
	Problems   []string `json:"problems,omitempty"`
}

//go:embed strategies.json
var raw []byte

func Strategies() []Strategy {
	var out []Strategy
	_ = json.Unmarshal(raw, &out)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func Find(name string) (Strategy, bool) {
	for _, s := range Strategies() {
		if s.Name == name {
			return s, true
		}
	}
	return Strategy{}, false
}
