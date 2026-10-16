package netcfg

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func seedConfig() Config {
	return Default(Seed{
		Hostname: "fw1", Mgmt: "eth0", WAN: "eth1", LAN: "eth2",
		LANAddr: "192.168.1.1/24", WANDHCP: true,
	})
}

func TestDefaultIsValid(t *testing.T) {
	c := seedConfig()
	if err := Validate(&c); err != nil {
		t.Fatalf("конфиг по умолчанию не проходит проверку: %v", err)
	}
}

func TestValidateCollectsAllErrors(t *testing.T) {
	c := seedConfig()
	c.Interfaces[1].Zone = "nowhere"
	c.Rules[0].Action = "maybe"
	c.Routes = append(c.Routes, Route{Destination: "10.0.0.0/8"})

	err := Validate(&c)
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("ожидалась ValidationError, получено %v", err)
	}
	want := []string{"interfaces[1].zone", "rules[0].action", "routes[0]"}
	for _, w := range want {
		found := false
		for _, fe := range ve.Errors {
			if fe.Path == w {
				found = true
			}
		}
		if !found {
			t.Errorf("нет ошибки для %s, есть: %v", w, ve.Errors)
		}
	}
}

func TestInterfaceWithoutAddress(t *testing.T) {
	c := seedConfig()
	c.Interfaces[2].Addresses = nil
	if err := Validate(&c); err != nil {
		t.Fatalf("сохранённый конфиг без адреса должен подниматься агентом: %v", err)
	}
	err := ValidateCommit(&c)
	if err == nil || !strings.Contains(err.Error(), "interfaces[2].addresses") {
		t.Fatalf("LAN без адреса принят: %v", err)
	}
}

func TestGroupCycle(t *testing.T) {
	c := seedConfig()
	c.Objects.Groups = []AddressGroup{
		{Name: "a", Members: []string{"b"}},
		{Name: "b", Members: []string{"a"}},
	}
	err := Validate(&c)
	if err == nil || !strings.Contains(err.Error(), "саму себя") {
		t.Fatalf("цикл в группах не пойман: %v", err)
	}
}

func TestResolveAddresses(t *testing.T) {
	c := seedConfig()
	c.Objects.Addresses = []AddressObject{
		{Name: "srv", Prefixes: []string{"10.0.0.5", "10.0.1.0/24"}},
		{Name: "v6", Prefixes: []string{"2001:db8::/32"}},
	}
	c.Objects.Groups = []AddressGroup{{Name: "all", Members: []string{"srv", "v6", "192.0.2.7"}}}
	r := NewResolver(&c)

	got, err := r.Addresses([]string{"all", "10.0.0.5"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"10.0.0.5/32", "10.0.1.0/24", "192.0.2.7/32", "2001:db8::/32"}
	if len(got) != len(want) {
		t.Fatalf("получено %v", got)
	}
	for i, w := range want {
		if got[i] != netip.MustParsePrefix(w) {
			t.Errorf("[%d] = %v, ожидалось %s", i, got[i], w)
		}
	}

	if any, _ := r.Addresses([]string{"any"}); any != nil {
		t.Errorf("any должен давать nil, получено %v", any)
	}
	if _, err := r.Addresses([]string{"nope"}); err == nil {
		t.Error("неизвестное имя должно быть ошибкой")
	}
}

func TestParseService(t *testing.T) {
	tests := []struct {
		proto Proto
		ports string
		want  []PortRange
		bad   bool
	}{
		{ProtoTCP, "80,443", []PortRange{{80, 80}, {443, 443}}, false},
		{ProtoUDP, "5000-5010", []PortRange{{5000, 5010}}, false},
		{ProtoTCP, "", nil, false},
		{ProtoTCP, "10-5", nil, true},
		{ProtoTCP, "0", nil, true},
		{ProtoICMP, "8", nil, true},
		{"gre", "", nil, true},
	}
	for _, tt := range tests {
		got, err := ParseService(tt.proto, tt.ports)
		if (err != nil) != tt.bad {
			t.Errorf("%s/%s: ошибка %v", tt.proto, tt.ports, err)
			continue
		}
		if tt.bad {
			continue
		}
		if len(got.Ports) != len(tt.want) {
			t.Errorf("%s/%s: %v", tt.proto, tt.ports, got.Ports)
			continue
		}
		for i := range tt.want {
			if got.Ports[i] != tt.want[i] {
				t.Errorf("%s/%s: %v", tt.proto, tt.ports, got.Ports)
			}
		}
	}
}

func TestDNATDuplicate(t *testing.T) {
	c := seedConfig()
	d := DNATRule{ID: "d1", Name: "web", Enabled: true, Interface: "eth1", Proto: ProtoTCP, Port: 443, ToAddress: "192.168.1.10"}
	c.NAT.Destination = []DNATRule{d, d}
	c.NAT.Destination[1].ID = "d2"
	if err := Validate(&c); err == nil || !strings.Contains(err.Error(), "уже пробрасывается") {
		t.Fatalf("дубль проброса не пойман: %v", err)
	}
}
