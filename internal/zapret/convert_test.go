package zapret

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestConvertFlowseal(t *testing.T) {
	raw, err := os.ReadFile("testdata/alt3.bat")
	if err != nil {
		t.Fatal(err)
	}
	s := Convert("general (ALT3)", string(raw))
	if !s.Compatible {
		t.Fatalf("стратегия признана непереносимой: %v", s.Problems)
	}
	if !slices.Contains(s.TCPPorts, "443") || !slices.Contains(s.UDPPorts, "50000-50100") {
		t.Fatalf("порты: tcp %v udp %v", s.TCPPorts, s.UDPPorts)
	}
	for _, a := range s.Args {
		if strings.HasPrefix(a, "--wf-") || strings.Contains(a, "%") || strings.Contains(a, "\"") {
			t.Errorf("аргумент не переведён: %q", a)
		}
	}
	if s.Args[0] != "--filter-udp=443" {
		t.Errorf("первый аргумент %q", s.Args[0])
	}
	if strings.Count(strings.Join(s.Args, " "), "--new") != 6 {
		t.Errorf("профилей должно остаться 7 (два игровых отброшены): %d разделителей", strings.Count(strings.Join(s.Args, " "), "--new"))
	}
	exp := strings.Join(s.Expand("/opt/zapret/bin", "/opt/zapret/lists"), " ")
	if !strings.Contains(exp, "--hostlist=/opt/zapret/lists/list-general.txt") ||
		!strings.Contains(exp, "--dpi-desync-fake-quic=/opt/zapret/bin/quic_initial_www_google_com.bin") {
		t.Fatalf("пути не подставлены: %s", exp)
	}
}

func TestConvertRejectsWindowsOnly(t *testing.T) {
	s := Convert("x", `start "z" /min "%BIN%winws.exe" --wf-tcp=443 --wf-raw=foo --filter-tcp=443 --dpi-desync=fake`)
	if s.Compatible {
		t.Fatal("--wf-raw должен делать стратегию непереносимой")
	}
}

func TestSplit(t *testing.T) {
	got := split(`--a="x y" --b=c  "--d" --e=^!`)
	want := []string{"--a=x y", "--b=c", "--d", "--e=!"}
	if !slices.Equal(got, want) {
		t.Fatalf("%q", got)
	}
}

func TestRules(t *testing.T) {
	r := Rules("eth1", Strategy{TCPPorts: []string{"80", "443"}, UDPPorts: []string{"443", "50000-50100"}})
	for _, want := range []string{
		`oifname "eth1" meta mark & 0x40000000 == 0 tcp dport { 80, 443 } ct original packets 1-6 queue flags bypass to 200`,
		`udp dport { 443, 50000-50100 }`,
		"delete table inet rendol_zapret",
	} {
		if !strings.Contains(r, want) {
			t.Errorf("нет %q в\n%s", want, r)
		}
	}
}
