// rendol-updater — выполняет на узле то, что поду rendol-api делать нельзя:
// установку обновлений, перезагрузку и выключение. Запускается systemd по
// появлению request.json (см. iso/rootfs/etc/systemd/system/rendol-updater.*).
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"rendolnet/internal/update"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "использование: rendol-updater run | version")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version":
		fmt.Println(version)
	case "run":
		if err := run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "неизвестная команда", os.Args[1])
		os.Exit(2)
	}
}

func run() error {
	h := update.DefaultHost()
	h.Log = func(s string) { fmt.Println(s) }

	reqPath := filepath.Join(h.Root, update.RequestFile)
	req, err := update.ReadRequest(h.Root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	// файл убирается сразу: иначе systemd .path сработает на него снова
	_ = os.Remove(reqPath)
	if err != nil {
		return fmt.Errorf("запрос не читается: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()

	switch req.Action {
	case update.ActionUpdate:
		return h.Apply(ctx, req)
	case update.ActionReboot, update.ActionPoweroff:
		now := time.Now()
		_ = update.WriteJSON(filepath.Join(h.Root, update.StatusFile), update.Status{
			RequestID: req.ID, Action: req.Action, State: update.StateDone, Started: &now, Finished: &now,
			Message: "запрошено пользователем " + req.User,
		})
		return exec.CommandContext(ctx, "systemctl", string(req.Action)).Run()
	case update.ActionSSH:
		now := time.Now()
		msg, err := applySSH(ctx, req.SSHKey)
		st := update.Status{RequestID: req.ID, Action: req.Action, State: update.StateDone, Started: &now, Finished: &now, Message: msg}
		if err != nil {
			st.State, st.Message = update.StateFailed, err.Error()
		}
		_ = update.WriteJSON(filepath.Join(h.Root, update.StatusFile), st)
		return err
	}
	return fmt.Errorf("неизвестное действие %q", req.Action)
}

// applySSH: ключ — вход root только по ключу; пусто — sshd выключен.
func applySSH(ctx context.Context, key string) (string, error) {
	if key == "" {
		_ = os.Remove("/root/.ssh/authorized_keys")
		if out, err := exec.CommandContext(ctx, "systemctl", "disable", "--now", "ssh.service").CombinedOutput(); err != nil {
			return "", fmt.Errorf("ssh: %s", out)
		}
		return "SSH выключен", nil
	}
	// ключи хоста попали в образ при сборке ISO и одинаковы на всех узлах
	const ownKeys = "/etc/ssh/.rendolnet-host-keys"
	if _, err := os.Stat(ownKeys); err != nil {
		old, _ := filepath.Glob("/etc/ssh/ssh_host_*")
		for _, f := range old {
			_ = os.Remove(f)
		}
		if out, err := exec.CommandContext(ctx, "ssh-keygen", "-A").CombinedOutput(); err != nil {
			return "", fmt.Errorf("ssh-keygen: %s", out)
		}
		_ = os.WriteFile(ownKeys, nil, 0o600)
	}
	if err := os.MkdirAll("/root/.ssh", 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile("/root/.ssh/authorized_keys", []byte(key+"\n"), 0o600); err != nil {
		return "", err
	}
	conf := "PermitRootLogin prohibit-password\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nX11Forwarding no\n"
	if err := os.WriteFile("/etc/ssh/sshd_config.d/rendolnet.conf", []byte(conf), 0o644); err != nil {
		return "", err
	}
	if out, err := exec.CommandContext(ctx, "systemctl", "enable", "--now", "ssh.service").CombinedOutput(); err != nil {
		return "", fmt.Errorf("ssh: %s", out)
	}
	_ = exec.CommandContext(ctx, "systemctl", "reload", "ssh.service").Run()
	return "SSH включён, вход root по ключу", nil
}
