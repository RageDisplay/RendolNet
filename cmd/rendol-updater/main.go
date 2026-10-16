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
	}
	return fmt.Errorf("неизвестное действие %q", req.Action)
}
