package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/good-fish-man/agent-runtime-client/boot"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
	log "github.com/good-fish-man/logx"
)

const defaultConfigPath = "manifest/config/config.yaml"

func main() {
	ctx := context.Background()
	cfgPath := flag.String("config", defaultConfigPath, "path to config yaml")
	flag.Parse()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	for {
		restart, err := serve(ctx, *cfgPath, quit)
		if err != nil {
			log.Fatalf(ctx, "service failed: %v", err)
		}
		if !restart {
			return
		}
		log.Infof(ctx, "restarting %s with updated configuration...", consts.ServiceName)
	}
}

func serve(ctx context.Context, cfgPath string, quit <-chan os.Signal) (bool, error) {
	app, err := boot.Init(ctx, cfgPath)
	if err != nil {
		return false, err
	}
	defer func() { _ = app.Close() }()

	// Non-fatal startup probe: the client stays up even if the runtime is down.
	if st, err := app.PingRuntime(ctx); err != nil {
		log.Warnf(ctx, "agent-runtime health probe failed (continuing): %v", err)
	} else {
		log.Infof(ctx, "agent-runtime health: %s (version %s)", st.Status, st.Version)
	}

	srv := &http.Server{
		Addr:    app.Cfg.Server.HTTPAddr,
		Handler: app.Engine,
	}

	log.Go(ctx, func(serverCtx context.Context) {
		log.Infof(serverCtx, "%s listening on %s", consts.ServiceName, app.Cfg.Server.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf(serverCtx, "http server error: %v", err)
		}
	})

	restart := false
	select {
	case <-quit:
	case <-app.Restart:
		restart = true
	}

	log.Infof(ctx, "shutting down %s...", consts.ServiceName)
	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Errorf(shutdownCtx, "graceful shutdown failed: %v", err)
	}
	if !restart {
		log.Infof(ctx, "bye")
	}
	return restart, nil
}
