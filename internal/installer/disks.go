// Package installer ставит RendolNet с live-ISO на системный диск.
//
// Всё разрушительное защищено дважды: список дисков решает, что вообще можно
// предложить, а план отказывается от неподходящего диска, что бы ни пришло
// из интерфейса или autoinstall.yaml.
package installer

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const MinSystemDisk = 8 << 30

// LiveMediumMount — куда live-boot монтирует носитель, с которого загрузились.
const LiveMediumMount = "/run/live/medium"

type DiskState string

const (
	DiskFree    DiskState = "free"
	DiskHasData DiskState = "has_data" // выбирается только после ввода серийного номера
	DiskBlocked DiskState = "blocked"
)

type Disk struct {
	Name       string
	Path       string
	ByID       string // /dev/disk/by-id/…, используется во всех командах
	Model      string
	Serial     string
	SizeBytes  uint64
	Rotational bool
	Transport  string
	State      DiskState
	Reason     string
	Contents   []string
}

type lsblkDev struct {
	Name        string     `json:"name"`
	Path        string     `json:"path"`
	Type        string     `json:"type"`
	Size        jsonNumber `json:"size"`
	Rota        jsonBool   `json:"rota"`
	Tran        *string    `json:"tran"`
	Model       *string    `json:"model"`
	Serial      *string    `json:"serial"`
	FSType      *string    `json:"fstype"`
	Label       *string    `json:"label"`
	Mountpoints []*string  `json:"mountpoints"`
	RO          jsonBool   `json:"ro"`
	Children    []lsblkDev `json:"children"`
}

var LsblkArgs = []string{"-J", "-b", "-o", "NAME,PATH,TYPE,SIZE,ROTA,TRAN,MODEL,SERIAL,FSTYPE,LABEL,MOUNTPOINTS,RO"}

// Разные версии lsblk пишут числа и булевы то строкой, то как есть.
type jsonNumber uint64

func (n *jsonNumber) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" || s == "" {
		return nil
	}
	var v uint64
	_, err := fmt.Sscan(s, &v)
	*n = jsonNumber(v)
	return err
}

type jsonBool bool

func (v *jsonBool) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	*v = jsonBool(s == "true" || s == "1")
	return nil
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

func walk(d lsblkDev, f func(lsblkDev)) {
	f(d)
	for _, c := range d.Children {
		walk(c, f)
	}
}

func ParseDisks(lsblkJSON []byte, byID map[string]string) ([]Disk, error) {
	var out struct {
		Devices []lsblkDev `json:"blockdevices"`
	}
	if err := json.Unmarshal(lsblkJSON, &out); err != nil {
		return nil, fmt.Errorf("installer: вывод lsblk: %w", err)
	}
	var disks []Disk
	for _, d := range out.Devices {
		if d.Type != "disk" || strings.HasPrefix(d.Name, "zram") || strings.HasPrefix(d.Name, "ram") {
			continue
		}
		disk := Disk{Name: d.Name, Path: d.Path, ByID: byID[d.Name], Model: str(d.Model), Serial: str(d.Serial),
			SizeBytes: uint64(d.Size), Rotational: bool(d.Rota), Transport: str(d.Tran), State: DiskFree}
		if disk.Path == "" {
			disk.Path = "/dev/" + d.Name
		}
		var mounts, contents []string
		parts := 0
		walk(d, func(x lsblkDev) {
			for _, m := range x.Mountpoints {
				if m != nil && *m != "" {
					mounts = append(mounts, *m)
				}
			}
			if x.Type == "part" {
				parts++
			}
			if fs := str(x.FSType); fs != "" {
				c := fs
				if l := str(x.Label); l != "" {
					c += " «" + l + "»"
				}
				contents = append(contents, c)
			}
		})
		disk.Contents = contents
		switch {
		case containsPrefix(mounts, LiveMediumMount):
			disk.State, disk.Reason = DiskBlocked, "установочный носитель"
		case len(mounts) > 0:
			disk.State, disk.Reason = DiskBlocked, "смонтирован: "+strings.Join(mounts, ", ")
		case bool(d.RO):
			disk.State, disk.Reason = DiskBlocked, "только чтение"
		case disk.SizeBytes < MinSystemDisk:
			disk.State, disk.Reason = DiskBlocked, "меньше 8 ГиБ"
		case parts > 0 || len(contents) > 0:
			disk.State = DiskHasData
			disk.Reason = fmt.Sprintf("разделов: %d", parts)
			if len(contents) > 0 {
				disk.Reason += ", " + strings.Join(contents, ", ")
			}
		}
		disks = append(disks, disk)
	}
	sort.Slice(disks, func(i, j int) bool { return disks[i].Name < disks[j].Name })
	return disks, nil
}

func containsPrefix(list []string, prefix string) bool {
	for _, s := range list {
		if s == prefix || strings.HasPrefix(s, prefix+"/") {
			return true
		}
	}
	return false
}

// ByIDLinks выбирает для каждого диска самую узнаваемую ссылку by-id:
// модель с серийным номером лучше, чем wwn-.
func ByIDLinks(links map[string]string) map[string]string {
	rank := func(l string) int {
		if strings.HasPrefix(l, "wwn-") || strings.HasPrefix(l, "nvme-eui.") || strings.HasPrefix(l, "nvme-nvme.") {
			return 1
		}
		return 2
	}
	best := map[string]string{}
	for link, target := range links {
		if strings.Contains(link, "-part") {
			continue
		}
		dev := target[strings.LastIndex(target, "/")+1:]
		cur, ok := best[dev]
		if !ok || rank(link) > rank(cur) || (rank(link) == rank(cur) && link < cur) {
			best[dev] = link
		}
	}
	for dev, link := range best {
		best[dev] = "/dev/disk/by-id/" + link
	}
	return best
}

// ConfirmationFor — что нужно ввести, чтобы стереть диск с данными:
// серийный номер, а если его нет (виртуальные диски) — размер в байтах.
func ConfirmationFor(d Disk) string {
	if d.Serial != "" {
		return d.Serial
	}
	return fmt.Sprint(d.SizeBytes)
}

func CheckTarget(d Disk, confirm string) error {
	switch d.State {
	case DiskBlocked:
		return fmt.Errorf("%s нельзя использовать: %s", d.Name, d.Reason)
	case DiskHasData:
		if !strings.EqualFold(strings.TrimSpace(confirm), ConfirmationFor(d)) {
			return fmt.Errorf("на %s есть данные (%s), подтверждение не совпало", d.Name, d.Reason)
		}
	}
	return nil
}
