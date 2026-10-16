// rendol-installer — установка RendolNet с live-ISO. Работает на tty1
// live-системы; с rn.autoinstall=1 в строке ядра берёт ответы из
// autoinstall.yaml на томе с меткой RNAUTO.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"rendolnet/internal/installer"
)

var version = "dev"

const logPath = "/run/rendol-installer.log"

type env struct {
	runner installer.Runner
	log    io.Writer
}

func (e *env) logf(f string, a ...any) {
	fmt.Fprintf(e.log, time.Now().UTC().Format("15:04:05 ")+f+"\n", a...)
}

func (e *env) execute(ctx context.Context, steps []installer.Step, p installer.Progress) error {
	return installer.Execute(ctx, e.runner, steps, p, e.logf)
}

func (e *env) reboot() { _ = exec.CommandContext(context.Background(), "systemctl", "reboot").Run() }

func main() {
	if err := installer.GuardLive(); err != nil {
		fmt.Fprintln(os.Stderr, "rendol-installer:", err)
		os.Exit(2)
	}
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer lf.Close()
	e := &env{runner: installer.Exec{Timeout: 30 * time.Minute}, log: lf}
	ctx := context.Background()

	cmdline, _ := os.ReadFile("/proc/cmdline")
	if installer.AutoinstallRequested(string(cmdline)) {
		if err := autoinstall(ctx, e); err != nil {
			e.logf("автоустановка: %v", err)
			fmt.Fprintln(os.Stderr, "АВТОУСТАНОВКА НЕ УДАЛАСЬ:", err)
			select {} // повторять разрушительную операцию сам не будет
		}
		return
	}

	disks, err := installer.Inventory(ctx, e.runner)
	if err != nil {
		fmt.Fprintln(os.Stderr, "диски:", err)
		os.Exit(1)
	}
	p := tea.NewProgram(newModel(e, disks, installer.NICs("/sys/class/net")), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func autoinstall(ctx context.Context, e *env) error {
	dev := filepath.Join("/dev/disk/by-label", installer.AutoinstallLabel)
	mnt := "/run/rnauto"
	if err := os.MkdirAll(mnt, 0o700); err != nil {
		return err
	}
	if _, err := e.runner.Run(ctx, "mount", "-o", "ro", dev, mnt); err != nil {
		return fmt.Errorf("том %s: %w", installer.AutoinstallLabel, err)
	}
	raw, err := os.ReadFile(filepath.Join(mnt, "autoinstall.yaml"))
	_, _ = e.runner.Run(ctx, "umount", mnt)
	if err != nil {
		return err
	}
	a, err := installer.ParseAutoinstall(raw)
	if err != nil {
		return err
	}
	disks, err := installer.Inventory(ctx, e.runner)
	if err != nil {
		return err
	}
	d, err := installer.FindDisk(disks, a.Disk)
	if err != nil {
		return err
	}
	steps, err := installer.Plan(a, d)
	if err != nil {
		return err
	}
	if err := e.execute(ctx, steps, func(i, total int, title string) {
		fmt.Printf("[%d/%d] %s\n", i+1, total, title)
	}); err != nil {
		return err
	}
	fmt.Println("установка завершена")
	if a.Reboot {
		e.reboot()
	}
	return nil
}
