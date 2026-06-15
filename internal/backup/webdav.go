package backup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/studio-b12/gowebdav"
)

// webdavTimeout bounds each individual WebDAV HTTP request. Backups are small
// (zipped TOML), so a couple of minutes is generous.
const webdavTimeout = 2 * time.Minute

// webdavUploader stores backups on a WebDAV server (Nextcloud, Jianguoyun,
// Synology, ...) via gowebdav. The client is rooted at base_url; the channel
// prefix is an extra folder under it.
type webdavUploader struct {
	cli    *gowebdav.Client
	prefix string
}

func newWebDAVUploader(c *WebDAVConfig) (*webdavUploader, error) {
	if c == nil {
		return nil, fmt.Errorf("webdav 配置为空")
	}
	base := strings.TrimSpace(c.BaseURL)
	if base == "" {
		return nil, fmt.Errorf("webdav base_url 必填")
	}
	cli := gowebdav.NewClient(base, c.Username, c.Password)
	cli.SetTimeout(webdavTimeout)
	return &webdavUploader{cli: cli, prefix: c.Prefix}, nil
}

// davPath maps a channel-relative key to an absolute WebDAV path under base_url.
func (u *webdavUploader) davPath(rel string) string {
	return "/" + joinKey(u.prefix, rel)
}

func (u *webdavUploader) Put(_ context.Context, key string, data []byte) error {
	p := u.davPath(key)
	if dir := pathDir(p); dir != "" && dir != "/" {
		// Best effort: ensure the parent collection chain exists. A real failure
		// surfaces from Write below.
		_ = u.cli.MkdirAll(dir, 0o755)
	}
	if err := u.cli.Write(p, data, 0o644); err != nil {
		return fmt.Errorf("写入 WebDAV 失败：%w", err)
	}
	return nil
}

func (u *webdavUploader) Get(_ context.Context, key string) ([]byte, error) {
	data, err := u.cli.Read(u.davPath(key))
	if err != nil {
		return nil, fmt.Errorf("读取 WebDAV 对象失败：%w", err)
	}
	return data, nil
}

func (u *webdavUploader) List(_ context.Context, prefix string) ([]Object, error) {
	raw, err := u.walk(u.davPath(prefix))
	if err != nil {
		return nil, err
	}
	base := "/" + joinKey(u.prefix, "")
	out := make([]Object, 0, len(raw))
	for _, o := range raw {
		rel := strings.TrimPrefix(o.Key, base)
		rel = strings.TrimPrefix(rel, "/")
		// 必须带上 Modified：walk() 已从 PROPFIND 取到 ModTime，这里重组 Object
		// 裁剪 prefix 时若漏掉它，o.Modified 会退化成 time.Time{} 零值，
		// 经 API 的 .Unix() 变成负数，前端渲染成 0001-01-01（上海 LMT → 1/1/1 08:05:43）。
		out = append(out, Object{Key: rel, Size: o.Size, Modified: o.Modified})
	}
	return out, nil
}

// walk recursively lists files under dir. A missing directory yields no
// objects (first run before any backup exists), not an error.
func (u *webdavUploader) walk(dir string) ([]Object, error) {
	entries, err := u.cli.ReadDir(dir)
	if err != nil {
		if gowebdav.IsErrNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("列举 WebDAV 目录失败：%w", err)
	}
	var out []Object
	for _, e := range entries {
		child := strings.TrimSuffix(dir, "/") + "/" + e.Name()
		if e.IsDir() {
			sub, err := u.walk(child)
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
		} else {
			out = append(out, Object{Key: child, Size: e.Size(), Modified: e.ModTime()})
		}
	}
	return out, nil
}

func (u *webdavUploader) Delete(_ context.Context, key string) error {
	if err := u.cli.Remove(u.davPath(key)); err != nil {
		return fmt.Errorf("删除 WebDAV 对象失败：%w", err)
	}
	return nil
}

func (u *webdavUploader) Test(_ context.Context) error {
	if err := u.cli.Connect(); err != nil {
		return fmt.Errorf("连接 WebDAV 失败（检查地址 / 账号 / 口令）：%w", err)
	}
	// If a base prefix is set, probe it but tolerate not-found — it is created
	// lazily on the first backup.
	if strings.TrimSpace(u.prefix) != "" {
		if _, err := u.cli.Stat(u.davPath("")); err != nil && !gowebdav.IsErrNotFound(err) {
			return fmt.Errorf("访问 WebDAV 路径 %q 失败：%w", u.prefix, err)
		}
	}
	return nil
}

// pathDir returns the directory portion of a slash path ("" if none).
func pathDir(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}
