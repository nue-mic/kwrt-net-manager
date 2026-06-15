package backup

import (
	"testing"
	"time"
)

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"每日":          "每日",
		"daily backup": "daily-backup",
		"a/b\\c":       "a-b-c",
		"  spaced  ":   "spaced",
		"":             "default",
		"...":          "default",
		"a//b":         "a-b",
	}
	for in, want := range cases {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderPath(t *testing.T) {
	tm := time.Date(2026, 6, 13, 3, 0, 5, 0, time.UTC)
	got := renderPath(DefaultPathTemplate, "每日", "vps1", tm)
	want := "frpcmgr-backups/每日/2026/06/frpcmgr-20260613-030005.zip"
	if got != want {
		t.Fatalf("renderPath = %q, want %q", got, want)
	}

	// All tokens + host + dedup slashes.
	tpl := "{host}//{year}-{month}-{day}/{date}_{time}/{ts}.zip"
	got = renderPath(tpl, "s", "h", tm)
	want = "h/2026-06-13/20260613_030005/20260613-030005.zip"
	if got != want {
		t.Fatalf("renderPath tokens = %q, want %q", got, want)
	}
}

func TestRetentionPrefix(t *testing.T) {
	// Default template → stable dir before the first time token.
	if got := retentionPrefix(DefaultPathTemplate, "每日", "h"); got != "frpcmgr-backups/每日/" {
		t.Fatalf("retentionPrefix default = %q", got)
	}
	// {host} is stable, included in the prefix.
	if got := retentionPrefix("a/{host}/{year}/x.zip", "s", "h"); got != "a/h/" {
		t.Fatalf("retentionPrefix host = %q", got)
	}
	// Time token in the very first segment → no stable prefix (retention skipped).
	if got := retentionPrefix("{date}-{time}.zip", "s", "h"); got != "" {
		t.Fatalf("retentionPrefix unstable = %q, want empty", got)
	}
	// No time token at all → directory of the rendered path.
	if got := retentionPrefix("backups/{schedule}/file.zip", "每日", "h"); got != "backups/每日/" {
		t.Fatalf("retentionPrefix no-time = %q", got)
	}
}

func TestJoinKey(t *testing.T) {
	cases := []struct{ prefix, key, want string }{
		{"", "a/b.zip", "a/b.zip"},
		{"/root/", "a/b.zip", "root/a/b.zip"},
		{"root", "/a/b.zip", "root/a/b.zip"},
		{"root//sub", "a.zip", "root/sub/a.zip"},
		{"root", "", "root"},
	}
	for _, c := range cases {
		if got := joinKey(c.prefix, c.key); got != c.want {
			t.Errorf("joinKey(%q,%q) = %q, want %q", c.prefix, c.key, got, c.want)
		}
	}
}

func TestStripPrefix(t *testing.T) {
	if got := stripPrefix("frpc/a/b.zip", "frpc"); got != "a/b.zip" {
		t.Errorf("stripPrefix = %q", got)
	}
	if got := stripPrefix("a/b.zip", ""); got != "a/b.zip" {
		t.Errorf("stripPrefix empty base = %q", got)
	}
}

func TestValidateChannel(t *testing.T) {
	ok := Channel{Name: "x", Kind: KindS3, S3: &S3Config{Endpoint: "e", Bucket: "b"}}
	if err := ValidateChannel(ok); err != nil {
		t.Fatalf("valid s3 rejected: %v", err)
	}
	bad := []Channel{
		{Name: "", Kind: KindS3, S3: &S3Config{Endpoint: "e", Bucket: "b"}},
		{Name: "x", Kind: KindS3, S3: &S3Config{Endpoint: "", Bucket: "b"}},
		{Name: "x", Kind: KindS3, S3: &S3Config{Endpoint: "e", Bucket: ""}},
		{Name: "x", Kind: KindS3},
		{Name: "x", Kind: KindWebDAV, WebDAV: &WebDAVConfig{BaseURL: ""}},
		{Name: "x", Kind: "ftp"},
	}
	for i, ch := range bad {
		if err := ValidateChannel(ch); err == nil {
			t.Errorf("bad channel %d accepted", i)
		}
	}
	wd := Channel{Name: "x", Kind: KindWebDAV, WebDAV: &WebDAVConfig{BaseURL: "https://d/"}}
	if err := ValidateChannel(wd); err != nil {
		t.Fatalf("valid webdav rejected: %v", err)
	}
}

func TestValidateSchedule(t *testing.T) {
	ok := Schedule{Name: "d", ChannelID: "ch1", Cron: "0 3 * * *", Retention: 7}
	if err := ValidateSchedule(ok); err != nil {
		t.Fatalf("valid schedule rejected: %v", err)
	}
	bad := []Schedule{
		{Name: "", ChannelID: "c", Cron: "@daily"},
		{Name: "d", ChannelID: "", Cron: "@daily"},
		{Name: "d", ChannelID: "c", Cron: "not a cron"},
		{Name: "d", ChannelID: "c", Cron: "@daily", Retention: -1},
	}
	for i, s := range bad {
		if err := ValidateSchedule(s); err == nil {
			t.Errorf("bad schedule %d accepted", i)
		}
	}
	if err := ValidateSchedule(Schedule{Name: "d", ChannelID: "c", Cron: "@every 6h"}); err != nil {
		t.Errorf("@every descriptor rejected: %v", err)
	}
}

func TestMergeChannelSecrets(t *testing.T) {
	old := Channel{Kind: KindS3, S3: &S3Config{SecretAccessKey: "OLD"}}
	// Blank incoming secret → keep old.
	neu := Channel{Kind: KindS3, S3: &S3Config{SecretAccessKey: ""}}
	if got := MergeChannelSecrets(old, neu); got.S3.SecretAccessKey != "OLD" {
		t.Errorf("blank secret not preserved: %q", got.S3.SecretAccessKey)
	}
	// Provided secret → replace.
	neu2 := Channel{Kind: KindS3, S3: &S3Config{SecretAccessKey: "NEW"}}
	if got := MergeChannelSecrets(old, neu2); got.S3.SecretAccessKey != "NEW" {
		t.Errorf("secret not replaced: %q", got.S3.SecretAccessKey)
	}
	// WebDAV password.
	oldW := Channel{Kind: KindWebDAV, WebDAV: &WebDAVConfig{Password: "PW"}}
	neuW := Channel{Kind: KindWebDAV, WebDAV: &WebDAVConfig{Password: ""}}
	if got := MergeChannelSecrets(oldW, neuW); got.WebDAV.Password != "PW" {
		t.Errorf("blank webdav password not preserved")
	}
}

func TestObjectMatcher(t *testing.T) {
	m := objectMatcher(DefaultPathTemplate, "每日", "h")
	if m == nil {
		t.Fatal("matcher nil for default template")
	}
	// This schedule's own object matches.
	if !m.MatchString("frpcmgr-backups/每日/2026/06/frpcmgr-20260613-030005.zip") {
		t.Error("own object should match")
	}
	// A foreign file in the same dir must NOT match (won't be deleted by retention).
	if m.MatchString("frpcmgr-backups/每日/2026/06/manual-keep.zip") {
		t.Error("foreign file should not match")
	}
	if m.MatchString("frpcmgr-backups/每日/important.zip") {
		t.Error("foreign top-level file should not match")
	}
	// Another schedule's object must NOT match.
	if m.MatchString("frpcmgr-backups/每周/2026/06/frpcmgr-20260613-030005.zip") {
		t.Error("other schedule's object should not match")
	}
}

func TestScopeSignature(t *testing.T) {
	a := scopeSignature(DefaultPathTemplate, "每日", "h")
	b := scopeSignature(DefaultPathTemplate, "每日", "h")
	c := scopeSignature(DefaultPathTemplate, "每周", "h")
	if a != b {
		t.Error("same name+template should share a signature (collision)")
	}
	if a == c {
		t.Error("different schedule name should differ")
	}
	// slug collision is detected: "每日 备份"(space) and "每日/备份"(slash) → same slug.
	if scopeSignature(DefaultPathTemplate, "每日 备份", "h") != scopeSignature(DefaultPathTemplate, "每日/备份", "h") {
		t.Error("slug-colliding names should collide in signature")
	}
}

func TestRedactSecrets(t *testing.T) {
	ch := Channel{Kind: KindS3, S3: &S3Config{AccessKeyID: "ak", SecretAccessKey: "SK"}}
	r := RedactSecrets(ch)
	if r.S3.SecretAccessKey != "" {
		t.Error("secret not redacted")
	}
	if r.S3.AccessKeyID != "ak" {
		t.Error("non-secret over-redacted")
	}
	if ch.S3.SecretAccessKey != "SK" {
		t.Error("original mutated (not a copy)")
	}
	w := RedactSecrets(Channel{Kind: KindWebDAV, WebDAV: &WebDAVConfig{Username: "u", Password: "PW"}})
	if w.WebDAV.Password != "" || w.WebDAV.Username != "u" {
		t.Error("webdav redact wrong")
	}
}

func TestNormalizeChannel(t *testing.T) {
	// kind=webdav must drop a stale s3 block (which could hide a secret).
	ch := Channel{Kind: KindWebDAV, S3: &S3Config{SecretAccessKey: "leak"}, WebDAV: &WebDAVConfig{BaseURL: "u"}}
	NormalizeChannel(&ch)
	if ch.S3 != nil {
		t.Error("off-kind s3 not dropped")
	}
	ch2 := Channel{Kind: KindS3, S3: &S3Config{Bucket: "b"}, WebDAV: &WebDAVConfig{Password: "leak"}}
	NormalizeChannel(&ch2)
	if ch2.WebDAV != nil {
		t.Error("off-kind webdav not dropped")
	}
}

func TestChannelHasSecretAndClone(t *testing.T) {
	ch := Channel{Kind: KindS3, S3: &S3Config{SecretAccessKey: "s"}}
	if !ch.HasSecret() {
		t.Error("HasSecret should be true")
	}
	clone := ch.Clone()
	clone.S3.SecretAccessKey = "mutated"
	if ch.S3.SecretAccessKey != "s" {
		t.Error("Clone is not a deep copy: mutation leaked")
	}
}
