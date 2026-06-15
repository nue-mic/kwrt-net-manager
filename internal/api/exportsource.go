package api

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/mia-clark/kwrt-net-manager/internal/store"
)

// ExportSource builds and restores the daemon's backup payload: a zip archive
// carrying meta.json (operator branding / system-config / backup config) plus
// the network configuration export (DHCP servers, static leases, ACL, routes).
// It is the single source of truth shared by the manual /export/all download,
// the /import/zip restore, and the scheduled-backup engine, so manual and
// automatic backups are byte-for-byte equivalent.
//
// In the frpc → network-manager rebuild this replaced the old config-profile
// importexport handler. Net is the network-config exporter; it may be nil
// during early bring-up, in which case only meta.json is archived.
type ExportSource struct {
	store *store.Store
	net   NetExporter
	log   *slog.Logger
	// afterRestore runs after a successful zip restore that touched backup
	// config, so the scheduler re-arms cron without a daemon restart. Optional.
	afterRestore func()
}

// SetAfterRestore registers a hook invoked after a restore (e.g. scheduler reload).
func (s *ExportSource) SetAfterRestore(fn func()) { s.afterRestore = fn }

// NetExporter is the subset of the network-config service the backup needs:
// dump the full managed state to JSON, and load it back. Implemented by
// internal/netcfg.Service. Kept as an interface so the api package does not
// hard-depend on netcfg's concrete type and tests can stub it.
type NetExporter interface {
	ExportJSON() ([]byte, error)
	ImportJSON(raw []byte) error
}

// NewExportSource wires the backup payload builder. net may be nil.
func NewExportSource(st *store.Store, net NetExporter, log *slog.Logger) *ExportSource {
	return &ExportSource{store: st, net: net, log: log}
}

// BuildBackupZip writes the archive to w: meta.json (secrets redacted) + the
// network-config export. It satisfies backup.Source.
func (s *ExportSource) BuildBackupZip(w io.Writer) error {
	zw := zip.NewWriter(w)
	if meta, err := s.store.RedactedMetaJSON(); err == nil {
		if fw, err := zw.Create("meta.json"); err == nil {
			_, _ = fw.Write(meta)
		}
	} else if s.log != nil {
		s.log.Warn("export meta.json failed", slog.Any("err", err))
	}
	if s.net != nil {
		if raw, err := s.net.ExportJSON(); err == nil {
			if fw, err := zw.Create("netcfg.json"); err == nil {
				_, _ = fw.Write(raw)
			}
		} else if s.log != nil {
			s.log.Warn("export netcfg.json failed", slog.Any("err", err))
		}
	}
	return zw.Close()
}

// RestoreFromZipBytes restores meta + netcfg from an /export/all zip given as
// raw bytes. Shared by the /import/zip upload and the "restore from a backup
// channel" flow. The only hard error is a non-zip payload.
func (s *ExportSource) RestoreFromZipBytes(body []byte) (map[string]any, error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, err
	}
	var metaRaw, netRaw []byte
	for _, zf := range zr.File {
		switch filepath.Base(zf.Name) {
		case "meta.json":
			metaRaw = readZipEntry(zf)
		case "netcfg.json":
			netRaw = readZipEntry(zf)
		}
	}

	brandingRestored, systemConfigRestored, backupRestored, netcfgRestored := false, false, false, false
	if len(metaRaw) > 0 {
		if brandingRestored, systemConfigRestored, backupRestored, err = s.store.ImportMeta(metaRaw); err != nil {
			s.log.Warn("restore meta from import failed", slog.Any("err", err))
		}
	}
	if len(netRaw) > 0 && s.net != nil {
		if err := s.net.ImportJSON(netRaw); err != nil {
			s.log.Warn("restore netcfg from import failed", slog.Any("err", err))
		} else {
			netcfgRestored = true
		}
	}
	if backupRestored && s.afterRestore != nil {
		s.afterRestore()
	}
	return map[string]any{
		"branding_restored":      brandingRestored,
		"system_config_restored": systemConfigRestored,
		"backup_restored":        backupRestored,
		"netcfg_restored":        netcfgRestored,
	}, nil
}

func readZipEntry(zf *zip.File) []byte {
	rc, err := zf.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	b, _ := io.ReadAll(io.LimitReader(rc, 8<<20))
	return b
}

// ExportAll GET /api/v1/export/all — download the backup zip.
func (s *ExportSource) ExportAll(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="kwrt-net-export-%s.zip"`, time.Now().UTC().Format("20060102-150405")))
	if err := s.BuildBackupZip(w); err != nil && s.log != nil {
		s.log.Warn("export all failed", slog.Any("err", err))
	}
}

// ImportZIP POST /api/v1/import/zip — multipart upload of an /export/all zip.
func (s *ExportSource) ImportZIP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		WriteError(w, http.StatusBadRequest, CodeBadRequest, "parse multipart: "+err.Error(), nil)
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		WriteError(w, http.StatusBadRequest, CodeBadRequest, "file field required", nil)
		return
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, 32<<20))
	if err != nil {
		WriteError(w, http.StatusBadRequest, CodeBadRequest, "read upload: "+err.Error(), nil)
		return
	}
	res, err := s.RestoreFromZipBytes(body)
	if err != nil {
		WriteError(w, http.StatusBadRequest, CodeBadRequest, "not a valid zip: "+err.Error(), nil)
		return
	}
	WriteJSON(w, http.StatusOK, res)
}
