// Package netcfg описывает конфигурацию узла: интерфейсы, зоны, маршруты,
// объекты, правила и NAT. Один документ целиком — это одна версия конфига;
// версии хранит rendol-api, применяет rendol-agent.
package netcfg

const SchemaVersion = 1

type Config struct {
	Schema     int         `json:"schema"`
	System     System      `json:"system"`
	Zones      []Zone      `json:"zones"`
	Interfaces []Interface `json:"interfaces"`
	Routes     []Route     `json:"routes"`
	Objects    Objects     `json:"objects"`
	Rules      []Rule      `json:"rules"`
	NAT        NAT         `json:"nat"`
	Protection Protection  `json:"protection"`
	Management Management  `json:"management"`
	Zapret     Zapret      `json:"zapret"`
}

type System struct {
	Hostname string   `json:"hostname"`
	DNS      []string `json:"dns,omitempty"`
}

// Zone "self" зарезервирована: это сам узел, адресат трафика к нему.
type Zone struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

const ZoneSelf = "self"

type Role string

const (
	RoleWAN  Role = "wan"
	RoleLAN  Role = "lan"
	RoleMgmt Role = "mgmt"
)

type Interface struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Role        Role     `json:"role"`
	Zone        string   `json:"zone"`
	Enabled     bool     `json:"enabled"`
	DHCP        bool     `json:"dhcp,omitempty"`
	Addresses   []string `json:"addresses,omitempty"` // CIDR: 192.0.2.1/24
	MTU         int      `json:"mtu,omitempty"`
	VLAN        *VLAN    `json:"vlan,omitempty"`
}

type VLAN struct {
	Parent string `json:"parent"`
	ID     int    `json:"id"`
}

type Route struct {
	Destination string `json:"destination"` // CIDR, 0.0.0.0/0 — маршрут по умолчанию
	Gateway     string `json:"gateway,omitempty"`
	Interface   string `json:"interface,omitempty"`
	Metric      int    `json:"metric,omitempty"`
	Comment     string `json:"comment,omitempty"`
}

type Objects struct {
	Addresses []AddressObject `json:"addresses"`
	Groups    []AddressGroup  `json:"groups"`
	Services  []Service       `json:"services"`
}

type AddressObject struct {
	Name     string   `json:"name"`
	Prefixes []string `json:"prefixes"`
	Comment  string   `json:"comment,omitempty"`
}

type AddressGroup struct {
	Name    string   `json:"name"`
	Members []string `json:"members"`
	Comment string   `json:"comment,omitempty"`
}

type Proto string

const (
	ProtoAny    Proto = "any"
	ProtoTCP    Proto = "tcp"
	ProtoUDP    Proto = "udp"
	ProtoICMP   Proto = "icmp"
	ProtoICMPv6 Proto = "icmpv6"
)

// Ports: "80", "8000-8080", через запятую: "80,443,8000-8080".
type Service struct {
	Name    string `json:"name"`
	Proto   Proto  `json:"proto"`
	Ports   string `json:"ports,omitempty"`
	Comment string `json:"comment,omitempty"`
}

type Action string

const (
	ActionAllow  Action = "allow"
	ActionDeny   Action = "deny"
	ActionReject Action = "reject"
)

// Source и Destination — имена объектов, групп или литералы CIDR/адресов.
// Пустой список и "any" означают любой адрес. Services — имена сервисов или
// литералы вида "tcp/443", "udp/53", "icmp".
type Rule struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Enabled     bool     `json:"enabled"`
	From        []string `json:"from"`
	To          []string `json:"to"`
	Source      []string `json:"source,omitempty"`
	Destination []string `json:"destination,omitempty"`
	Services    []string `json:"services,omitempty"`
	Action      Action   `json:"action"`
	Log         bool     `json:"log,omitempty"`
	Comment     string   `json:"comment,omitempty"`
}

type NAT struct {
	Source      []SNATRule `json:"source"`
	Destination []DNATRule `json:"destination"`
}

// Пустой Translate — маскарад, адрес исходящего интерфейса.
type SNATRule struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Enabled   bool     `json:"enabled"`
	Interface string   `json:"interface"`
	Source    []string `json:"source,omitempty"`
	Translate string   `json:"translate,omitempty"`
	Comment   string   `json:"comment,omitempty"`
}

// Проброс порта с адреса входящего интерфейса. AllowPolicy создаёт
// разрешение автоматически, без отдельного правила в политике.
type DNATRule struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	Interface   string `json:"interface"`
	Proto       Proto  `json:"proto"`
	Port        int    `json:"port"`
	ToAddress   string `json:"to_address"`
	ToPort      int    `json:"to_port,omitempty"`
	AllowPolicy bool   `json:"allow_policy"`
	Comment     string `json:"comment,omitempty"`
}

type Protection struct {
	Blocklist []string `json:"blocklist,omitempty"`
	// Новых TCP-соединений в секунду с одного адреса. 0 — без ограничения.
	SYNRate  int `json:"syn_rate,omitempty"`
	SYNBurst int `json:"syn_burst,omitempty"`
	// Писать в журнал отбои по умолчанию (не попавшие ни под одно правило).
	LogDefaultDeny bool `json:"log_default_deny"`
}

// Доступ к самому узлу через интерфейс управления не зависит от политики:
// без этого одна неудачная правка правил отрезает администратора.
type Management struct {
	Interface string   `json:"interface"`
	AllowFrom []string `json:"allow_from,omitempty"`
	Ports     []int    `json:"ports"`
}

type Zapret struct {
	Enabled   bool   `json:"enabled"`
	Strategy  string `json:"strategy,omitempty"`
	Interface string `json:"interface,omitempty"`
	// Fooling заменяет fooling=ts в стратегии: ts работает, только если
	// клиенты сами ставят TCP timestamps. Пусто — как в стратегии.
	Fooling string   `json:"fooling,omitempty"`
	Hosts   []string `json:"hosts,omitempty"`
	IPs     []string `json:"ips,omitempty"`
}
