// Package backup implements the scheduled-backup subsystem: pluggable storage
// channels (S3-compatible object storage and WebDAV), cron-driven schedules
// that package the full config export and upload it, plus a small run history.
//
// The data models here are pure (no heavy deps) so the manager package can
// persist them in meta.json without pulling the S3/WebDAV/cron clients into its
// own compile unit's public surface. The engine files (uploader/s3/webdav/
// scheduler) live in the same package and depend on the heavy libraries.
package backup

import "strings"

// Channel kinds.
const (
	KindS3     = "s3"
	KindWebDAV = "webdav"
)

// DefaultPathTemplate is the object-key template used when a schedule does not
// set its own. See docs/BACKUP.zh-CN.md for the placeholder list.
const DefaultPathTemplate = "frpcmgr-backups/{schedule}/{year}/{month}/frpcmgr-{date}-{time}.zip"

// RunHistoryCap is the max number of run records kept (host-local history).
const RunHistoryCap = 200

// Channel is one configured storage destination. Exactly one of S3/WebDAV is
// populated, selected by Kind.
type Channel struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Kind      string        `json:"kind"` // KindS3 | KindWebDAV
	S3        *S3Config     `json:"s3,omitempty"`
	WebDAV    *WebDAVConfig `json:"webdav,omitempty"`
	CreatedAt int64         `json:"created_at"`
	UpdatedAt int64         `json:"updated_at"`
}

// S3Config holds an S3-compatible object-storage target. The same shape serves
// AWS S3, Aliyun OSS, Cloudflare R2, MinIO, Backblaze B2, etc.
type S3Config struct {
	Endpoint        string `json:"endpoint"`          // host[:port], no scheme (e.g. s3.amazonaws.com, xxx.r2.cloudflarestorage.com)
	Region          string `json:"region"`           // e.g. us-east-1, auto (R2)
	Bucket          string `json:"bucket"`           // target bucket
	AccessKeyID     string `json:"access_key_id"`    //
	SecretAccessKey string `json:"secret_access_key"` // sensitive; masked on API read
	Prefix          string `json:"prefix"`           // optional base folder inside the bucket
	UseSSL          bool   `json:"use_ssl"`          // https endpoint
	PathStyle       bool   `json:"path_style"`       // path-style addressing (needed by MinIO / some OSS)
}

// WebDAVConfig holds a WebDAV target (Nextcloud, Jianguoyun, Synology, ...).
type WebDAVConfig struct {
	BaseURL  string `json:"base_url"` // e.g. https://dav.jianguoyun.com/dav/
	Username string `json:"username"`
	Password string `json:"password"` // sensitive; masked on API read
	Prefix   string `json:"prefix"`   // optional base folder under base_url
}

// Schedule is one cron-driven backup job targeting a Channel.
type Schedule struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled"`
	Cron         string `json:"cron"`          // robfig/cron spec (5-field or @descriptor)
	ChannelID    string `json:"channel_id"`    // references Channel.ID
	PathTemplate string `json:"path_template"` // empty → DefaultPathTemplate
	Retention    int    `json:"retention"`     // keep newest N, 0 = unlimited
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

// EffectiveTemplate returns the schedule's path template or the default.
func (s Schedule) EffectiveTemplate() string {
	if strings.TrimSpace(s.PathTemplate) == "" {
		return DefaultPathTemplate
	}
	return s.PathTemplate
}

// Run status / trigger values.
const (
	StatusRunning = "running"
	StatusSuccess = "success"
	StatusFailed  = "failed"

	TriggerSchedule = "schedule"
	TriggerManual   = "manual"
)

// RunRecord is one backup execution result (kept as host-local history).
type RunRecord struct {
	ID         string `json:"id"`
	ScheduleID string `json:"schedule_id"`
	ChannelID  string `json:"channel_id"`
	Trigger    string `json:"trigger"`
	Status     string `json:"status"`
	StartedAt  int64  `json:"started_at"`
	FinishedAt int64  `json:"finished_at"`
	ObjectPath string `json:"object_path"`
	SizeBytes  int64  `json:"size_bytes"`
	Error      string `json:"error"`
}

// ---- deep-clone helpers (used by the meta store to hand out copies) ----

// Clone returns a deep copy of the channel (independent sub-config pointers).
func (c Channel) Clone() Channel {
	out := c
	if c.S3 != nil {
		s := *c.S3
		out.S3 = &s
	}
	if c.WebDAV != nil {
		w := *c.WebDAV
		out.WebDAV = &w
	}
	return out
}

// CloneChannels deep-copies a slice of channels.
func CloneChannels(in []Channel) []Channel {
	if in == nil {
		return nil
	}
	out := make([]Channel, len(in))
	for i, c := range in {
		out[i] = c.Clone()
	}
	return out
}

// CloneSchedules copies a slice of schedules (all value fields).
func CloneSchedules(in []Schedule) []Schedule {
	if in == nil {
		return nil
	}
	out := make([]Schedule, len(in))
	copy(out, in)
	return out
}

// CloneRuns copies a slice of run records (all value fields).
func CloneRuns(in []RunRecord) []RunRecord {
	if in == nil {
		return nil
	}
	out := make([]RunRecord, len(in))
	copy(out, in)
	return out
}
