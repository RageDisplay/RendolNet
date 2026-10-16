package installer

import (
	"encoding/json"
	"strings"
	"testing"

	"rendolnet/internal/netcfg"
)

const lsblkOut = `{"blockdevices":[
 {"name":"sda","path":"/dev/sda","type":"disk","size":"34359738368","rota":"0","tran":"sata","model":"SSD","serial":"S1","ro":"0","mountpoints":[null]},
 {"name":"sdb","path":"/dev/sdb","type":"disk","size":"34359738368","rota":"1","model":"HDD","serial":"S2","ro":"0","mountpoints":[null],
  "children":[{"name":"sdb1","path":"/dev/sdb1","type":"part","size":"1000","fstype":"ext4","label":"old","mountpoints":[null]}]},
 {"name":"sdc","path":"/dev/sdc","type":"disk","size":"2000000000","ro":"0","mountpoints":[null]},
 {"name":"sr0","path":"/dev/sr0","type":"disk","size":"900000000","ro":"0","mountpoints":["/run/live/medium"]}
]}`

func answers() Answers {
	return Answers{
		Hostname: "fw1", Disk: "/dev/sda", Mgmt: "eno1", MgmtAddr: Addr{Address: "10.0.0.5/24", Gateway: "10.0.0.1"},
		WAN: "eno2", WANAddr: Addr{DHCP: true}, LAN: "eno3", LANAddr: "192.168.10.1/24",
	}
}

func TestClassification(t *testing.T) {
	disks, err := ParseDisks([]byte(lsblkOut), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]DiskState{"sda": DiskFree, "sdb": DiskHasData, "sdc": DiskBlocked, "sr0": DiskBlocked}
	for _, d := range disks {
		if want[d.Name] != d.State {
			t.Errorf("%s: %s, ожидалось %s (%s)", d.Name, d.State, want[d.Name], d.Reason)
		}
	}
}

func TestPlanRefusesDataDiskWithoutConfirm(t *testing.T) {
	disks, _ := ParseDisks([]byte(lsblkOut), nil)
	sdb, _ := FindDisk(disks, "sdb")
	if _, err := Plan(answers(), sdb); err == nil {
		t.Fatal("диск с данными принят без подтверждения")
	}
	a := answers()
	a.Confirm = "S2"
	if _, err := Plan(a, sdb); err != nil {
		t.Fatal(err)
	}
}

func TestPlanTouchesOnlyTarget(t *testing.T) {
	disks, _ := ParseDisks([]byte(lsblkOut), nil)
	sda, _ := FindDisk(disks, "sda")
	steps, err := Plan(answers(), sda)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		joined := strings.Join(s.Argv, " ")
		for _, other := range []string{"/dev/sdb", "/dev/sdc", "/dev/sr0"} {
			if strings.Contains(joined, other) {
				t.Errorf("шаг %q трогает %s", s.Title, other)
			}
		}
		if s.Kind == File && !strings.HasPrefix(s.Path, Target+"/") {
			t.Errorf("файл вне целевой системы: %s", s.Path)
		}
	}
}

func TestSeedConfigValid(t *testing.T) {
	b, err := SeedConfig(answers())
	if err != nil {
		t.Fatal(err)
	}
	var ap struct {
		Config netcfg.Config `json:"config"`
	}
	if err := json.Unmarshal(b, &ap); err != nil {
		t.Fatal(err)
	}
	if err := netcfg.Validate(&ap.Config); err != nil {
		t.Fatal(err)
	}
	if ap.Config.Management.Interface != "eno1" || len(ap.Config.NAT.Source) != 1 {
		t.Fatalf("конфиг: %+v", ap.Config.Management)
	}
}

func TestBootGuard(t *testing.T) {
	g := BootGuard([]string{"eno2", "eno3"})
	if !strings.Contains(g, `iifname { "eno2", "eno3" } drop`) || strings.Contains(g, "flush ruleset") {
		t.Fatalf("загрузочная защита:\n%s", g)
	}
}

func TestMgmtUnit(t *testing.T) {
	u := MgmtNetworkUnit("eno1", Addr{Address: "10.0.0.5/24", Gateway: "10.0.0.1", DNS: []string{"1.1.1.1"}})
	for _, want := range []string{"Name=eno1", "Address=10.0.0.5/24", "Gateway=10.0.0.1", "Metric=2048", "DNS=1.1.1.1"} {
		if !strings.Contains(u, want) {
			t.Errorf("нет %q в\n%s", want, u)
		}
	}
}

func TestAnswersValidation(t *testing.T) {
	a := answers()
	a.LAN = "eno2"
	if err := a.Validate(); err == nil {
		t.Error("один интерфейс в двух ролях принят")
	}
	a = answers()
	a.MgmtAddr = Addr{Address: "10.0.0.5"}
	if err := a.Validate(); err == nil {
		t.Error("адрес без маски принят")
	}
}

func TestAutoinstall(t *testing.T) {
	if AutoinstallRequested("quiet boot=live") || !AutoinstallRequested("boot=live rn.autoinstall=1") {
		t.Fatal("флаг автоустановки")
	}
	_, err := ParseAutoinstall([]byte("hostname: fw\nmgmt: eno1\nmgmt_addr: {dhcp: true}\n"))
	if err == nil {
		t.Fatal("автоустановка без диска принята")
	}
	a, err := ParseAutoinstall([]byte("hostname: fw\ndisk: /dev/disk/by-id/ata-x\nmgmt: eno1\nmgmt_addr: {dhcp: true}\nwan: eno2\nwan_addr: {dhcp: true}\n"))
	if err != nil || a.WAN != "eno2" {
		t.Fatalf("%v %+v", err, a)
	}
}
