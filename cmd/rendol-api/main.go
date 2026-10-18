// rendol-api — API, веб-интерфейс и хранилище версий конфига.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"rendolnet/internal/agentclient"
	"rendolnet/internal/api"
	"rendolnet/internal/platform/auth"
	"rendolnet/internal/platform/db"
	"rendolnet/internal/platform/env"
	rnlog "rendolnet/internal/platform/log"
	"rendolnet/internal/store"
)

var version = "dev"

func main() {
	log := rnlog.Setup("rendol-api")
	if len(os.Args) > 1 && os.Args[1] == "prepare-dirs" {
		err := prepareDirs(map[string]os.FileMode{
			env.String("RN_HOST_DIR", "/var/lib/rendolnet/host"): 0o770 | os.ModeSetgid,
			env.String("RN_TLS_DIR", "/var/lib/rendolnet/tls"):   0o700,
		})
		if err != nil {
			log.Error("каталоги api", "err", err)
			os.Exit(1)
		}
		return
	}
	log.Info("старт", "version", version)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, log, env.String("RN_DATABASE_URL", "postgres://rendolnet:rendolnet@127.0.0.1:5432/rendolnet"), 5*time.Minute)
	if err != nil {
		log.Error("база", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		log.Error("миграции", "err", err)
		os.Exit(1)
	}
	st := store.New(pool)

	if err := api.EnsureAdmin(ctx, log, st, env.String("RN_ADMIN_PASSWORD", "admin")); err != nil {
		log.Error("первый пользователь", "err", err)
		os.Exit(1)
	}
	key, err := api.SessionKey(ctx, st)
	if err != nil {
		log.Error("ключ сессий", "err", err)
		os.Exit(1)
	}

	srv := api.New(api.Config{
		Version:      version,
		WebDir:       env.String("RN_WEB_DIR", "/app/web"),
		HostDir:      env.String("RN_HOST_DIR", "/var/lib/rendolnet/host"),
		DataDir:      env.String("RN_DATA_DIR", "/var/lib/rendolnet"),
		ZapretStatus: env.String("RN_ZAPRET_STATUS", "/run/rendolnet/zapret.json"),
	}, log, st, agentclient.New(env.String("RN_AGENT_SOCKET", "/run/rendolnet/agent.sock")), auth.NewSigner(key))

	go func() {
		// агент может подняться позже api — ждём, пока он ответит
		for ctx.Err() == nil {
			err := srv.Bootstrap(ctx)
			if err == nil {
				srv.Reconcile(ctx)
				return
			}
			log.Warn("начальная ревизия", "err", err)
			time.Sleep(5 * time.Second)
		}
	}()
	go srv.Collect(ctx)
	go srv.WatchAdminReset(ctx)

	hs := &http.Server{
		Addr:              env.String("RN_LISTEN", ":443"),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = hs.Shutdown(sctx)
	}()

	if env.Bool("RN_PLAIN_HTTP") {
		log.Warn("HTTPS выключен (RN_PLAIN_HTTP), только для разработки", "addr", hs.Addr)
		err = hs.ListenAndServe()
	} else {
		cert, cerr := api.LoadOrCreateCert(env.String("RN_TLS_DIR", "/var/lib/rendolnet/tls"))
		if cerr != nil {
			log.Error("сертификат", "err", cerr)
			os.Exit(1)
		}
		hs.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
		log.Info("слушаем", "addr", hs.Addr)
		err = hs.ListenAndServeTLS("", "")
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("http", "err", err)
		os.Exit(1)
	}
}
