package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mia-clark/kwrt-net-manager/internal/api"
	"github.com/mia-clark/kwrt-net-manager/internal/appcfg"
	"github.com/mia-clark/kwrt-net-manager/internal/backup"
	"github.com/mia-clark/kwrt-net-manager/internal/ddns"
	"github.com/mia-clark/kwrt-net-manager/internal/eventbus"
	"github.com/mia-clark/kwrt-net-manager/internal/logcenter"
	"github.com/mia-clark/kwrt-net-manager/internal/netcfg"
	"github.com/mia-clark/kwrt-net-manager/internal/pkgmgr"
	"github.com/mia-clark/kwrt-net-manager/internal/speedtest"
	"github.com/mia-clark/kwrt-net-manager/internal/store"
	"github.com/mia-clark/kwrt-net-manager/pkg/version"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		os.Exit(runServe(os.Args[2:]))
	case "health":
		os.Exit(runHealth(os.Args[2:]))
	case "version", "-v", "--version":
		fmt.Printf("kwrtmgrd %s (built %s)\n", version.Number, version.BuildDate)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `kwrtmgrd — KWRT 网络管理守护进程 (DHCP / 静态路由)

USAGE
  kwrtmgrd <command> [flags]

COMMANDS
  serve     Run the HTTP API server (default for containers)
  health    Probe /api/v1/health and exit non-zero on failure
  version   Print version information
  help      Show this help

ENV
  KWRTNET_API_TOKEN          Required. Bearer token for API auth.
  KWRTNET_HTTP_ADDR          Listen address (default ":18080")
  KWRTNET_DATA_DIR           Data root (default "/data")
  KWRTNET_CORS_ORIGINS       Comma-separated origins or "*" (default "*")
  KWRTNET_LOG_LEVEL          trace|debug|info|warn|error (default "info")
  KWRTNET_DOCS_ENABLED       Expose /api/docs Scalar UI (default "true")
  KWRTNET_NETCFG_BACKEND     uci|store|auto network backend (default "auto")`)
}

func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	_ = fs.Parse(args)

	cfg, err := appcfg.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		return 1
	}
	if err := cfg.EnsureDirs(); err != nil {
		fmt.Fprintf(os.Stderr, "cannot create data dirs: %v\n", err)
		return 1
	}

	// A LevelVar lets the running level be changed at runtime via the
	// system-config UI (KWRTNET_LOG_LEVEL is the boot default).
	levelVar := new(slog.LevelVar)
	levelVar.Set(appcfg.ParseLevel(cfg.LogLevel))
	// 按级别分流：INFO/DEBUG→stdout、WARN/ERROR→stderr。procd 据此把正常日志标 daemon.info、
	// 告警/错误标 daemon.err，logread 的 severity 才与日志级别一致（不再把 200 访问日志当 err）。
	logger := slog.New(appcfg.NewLogHandler(&slog.HandlerOptions{Level: levelVar}))
	if cfg.HTTPAddrWarn != "" {
		logger.Warn("listen addr normalize", slog.String("detail", cfg.HTTPAddrWarn))
	}
	logger.Info("starting kwrtmgrd",
		slog.String("addr", cfg.HTTPAddr),
		slog.String("data_dir", cfg.DataDir),
		slog.String("version", version.Number),
		slog.String("netcfg_backend", cfg.NetcfgBackend),
	)

	bus := eventbus.New(1024)

	// meta.json store: branding / system-config / backup config.
	st, err := store.New(cfg.MetaFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open meta store: %v\n", err)
		return 1
	}

	// Network-config service (DHCP + static routing) over the selected backend.
	nbe, err := netcfg.NewBackend(cfg.NetcfgBackend, cfg.DataDir, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init netcfg backend: %v\n", err)
		return 1
	}
	nsvc := netcfg.NewService(nbe, bus, logger)
	logger.Info("netcfg ready", slog.String("backend", nbe.Kind()))

	// 日志中心：系统/DHCP/拨号/DDNS 日志 + 本工具审计 + ARP 差分监控。
	logs := logcenter.New(cfg.DataDir, logger)
	arpCtx, arpCancel := context.WithCancel(context.Background())
	defer arpCancel()
	logs.StartARPMonitor(arpCtx, 20*time.Second)

	// 动态域名 DDNS（OpenWrt ddns-scripts；旁车 DATA_DIR/ddns.json）。
	ddnsSvc := ddns.New(pkgmgr.RealRunner{}, filepath.Join(cfg.DataDir, "ddns.json"), nil)
	// device 条目（按终端 MAC 解析 GUA）后台轮询：MAC→稳定 IPv6 写缓存，供 ddns-scripts ip_source='script' 拾取。
	// IPv6 主线无 lease ubus 事件，故轮询（60s）。非 OpenWrt 环境解析为空 → 无副作用。
	ddnsSvc.StartDevicePoller(arpCtx, 60*time.Second)
	// 线路测速（OpenWrt speedtest-go；多节点 + 历史落 DATA_DIR/speedtest_history.json）。
	speedSvc := speedtest.New(pkgmgr.RealRunner{}, cfg.DataDir)

	// Backup payload builder (meta.json + netcfg export).
	exportSrc := api.NewExportSource(st, nsvc, logger)

	// Scheduled-backup engine: cron-driven uploads of the export to the
	// configured storage channels.
	host, _ := os.Hostname()
	sched := backup.NewScheduler(st, exportSrc, st, bus, logger, host)
	sched.Start()
	defer sched.Stop()
	// A restored backup config must re-arm cron without a restart.
	exportSrc.SetAfterRestore(func() {
		if err := sched.Reload(); err != nil {
			logger.Warn("reload backup scheduler after restore failed", slog.Any("err", err))
		}
	})

	handler := api.NewRouter(api.Deps{
		Cfg:       cfg,
		Logger:    logger,
		Store:     st,
		Bus:       bus,
		LogLevel:  levelVar,
		Backup:    sched,
		Export:    exportSrc,
		Net:       nsvc,
		Logs:      logs,
		DDNS:      ddnsSvc,
		Speedtest: speedSvc,
	})
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("shutdown signal received", slog.String("signal", sig.String()))
	case err := <-errCh:
		logger.Error("http server crashed", slog.Any("err", err))
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownWait)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", slog.Any("err", err))
		return 1
	}
	logger.Info("bye")
	return 0
}

func runHealth(args []string) int {
	fs := flag.NewFlagSet("health", flag.ExitOnError)
	addr := fs.String("addr", "http://127.0.0.1:18080", "daemon base URL")
	_ = fs.Parse(args)

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(*addr + "/api/v1/health")
	if err != nil {
		fmt.Fprintf(os.Stderr, "health check failed: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "unhealthy: status=%d\n", resp.StatusCode)
		return 1
	}
	return 0
}
