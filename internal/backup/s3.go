package backup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// s3Uploader stores backups in any S3-compatible object store via minio-go.
type s3Uploader struct {
	cli    *minio.Client
	bucket string
	prefix string // base folder inside the bucket (may be empty)
}

func newS3Uploader(c *S3Config) (*s3Uploader, error) {
	if c == nil {
		return nil, fmt.Errorf("s3 配置为空")
	}
	ep := strings.TrimSpace(c.Endpoint)
	// Tolerate a pasted URL: strip scheme and trailing slash. The scheme is
	// controlled by UseSSL, not the endpoint string.
	ep = strings.TrimPrefix(ep, "https://")
	ep = strings.TrimPrefix(ep, "http://")
	ep = strings.TrimSuffix(ep, "/")
	if ep == "" {
		return nil, fmt.Errorf("s3 endpoint 必填")
	}
	if strings.TrimSpace(c.Bucket) == "" {
		return nil, fmt.Errorf("s3 bucket 必填")
	}
	lookup := minio.BucketLookupAuto
	if c.PathStyle {
		lookup = minio.BucketLookupPath
	}
	cli, err := minio.New(ep, &minio.Options{
		Creds:        credentials.NewStaticV4(c.AccessKeyID, c.SecretAccessKey, ""),
		Secure:       c.UseSSL,
		Region:       strings.TrimSpace(c.Region),
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化 s3 客户端失败：%w", err)
	}
	return &s3Uploader{cli: cli, bucket: strings.TrimSpace(c.Bucket), prefix: c.Prefix}, nil
}

func (u *s3Uploader) Put(ctx context.Context, key string, data []byte) error {
	full := joinKey(u.prefix, key)
	_, err := u.cli.PutObject(ctx, u.bucket, full, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "application/zip"})
	return err
}

func (u *s3Uploader) Get(ctx context.Context, key string) ([]byte, error) {
	full := joinKey(u.prefix, key)
	obj, err := u.cli.GetObject(ctx, u.bucket, full, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	return io.ReadAll(obj)
}

func (u *s3Uploader) List(ctx context.Context, prefix string) ([]Object, error) {
	base := joinKey(u.prefix, "") // trimmed channel prefix, no trailing slash
	listPrefix := joinKey(u.prefix, prefix)
	var out []Object
	for obj := range u.cli.ListObjects(ctx, u.bucket, minio.ListObjectsOptions{
		Prefix:    listPrefix,
		Recursive: true,
	}) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		out = append(out, Object{Key: stripPrefix(obj.Key, base), Size: obj.Size, Modified: obj.LastModified})
	}
	return out, nil
}

func (u *s3Uploader) Delete(ctx context.Context, key string) error {
	full := joinKey(u.prefix, key)
	return u.cli.RemoveObject(ctx, u.bucket, full, minio.RemoveObjectOptions{})
}

func (u *s3Uploader) Test(ctx context.Context) error {
	exists, err := u.cli.BucketExists(ctx, u.bucket)
	if err != nil {
		return fmt.Errorf("连接 s3 失败：%w", err)
	}
	if !exists {
		return fmt.Errorf("bucket %q 不存在或无访问权限", u.bucket)
	}
	return nil
}

// stripPrefix removes the channel base prefix from a full object key, yielding
// the channel-relative key the Uploader contract promises.
func stripPrefix(full, base string) string {
	if base == "" {
		return full
	}
	rel := strings.TrimPrefix(full, base)
	return strings.TrimPrefix(rel, "/")
}
