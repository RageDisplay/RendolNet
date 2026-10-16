// rendol-agent — dataplane узла. Работает в поде с hostNetwork и правами на
// BPF и сеть; конфиг получает от rendol-api через unix-сокет.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"rendolnet/internal/agent"
	"rendolnet/internal/dataplane"
	"rendolnet/internal/platform/env"
	rnlog "rendolnet/internal/platform/log"
	"rendolnet/internal/zapret"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "zapret":
			log := rnlog.Setup("rendol-zapret")
			r := &zapret.Runner{
				Log:       log,
				AgentSock: env.String("RN_AGENT_SOCKET", "/run/rendolnet/agent.sock"),
				Nfqws:     env.String("RN_NFQWS", "/app/nfqws"),
				BaseDir:   env.String("RN_ZAPRET_DIR", "/opt/zapret"),
				WorkDir:   "/run/zapret",
				StatusOut: env.String("RN_ZAPRET_STATUS", "/run/rendolnet/zapret.json"),
			}
			if err := r.Run(ctx); err != nil {
				log.Error("zapret", "err", err)
				os.Exit(1)
			}
			return
		case "zapret-check":
			if err := zapret.CheckAll(os.Args[2], os.Args[3]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Println("все стратегии приняты nfqws")
			return
		}
	}

	log := rnlog.Setup("rendol-agent")
	log.Info("старт", "version", version)

	dp, err := dataplane.Open(log, env.String("RN_BPF_DIR", dataplane.DefaultPinDir))
	if err != nil {
		log.Error("dataplane", "err", err)
		os.Exit(1)
	}
	// Close не снимает программы с интерфейсов: связи закреплены, и узел
	// продолжает фильтровать трафик, пока агент перезапускается.
	defer func() { _ = dp.Close() }()

	stateDir := env.String("RN_STATE_DIR", "/var/lib/rendolnet/agent")
	if s := env.String("RN_SEED", ""); s != "" {
		seed, err := agent.ParseSeed(s)
		if err == nil {
			var made bool
			if made, err = agent.SeedIfEmpty(stateDir, seed); made {
				log.Info("записан стартовый конфиг из RN_SEED")
			}
		}
		if err != nil {
			log.Error("RN_SEED", "err", err)
		}
	}

	a := agent.New(log, dp, agent.NewLinuxNetwork(log), stateDir, version)
	if err := a.Start(ctx); err != nil {
		log.Error("запуск агента", "err", err)
	}

	sock := env.String("RN_AGENT_SOCKET", "/run/rendolnet/agent.sock")
	if err := os.MkdirAll(filepath.Dir(sock), 0o750); err != nil {
		log.Error("каталог сокета", "err", err)
		os.Exit(1)
	}
	l, err := agent.ListenUnix(sock)
	if err != nil {
		log.Error("сокет", "err", err)
		os.Exit(1)
	}
	srv := &http.Server{Handler: a.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	if err := srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("http", "err", err)
		os.Exit(1)
	}
}
