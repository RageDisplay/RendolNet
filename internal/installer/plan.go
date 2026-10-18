package installer

import (
	"fmt"
)

type StepKind int

const (
	Cmd StepKind = iota
	File
	Fstab
)

type Step struct {
	Kind     StepKind
	Title    string
	Argv     []string
	Path     string
	Content  string
	Mode     uint32
	Optional bool
}

const (
	Target     = "/target"
	Squashfs   = LiveMediumMount + "/live/filesystem.squashfs"
	LiveMarker = "/etc/rendolnet-live"
)

func partition(d Disk, n int) string {
	if d.ByID != "" {
		return fmt.Sprintf("%s-part%d", d.ByID, n)
	}
	p := d.Path
	if last := p[len(p)-1]; last >= '0' && last <= '9' {
		return fmt.Sprintf("%sp%d", p, n)
	}
	return fmt.Sprintf("%s%d", p, n)
}

// Plan собирает шаги установки. Пригодность диска перепроверяется здесь же:
// обойти подтверждение из интерфейса нельзя.
func Plan(a Answers, d Disk) ([]Step, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	if err := CheckTarget(d, a.Confirm); err != nil {
		return nil, err
	}
	seed, err := SeedConfig(a)
	if err != nil {
		return nil, fmt.Errorf("стартовый конфиг: %w", err)
	}
	dev := d.Path
	if d.ByID != "" {
		dev = d.ByID
	}
	esp, root := partition(d, 1), partition(d, 3)
	in := func(p string) string { return Target + p }
	chroot := func(args ...string) []string { return append([]string{"chroot", Target}, args...) }

	var data []string
	if a.WAN != "" {
		data = append(data, a.WAN)
	}
	for _, l := range a.LANs {
		data = append(data, l.Iface)
	}
	hosts := fmt.Sprintf("127.0.0.1\tlocalhost\n127.0.1.1\t%s\n::1\tlocalhost ip6-localhost ip6-loopback\n", a.Hostname)

	s := []Step{
		{Kind: Cmd, Title: "Очистка таблицы разделов", Argv: []string{"sgdisk", "--zap-all", dev}},
		{Kind: Cmd, Title: "Очистка сигнатур", Argv: []string{"wipefs", "--all", "--force", dev}},
		{Kind: Cmd, Title: "Разметка", Argv: []string{"sgdisk",
			"-n1:0:+512M", "-t1:EF00", "-c1:RN-ESP",
			"-n2:0:+1M", "-t2:EF02", "-c2:RN-BIOS",
			"-n3:0:0", "-t3:8304", "-c3:RN-ROOT", dev}},
		{Kind: Cmd, Title: "Перечитывание разделов", Argv: []string{"partprobe", dev}, Optional: true},
		{Kind: Cmd, Title: "Ожидание устройств", Argv: []string{"udevadm", "settle", "--timeout=30"}},
		{Kind: Cmd, Title: "Форматирование EFI", Argv: []string{"mkfs.vfat", "-F", "32", "-n", "RN-ESP", esp}},
		{Kind: Cmd, Title: "Форматирование системы", Argv: []string{"mkfs.ext4", "-F", "-q", "-L", "rn-root", root}},
		{Kind: Cmd, Title: "Монтирование", Argv: []string{"mkdir", "-p", Target}},
		{Kind: Cmd, Title: "Монтирование", Argv: []string{"mount", root, Target}},
		{Kind: Cmd, Title: "Монтирование", Argv: []string{"mkdir", "-p", in("/boot/efi")}},
		{Kind: Cmd, Title: "Монтирование", Argv: []string{"mount", esp, in("/boot/efi")}},
		{Kind: Cmd, Title: "Копирование системы", Argv: []string{"unsquashfs", "-f", "-q", "-n", "-d", Target, Squashfs}},
		{Kind: Fstab, Title: "fstab", Path: in("/etc/fstab"), Argv: []string{root, esp}},

		{Kind: File, Title: "Имя узла", Path: in("/etc/hostname"), Content: a.Hostname + "\n", Mode: 0o644},
		{Kind: File, Title: "Имя узла", Path: in("/etc/hosts"), Content: hosts, Mode: 0o644},
		{Kind: File, Title: "Часовой пояс", Path: in("/etc/timezone"), Content: a.Timezone + "\n", Mode: 0o644},
		{Kind: Cmd, Title: "Часовой пояс", Argv: []string{"ln", "-sf", "/usr/share/zoneinfo/" + a.Timezone, in("/etc/localtime")}},
		{Kind: File, Title: "Сеть управления", Path: in("/etc/systemd/network/10-mgmt.network"), Content: MgmtNetworkUnit(a.Mgmt, a.MgmtAddr), Mode: 0o644},
		{Kind: File, Title: "Загрузочная защита", Path: in("/etc/nftables.conf"), Content: BootGuard(data), Mode: 0o644},
		{Kind: File, Title: "Конфиг узла", Path: in("/var/lib/rendolnet/agent/confirmed.json"), Content: string(seed), Mode: 0o600},
		{Kind: File, Title: "Пароль базы", Path: in("/var/lib/rancher/k3s/server/manifests/rendolnet-local.yaml"), Content: DBSecret(), Mode: 0o600},
		{Kind: Cmd, Title: "Убираем live-систему", Argv: []string{"rm", "-f", in(LiveMarker),
			in("/etc/systemd/system/getty@tty1.service.d/rendol-installer.conf"),
			in("/etc/systemd/network/99-live.network")}},
	}
	for _, m := range []string{"/dev", "/proc", "/sys", "/run"} {
		s = append(s, Step{Kind: Cmd, Title: "Загрузчик", Argv: []string{"mount", "--rbind", m, in(m)}})
	}
	s = append(s,
		Step{Kind: Cmd, Title: "Загрузчик UEFI", Argv: chroot("grub-install", "--target=x86_64-efi",
			"--efi-directory=/boot/efi", "--bootloader-id=rendolnet", "--removable", "--no-nvram")},
		Step{Kind: Cmd, Title: "Загрузчик UEFI", Argv: chroot("grub-install", "--target=x86_64-efi",
			"--efi-directory=/boot/efi", "--bootloader-id=rendolnet"), Optional: true},
		Step{Kind: Cmd, Title: "Загрузчик BIOS", Argv: chroot("grub-install", "--target=i386-pc", dev)},
		Step{Kind: Cmd, Title: "Загрузчик", Argv: chroot("update-grub")},
		Step{Kind: Cmd, Title: "initramfs", Argv: chroot("update-initramfs", "-u", "-k", "all")},
	)
	if a.SSHKey != "" {
		s = append(s,
			Step{Kind: File, Title: "SSH", Path: in("/root/.ssh/authorized_keys"), Content: a.SSHKey + "\n", Mode: 0o600},
			Step{Kind: File, Title: "SSH", Path: in("/etc/ssh/sshd_config.d/rendolnet.conf"), Mode: 0o644,
				Content: "PermitRootLogin prohibit-password\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nX11Forwarding no\n"},
			Step{Kind: Cmd, Title: "SSH", Argv: chroot("chmod", "700", "/root/.ssh")},
			Step{Kind: Cmd, Title: "SSH", Argv: chroot("systemctl", "enable", "ssh.service")})
	} else {
		s = append(s, Step{Kind: Cmd, Title: "SSH", Argv: chroot("systemctl", "disable", "ssh.service"), Optional: true})
	}
	s = append(s,
		Step{Kind: Cmd, Title: "Службы", Argv: chroot("systemctl", "enable", "systemd-networkd.service",
			"nftables.service", "k3s.service", "rendol-updater.path", "rendol-updater.timer", "rendol-console.service")},
		Step{Kind: Cmd, Title: "Завершение", Argv: []string{"sync"}},
		Step{Kind: Cmd, Title: "Размонтирование", Argv: []string{"umount", "-R", Target}, Optional: true},
	)
	return s, nil
}
