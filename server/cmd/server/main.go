package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/api"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/auth"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/datasets"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/jobs"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/pools"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/registry"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store/sqlite"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/ws"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/web"
)

func main() {
	addr := flag.String("addr", envOr("NT_ADDR", ":8080"), "listen address")
	dataRoot := flag.String("data", envOr("NT_DATA", "server_data"), "data root directory")
	httpBase := flag.String("http-base", envOr("NT_HTTP_BASE", ""), "public base url for agent asset downloads")
	flag.Parse()

	if *httpBase == "" {
		*httpBase = "http://localhost" + *addr
	}

	if err := os.MkdirAll(*dataRoot, 0o755); err != nil {
		slog.Error("data root", "err", err)
		os.Exit(1)
	}
	st, err := sqlite.Open(filepath.Join(*dataRoot, "server.db"))
	if err != nil {
		slog.Error("open store", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	authSvc := auth.NewService(st, 12*time.Hour)
	reg := registry.New(st, authSvc)
	poolsSvc := pools.New(st)
	hub := ws.NewHub(reg, st, ws.Config{HeartbeatIntervalS: 15})

	logDir := filepath.Join(*dataRoot, "logs")
	jobsSvc := jobs.New(st, poolsSvc, hub, jobs.Config{LogDir: logDir, HTTPBase: *httpBase})
	hub.SetHandler(jobsSvc)
	if err := jobsSvc.RebuildQueues(context.Background()); err != nil {
		slog.Error("rebuild queues", "err", err)
		os.Exit(1)
	}

	localBackend, err := datasets.NewLocal(*dataRoot)
	if err != nil {
		slog.Error("storage backend", "err", err)
		os.Exit(1)
	}
	datasetsSvc := datasets.New(st, localBackend)

	apiSrv := api.New(st, authSvc, poolsSvc, jobsSvc, datasetsSvc, hub)
	apiSrv.LogDir = logDir

	srv := &http.Server{
		Addr:    *addr,
		Handler: apiSrv.Router(web.Static()),
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go hub.RunJanitor(ctx)
	go jobsSvc.Run(ctx)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	slog.Info("server listening", "addr", *addr, "data", *dataRoot)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("serve", "err", err)
		os.Exit(1)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
