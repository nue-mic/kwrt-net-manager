package backup

import (
	"context"
	"fmt"
	"time"
)

// Object is a stored backup object, keyed relative to the channel's base prefix.
type Object struct {
	Key      string // channel-prefix-relative key
	Size     int64
	Modified time.Time // server-side last-modified; used to order retention reliably
}

// Uploader is the storage abstraction every channel kind implements. All keys
// and prefixes are RELATIVE to the channel's configured base prefix; each
// implementation joins/strips that prefix internally, so callers (the scheduler
// and its retention logic) work in one consistent key space.
type Uploader interface {
	// Put stores data at the relative key (overwriting), creating parent
	// directories where the backend requires them (WebDAV).
	Put(ctx context.Context, key string, data []byte) error
	// Get downloads the object at the relative key.
	Get(ctx context.Context, key string) ([]byte, error)
	// List returns every object under the relative prefix (recursive).
	List(ctx context.Context, prefix string) ([]Object, error)
	// Delete removes the object at the relative key.
	Delete(ctx context.Context, key string) error
	// Test verifies connectivity and credentials without writing anything.
	Test(ctx context.Context) error
}

// NewUploader builds the Uploader for a channel based on its kind.
func NewUploader(ch Channel) (Uploader, error) {
	switch ch.Kind {
	case KindS3:
		return newS3Uploader(ch.S3)
	case KindWebDAV:
		return newWebDAVUploader(ch.WebDAV)
	default:
		return nil, fmt.Errorf("未知的存储渠道类型 %q", ch.Kind)
	}
}
