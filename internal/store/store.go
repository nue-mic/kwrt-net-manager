// Package store owns meta.json — the daemon-level persisted metadata that
// survives the frpc → network-manager rebuild: operator UI branding, runtime
// system-config overrides, and the scheduled-backup configuration. It was
// extracted from the old internal/manager so the shell (backup engine, branding
// UI, system-config endpoint) keeps working after the frpc core was removed.
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nue-mic/kwrt-net-manager/internal/backup"
)

// Sentinel errors. The API layer maps these to HTTP statuses.
var (
	ErrNotFound = errors.New("not found")
	ErrExists   = errors.New("already exists")
)

// Meta is the persisted daemon metadata at <data>/meta.json. Legacy frpc keys
// (sort / auto_start / log_view_since) are intentionally dropped — old files
// still load because encoding/json ignores unknown fields.
type Meta struct {
	Version      int           `json:"version"`
	Branding     *Branding     `json:"branding,omitempty"`
	SystemConfig *SystemConfig `json:"system_config,omitempty"`
	Backup       *BackupData   `json:"backup,omitempty"`
}

// BackupData is the persisted scheduled-backup state.
type BackupData struct {
	Channels  []backup.Channel   `json:"channels,omitempty"`
	Schedules []backup.Schedule  `json:"schedules,omitempty"`
	Runs      []backup.RunRecord `json:"runs,omitempty"`
}

func cloneBackupData(b BackupData) BackupData {
	return BackupData{
		Channels:  backup.CloneChannels(b.Channels),
		Schedules: backup.CloneSchedules(b.Schedules),
		Runs:      backup.CloneRuns(b.Runs),
	}
}

// SystemConfig holds operator overrides for runtime daemon settings. Each field
// is a pointer: nil means "fall back to the KWRTNET_* env value".
type SystemConfig struct {
	LogLevel          *string   `json:"log_level,omitempty"`
	SelfUpdateEnabled *bool     `json:"self_update_enabled,omitempty"`
	DocsEnabled       *bool     `json:"docs_enabled,omitempty"`
	CORSOrigins       *[]string `json:"cors_origins,omitempty"`
}

func cloneSystemConfig(c SystemConfig) SystemConfig {
	out := SystemConfig{}
	if c.LogLevel != nil {
		v := *c.LogLevel
		out.LogLevel = &v
	}
	if c.SelfUpdateEnabled != nil {
		v := *c.SelfUpdateEnabled
		out.SelfUpdateEnabled = &v
	}
	if c.DocsEnabled != nil {
		v := *c.DocsEnabled
		out.DocsEnabled = &v
	}
	if c.CORSOrigins != nil {
		v := append([]string(nil), *c.CORSOrigins...)
		out.CORSOrigins = &v
	}
	return out
}

// Branding is the operator-editable UI branding. Empty fields resolve to the
// Default* constants via Effective().
type Branding struct {
	AppName     string `json:"app_name,omitempty"`
	AppSubtitle string `json:"app_subtitle,omitempty"`
	HTMLTitle   string `json:"html_title,omitempty"`
}

// Default branding values for the KWRT network manager. Used as fallback
// whenever a field is unset/empty.
const (
	DefaultAppName     = "OP增强爱快系统"
	DefaultAppSubtitle = "仿爱快 · DHCP / 静态路由"
	DefaultHTMLTitle   = "OP增强爱快系统 · DHCP / 静态路由控制台"
)

// Effective returns a copy with every empty field filled from the defaults.
func (b Branding) Effective() Branding {
	out := b
	if strings.TrimSpace(out.AppName) == "" {
		out.AppName = DefaultAppName
	}
	if strings.TrimSpace(out.AppSubtitle) == "" {
		out.AppSubtitle = DefaultAppSubtitle
	}
	if strings.TrimSpace(out.HTMLTitle) == "" {
		out.HTMLTitle = DefaultHTMLTitle
	}
	return out
}

func defaultMeta() *Meta { return &Meta{Version: 1} }

// Store is the meta.json-backed persistence layer. Safe for concurrent use.
type Store struct {
	path string
	mu   sync.Mutex
	data *Meta
}

// New opens (or creates) the meta store at path.
func New(path string) (*Store, error) {
	s := &Store{path: path, data: defaultMeta()}
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		_ = json.Unmarshal(b, s.data)
		if s.data.Version == 0 {
			s.data.Version = 1
		}
	case errors.Is(err, os.ErrNotExist):
		if err := s.flushLocked(); err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	return s, nil
}

// MetaPath reports the on-disk path of meta.json.
func (s *Store) MetaPath() string { return s.path }

// ---- system config ----

// GetSystemConfig returns the raw stored overrides (no env defaults applied).
func (s *Store) GetSystemConfig() SystemConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.SystemConfig == nil {
		return SystemConfig{}
	}
	return cloneSystemConfig(*s.data.SystemConfig)
}

// SetSystemConfig persists the overrides wholesale.
func (s *Store) SetSystemConfig(c SystemConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cc := cloneSystemConfig(c)
	s.data.SystemConfig = &cc
	return s.flushLocked()
}

// UpdateSystemConfig runs the read-modify-write under the store lock.
func (s *Store) UpdateSystemConfig(apply func(*SystemConfig)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := SystemConfig{}
	if s.data.SystemConfig != nil {
		cur = cloneSystemConfig(*s.data.SystemConfig)
	}
	apply(&cur)
	cc := cloneSystemConfig(cur)
	s.data.SystemConfig = &cc
	return s.flushLocked()
}

// ---- branding ----

// GetBranding returns the effective branding (defaults filled in).
func (s *Store) GetBranding() Branding { return s.branding().Effective() }

// GetBrandingRaw returns the raw stored branding (no defaults applied).
func (s *Store) GetBrandingRaw() Branding { return s.branding() }

func (s *Store) branding() Branding {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Branding == nil {
		return Branding{}
	}
	return *s.data.Branding
}

// SetBranding trims + length-caps and persists branding; returns the effective
// value after the write.
func (s *Store) SetBranding(in Branding) (Branding, error) {
	in.AppName = truncateRunes(strings.TrimSpace(in.AppName), 40)
	in.AppSubtitle = truncateRunes(strings.TrimSpace(in.AppSubtitle), 60)
	in.HTMLTitle = truncateRunes(strings.TrimSpace(in.HTMLTitle), 120)
	s.mu.Lock()
	bc := in
	s.data.Branding = &bc
	err := s.flushLocked()
	s.mu.Unlock()
	if err != nil {
		return Branding{}, err
	}
	return in.Effective(), nil
}

// ---- scheduled-backup config (satisfies backup.Store / Recorder) ----

func (s *Store) ensureBackupLocked() *BackupData {
	if s.data.Backup == nil {
		s.data.Backup = &BackupData{}
	}
	return s.data.Backup
}

// ListBackupChannels returns all configured storage channels (with secrets).
func (s *Store) ListBackupChannels() []backup.Channel {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Backup == nil {
		return nil
	}
	return backup.CloneChannels(s.data.Backup.Channels)
}

// GetBackupChannel returns a channel by id.
func (s *Store) GetBackupChannel(id string) (backup.Channel, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Backup == nil {
		return backup.Channel{}, false
	}
	for _, c := range s.data.Backup.Channels {
		if c.ID == id {
			return c.Clone(), true
		}
	}
	return backup.Channel{}, false
}

// UpsertBackupChannel inserts (empty id) or replaces a storage channel.
func (s *Store) UpsertBackupChannel(ch backup.Channel) (backup.Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bd := s.ensureBackupLocked()
	now := time.Now().Unix()
	if ch.ID == "" {
		ch.ID = backup.NewID("ch")
		ch.CreatedAt = now
		ch.UpdatedAt = now
		bd.Channels = append(bd.Channels, ch.Clone())
		if err := s.flushLocked(); err != nil {
			return backup.Channel{}, err
		}
		return ch.Clone(), nil
	}
	for i := range bd.Channels {
		if bd.Channels[i].ID == ch.ID {
			ch.CreatedAt = bd.Channels[i].CreatedAt
			ch.UpdatedAt = now
			bd.Channels[i] = ch.Clone()
			if err := s.flushLocked(); err != nil {
				return backup.Channel{}, err
			}
			return ch.Clone(), nil
		}
	}
	return backup.Channel{}, ErrNotFound
}

// DeleteBackupChannel removes a storage channel by id.
func (s *Store) DeleteBackupChannel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Backup == nil {
		return ErrNotFound
	}
	bd := s.data.Backup
	out := bd.Channels[:0:0]
	found := false
	for _, c := range bd.Channels {
		if c.ID == id {
			found = true
			continue
		}
		out = append(out, c)
	}
	if !found {
		return ErrNotFound
	}
	bd.Channels = out
	return s.flushLocked()
}

// UpdateBackupChannel atomically mutates a channel under the store lock.
func (s *Store) UpdateBackupChannel(id string, apply func(*backup.Channel)) (backup.Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Backup == nil {
		return backup.Channel{}, ErrNotFound
	}
	for i := range s.data.Backup.Channels {
		if s.data.Backup.Channels[i].ID == id {
			apply(&s.data.Backup.Channels[i])
			s.data.Backup.Channels[i].ID = id
			s.data.Backup.Channels[i].UpdatedAt = time.Now().Unix()
			ch := s.data.Backup.Channels[i].Clone()
			if err := s.flushLocked(); err != nil {
				return backup.Channel{}, err
			}
			return ch, nil
		}
	}
	return backup.Channel{}, ErrNotFound
}

// ListBackupSchedules returns all backup schedules.
func (s *Store) ListBackupSchedules() []backup.Schedule {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Backup == nil {
		return nil
	}
	return backup.CloneSchedules(s.data.Backup.Schedules)
}

// GetBackupSchedule returns a schedule by id.
func (s *Store) GetBackupSchedule(id string) (backup.Schedule, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Backup == nil {
		return backup.Schedule{}, false
	}
	for _, sc := range s.data.Backup.Schedules {
		if sc.ID == id {
			return sc, true
		}
	}
	return backup.Schedule{}, false
}

// UpsertBackupSchedule inserts (empty id) or replaces a backup schedule.
func (s *Store) UpsertBackupSchedule(sc backup.Schedule) (backup.Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bd := s.ensureBackupLocked()
	now := time.Now().Unix()
	if sc.ID == "" {
		sc.ID = backup.NewID("sc")
		sc.CreatedAt = now
		sc.UpdatedAt = now
		bd.Schedules = append(bd.Schedules, sc)
		if err := s.flushLocked(); err != nil {
			return backup.Schedule{}, err
		}
		return sc, nil
	}
	for i := range bd.Schedules {
		if bd.Schedules[i].ID == sc.ID {
			sc.CreatedAt = bd.Schedules[i].CreatedAt
			sc.UpdatedAt = now
			bd.Schedules[i] = sc
			if err := s.flushLocked(); err != nil {
				return backup.Schedule{}, err
			}
			return sc, nil
		}
	}
	return backup.Schedule{}, ErrNotFound
}

// UpdateBackupSchedule atomically mutates a schedule under the store lock.
func (s *Store) UpdateBackupSchedule(id string, apply func(*backup.Schedule)) (backup.Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Backup == nil {
		return backup.Schedule{}, ErrNotFound
	}
	for i := range s.data.Backup.Schedules {
		if s.data.Backup.Schedules[i].ID == id {
			apply(&s.data.Backup.Schedules[i])
			s.data.Backup.Schedules[i].ID = id
			s.data.Backup.Schedules[i].UpdatedAt = time.Now().Unix()
			sc := s.data.Backup.Schedules[i]
			if err := s.flushLocked(); err != nil {
				return backup.Schedule{}, err
			}
			return sc, nil
		}
	}
	return backup.Schedule{}, ErrNotFound
}

// DeleteBackupSchedule removes a schedule by id.
func (s *Store) DeleteBackupSchedule(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Backup == nil {
		return ErrNotFound
	}
	bd := s.data.Backup
	out := bd.Schedules[:0:0]
	found := false
	for _, sc := range bd.Schedules {
		if sc.ID == id {
			found = true
			continue
		}
		out = append(out, sc)
	}
	if !found {
		return ErrNotFound
	}
	bd.Schedules = out
	return s.flushLocked()
}

// AppendBackupRun records a completed backup run (capped history).
func (s *Store) AppendBackupRun(r backup.RunRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	bd := s.ensureBackupLocked()
	bd.Runs = append(bd.Runs, r)
	if backup.RunHistoryCap > 0 && len(bd.Runs) > backup.RunHistoryCap {
		bd.Runs = append([]backup.RunRecord(nil), bd.Runs[len(bd.Runs)-backup.RunHistoryCap:]...)
	}
	return s.flushLocked()
}

// ListBackupRuns returns the run history newest-first, capped at limit (0=all).
func (s *Store) ListBackupRuns(limit int) []backup.RunRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Backup == nil {
		return nil
	}
	runs := s.data.Backup.Runs
	out := make([]backup.RunRecord, 0, len(runs))
	for i := len(runs) - 1; i >= 0; i-- {
		out = append(out, runs[i])
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Store) restoreBackupConfig(channels []backup.Channel, schedules []backup.Schedule) error {
	bd := s.ensureBackupLocked()
	if channels != nil {
		existing := make(map[string]backup.Channel, len(bd.Channels))
		for _, c := range bd.Channels {
			existing[c.ID] = c
		}
		merged := make([]backup.Channel, len(channels))
		for i, c := range channels {
			if old, ok := existing[c.ID]; ok {
				c = backup.MergeChannelSecrets(old, c)
			}
			merged[i] = c.Clone()
		}
		bd.Channels = merged
	}
	if schedules != nil {
		bd.Schedules = backup.CloneSchedules(schedules)
	}
	known := make(map[string]bool, len(bd.Channels))
	for _, c := range bd.Channels {
		known[c.ID] = true
	}
	kept := bd.Schedules[:0:0]
	for _, sc := range bd.Schedules {
		if known[sc.ChannelID] {
			kept = append(kept, sc)
		}
	}
	bd.Schedules = kept
	return s.flushLocked()
}

// ---- import / export of meta.json ----

// RedactedMetaJSON returns the meta.json bytes with every backup channel secret
// blanked, so an exported backup never carries credentials.
func (s *Store) RedactedMetaJSON() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	clone := *s.data
	if s.data.Branding != nil {
		b := *s.data.Branding
		clone.Branding = &b
	}
	if s.data.SystemConfig != nil {
		c := cloneSystemConfig(*s.data.SystemConfig)
		clone.SystemConfig = &c
	}
	if s.data.Backup != nil {
		bd := cloneBackupData(*s.data.Backup)
		for i := range bd.Channels {
			bd.Channels[i] = backup.RedactSecrets(bd.Channels[i])
		}
		clone.Backup = &bd
	}
	return json.MarshalIndent(&clone, "", "  ")
}

// ImportMeta restores operator branding, system-config overrides, and the
// scheduled-backup config from an /export/all meta.json blob.
func (s *Store) ImportMeta(raw []byte) (brandingRestored, systemConfigRestored, backupRestored bool, err error) {
	var meta Meta
	if e := json.Unmarshal(raw, &meta); e != nil {
		return false, false, false, e
	}
	if meta.Branding != nil {
		b := *meta.Branding
		if strings.TrimSpace(b.AppName) != "" ||
			strings.TrimSpace(b.AppSubtitle) != "" ||
			strings.TrimSpace(b.HTMLTitle) != "" {
			if _, e := s.SetBranding(b); e != nil {
				err = e
			} else {
				brandingRestored = true
			}
		}
	}
	if sc := meta.SystemConfig; sc != nil &&
		(sc.LogLevel != nil || sc.SelfUpdateEnabled != nil || sc.DocsEnabled != nil || sc.CORSOrigins != nil) {
		if e := s.SetSystemConfig(*sc); e != nil {
			if err == nil {
				err = e
			}
		} else {
			systemConfigRestored = true
		}
	}
	if bd := meta.Backup; bd != nil && (len(bd.Channels) > 0 || len(bd.Schedules) > 0) {
		s.mu.Lock()
		e := s.restoreBackupConfig(bd.Channels, bd.Schedules)
		s.mu.Unlock()
		if e != nil {
			if err == nil {
				err = e
			}
		} else {
			backupRestored = true
		}
	}
	return brandingRestored, systemConfigRestored, backupRestored, err
}

func (s *Store) flushLocked() error {
	tmp := s.path + ".tmp"
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// truncateRunes caps s to at most max runes so multi-byte CJK is not cut.
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) > max {
		return string(r[:max])
	}
	return s
}
