package agent

// Диагностика с узла: то, что администратор иначе делал бы из консоли.

type PingReply struct {
	Seq   int     `json:"seq"`
	RTTms float64 `json:"rtt_ms,omitempty"`
	Lost  bool    `json:"lost,omitempty"`
}

type PingResult struct {
	Target   string      `json:"target"`
	Address  string      `json:"address"`
	Replies  []PingReply `json:"replies"`
	Received int         `json:"received"`
}

type Hop struct {
	TTL     int     `json:"ttl"`
	Address string  `json:"address,omitempty"` // пусто — ответа не было
	RTTms   float64 `json:"rtt_ms,omitempty"`
}

type TraceResult struct {
	Target  string `json:"target"`
	Address string `json:"address"`
	Hops    []Hop  `json:"hops"`
	Reached bool   `json:"reached"`
}

type RouteEntry struct {
	Destination string `json:"destination"`
	Gateway     string `json:"gateway,omitempty"`
	Interface   string `json:"interface,omitempty"`
	Source      string `json:"source,omitempty"`
	Protocol    string `json:"protocol"`
	Metric      int    `json:"metric,omitempty"`
}

type Neighbor struct {
	Address   string `json:"address"`
	MAC       string `json:"mac,omitempty"`
	Interface string `json:"interface"`
	State     string `json:"state"`
}

type LookupResult struct {
	Name      string   `json:"name"`
	Server    string   `json:"server"`
	Addresses []string `json:"addresses"`
	RTTms     float64  `json:"rtt_ms"`
}
