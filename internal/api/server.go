package api

import (
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/mia-clark/kwrt-net-manager/internal/api/middleware"
	"github.com/mia-clark/kwrt-net-manager/internal/appcfg"
	"github.com/mia-clark/kwrt-net-manager/internal/backup"
	"github.com/mia-clark/kwrt-net-manager/internal/eventbus"
	"github.com/mia-clark/kwrt-net-manager/internal/netcfg"
	"github.com/mia-clark/kwrt-net-manager/internal/store"
	"github.com/mia-clark/kwrt-net-manager/web"
)

// Deps bundles the collaborators that handlers need.
type Deps struct {
	Cfg    *appcfg.Config
	Logger *slog.Logger
	// Store owns meta.json: branding, runtime system-config, backup config.
	Store *store.Store
	// Bus drives the WebSocket /events stream.
	Bus *eventbus.Bus
	// LogLevel is the live logger level knob so the system-config endpoint can
	// change verbosity without a restart. May be nil.
	LogLevel *slog.LevelVar
	// Backup is the scheduled-backup engine. May be nil (tests).
	Backup *backup.Scheduler
	// Export builds/restores the backup payload (meta + netcfg). Must be non-nil.
	Export *ExportSource
	// Net is the network-config service (DHCP + static routing). Must be non-nil.
	Net *netcfg.Service
}

// NewRouter assembles the chi mux with all middleware and route groups
// installed. It returns an http.Handler ready to be served.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()

	// Runtime config: env defaults overlaid with meta.json overrides, read live
	// by CORS / docs / self-update so a system-config UI change applies at once.
	rc := NewRuntimeConfig(d.Cfg, d.Store, d.LogLevel)

	r.Use(middleware.Recover(d.Logger))
	r.Use(middleware.AccessLog(d.Logger))
	r.Use(middleware.CORS(rc.EffectiveCORS))

	sys := NewSystemHandler(d.Cfg.DataDir)
	docs := NewDocsHandler(rc.DocsEnabled)
	ui := NewUIHandler(d.Store)

	// Unauthenticated probes + docs. The docs routes are always mounted; each
	// handler 404s per-request when docs are disabled, so the toggle is live.
	r.Get("/api/v1/health", sys.Health)
	// UI branding is read without auth so the login page + browser <title>
	// can render the custom values before the user is authenticated.
	r.Get("/api/v1/ui/branding", ui.GetBranding)
	r.Get("/api/docs", docs.Redirect)
	r.Get("/api/docs/", docs.UI)
	r.Get("/api/docs/openapi.yaml", docs.Spec)
	r.Get("/api/docs/openapi.json", docs.SpecJSON)

	events := NewEventsHandler(d.Bus, d.Logger, rc.EffectiveCORS)
	upd := NewUpdateHandler(d.Cfg.DataDir, rc.SelfUpdateEnabled, d.Logger)
	syscfg := NewSysConfigHandler(rc, d.Logger)
	bkp := NewBackupHandler(d.Store, d.Backup, d.Export.RestoreFromZipBytes, d.Logger)

	// Authenticated subtree.
	r.Group(func(r chi.Router) {
		r.Use(middleware.Bearer(d.Cfg.APIToken))

		r.Get("/api/v1/version", sys.Version)
		r.Get("/api/v1/version/check", upd.Check)
		r.Post("/api/v1/system/update", upd.Update)
		r.Get("/api/v1/system/update/log", upd.Log)
		r.Get("/api/v1/system/config", syscfg.Get)
		r.Put("/api/v1/system/config", syscfg.Put)
		r.Put("/api/v1/ui/branding", ui.UpdateBranding)

		// System monitoring (host CPU/memory/disk/network/connections/process).
		r.Get("/api/v1/system/info", sys.Info)
		r.Get("/api/v1/system/cpu", sys.CPU)
		r.Get("/api/v1/system/memory", sys.Memory)
		r.Get("/api/v1/system/disk", sys.Disk)
		r.Get("/api/v1/system/network", sys.Network)
		r.Get("/api/v1/system/connections", sys.Connections)
		r.Get("/api/v1/system/process", sys.Process)

		// Scheduled backup: storage channels, schedules, run history.
		r.Get("/api/v1/backup/channels", bkp.ListChannels)
		r.Post("/api/v1/backup/channels", bkp.CreateChannel)
		r.Post("/api/v1/backup/channels/test", bkp.TestChannelConfig)
		r.Put("/api/v1/backup/channels/{id}", bkp.UpdateChannel)
		r.Delete("/api/v1/backup/channels/{id}", bkp.DeleteChannel)
		r.Post("/api/v1/backup/channels/{id}/test", bkp.TestChannel)
		r.Get("/api/v1/backup/channels/{id}/objects", bkp.ListObjects)
		r.Get("/api/v1/backup/channels/{id}/download", bkp.Download)
		r.Post("/api/v1/backup/channels/{id}/restore", bkp.Restore)
		r.Get("/api/v1/backup/schedules", bkp.ListSchedules)
		r.Post("/api/v1/backup/schedules", bkp.CreateSchedule)
		r.Put("/api/v1/backup/schedules/{id}", bkp.UpdateSchedule)
		r.Delete("/api/v1/backup/schedules/{id}", bkp.DeleteSchedule)
		r.Post("/api/v1/backup/schedules/{id}/toggle", bkp.ToggleSchedule)
		r.Post("/api/v1/backup/schedules/{id}/run", bkp.RunSchedule)
		r.Get("/api/v1/backup/runs", bkp.ListRuns)

		// Full export / import (meta + network config).
		r.Get("/api/v1/export/all", d.Export.ExportAll)
		r.Post("/api/v1/import/zip", d.Export.ImportZIP)

		// Event stream (WebSocket).
		r.Get("/api/v1/events", events.Subscribe)

		// Network-config routes (DHCP + static routing) are registered here.
		registerNetcfgRoutes(r, d)
	})

	// WebUI 静态文件分发 & SPA 路由兼容
	webFS := web.GetFS()
	fileServer := http.FileServer(http.FS(webFS))

	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		// 未匹配的 api 请求不应回退到前端，直接 404
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		filePath := strings.TrimPrefix(r.URL.Path, "/")

		// 真正存在的静态资源（hash 命名的 js/css/图片等）交给 FileServer，
		// 保留其强缓存。index.html 例外——它要走品牌注入分支。
		if filePath != "" && filePath != "index.html" {
			if f, err := webFS.Open(filePath); err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// index.html（根路径、/index.html、或前端 BrowserRouter 深链接）→ 读取
		// 内嵌 index.html，就地注入当前品牌后写出，实现首屏零闪。
		index, err := fs.ReadFile(webFS, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		out := ui.InjectBranding(index)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(out)
	})

	return r
}
