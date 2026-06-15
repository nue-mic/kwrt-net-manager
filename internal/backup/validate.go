package backup

import (
	"errors"
	"fmt"
	"strings"
)

// ValidateChannel checks a channel has the minimum fields its kind needs.
// Connectivity/credential correctness is verified separately via Test.
func ValidateChannel(ch Channel) error {
	if strings.TrimSpace(ch.Name) == "" {
		return errors.New("渠道名称不能为空")
	}
	switch ch.Kind {
	case KindS3:
		if ch.S3 == nil {
			return errors.New("缺少 s3 配置")
		}
		if strings.TrimSpace(ch.S3.Endpoint) == "" {
			return errors.New("s3 endpoint 必填")
		}
		if strings.TrimSpace(ch.S3.Bucket) == "" {
			return errors.New("s3 bucket 必填")
		}
	case KindWebDAV:
		if ch.WebDAV == nil {
			return errors.New("缺少 webdav 配置")
		}
		if strings.TrimSpace(ch.WebDAV.BaseURL) == "" {
			return errors.New("webdav base_url 必填")
		}
	default:
		return fmt.Errorf("不支持的渠道类型 %q（仅支持 s3 / webdav）", ch.Kind)
	}
	return nil
}

// ValidateSchedule checks a schedule's own fields. The caller separately
// verifies that ChannelID references an existing channel.
func ValidateSchedule(s Schedule) error {
	if strings.TrimSpace(s.Name) == "" {
		return errors.New("计划名称不能为空")
	}
	if strings.TrimSpace(s.ChannelID) == "" {
		return errors.New("必须选择一个存储渠道")
	}
	if err := ValidateCron(s.Cron); err != nil {
		return err
	}
	if s.Retention < 0 {
		return errors.New("保留份数不能为负数")
	}
	return nil
}

// MergeChannelSecrets carries forward the old channel's secrets when the
// incoming update leaves them blank, implementing "leave the password field
// empty to keep the current one".
func MergeChannelSecrets(old, neu Channel) Channel {
	if neu.S3 != nil && old.S3 != nil && strings.TrimSpace(neu.S3.SecretAccessKey) == "" {
		neu.S3.SecretAccessKey = old.S3.SecretAccessKey
	}
	if neu.WebDAV != nil && old.WebDAV != nil && strings.TrimSpace(neu.WebDAV.Password) == "" {
		neu.WebDAV.Password = old.WebDAV.Password
	}
	return neu
}

// NormalizeChannel drops the sub-config that doesn't match the channel's kind,
// so a channel never carries a stale (and invisible) off-kind secret — e.g. an
// s3 block left over after switching the channel to webdav.
func NormalizeChannel(c *Channel) {
	switch c.Kind {
	case KindS3:
		c.WebDAV = nil
	case KindWebDAV:
		c.S3 = nil
	}
}

// RedactSecrets returns a deep copy of the channel with secrets blanked. Used
// to strip credentials out of the backup archive so they never ride along to
// the (potentially shared) storage destination or an exported download.
func RedactSecrets(c Channel) Channel {
	out := c.Clone()
	if out.S3 != nil {
		out.S3.SecretAccessKey = ""
	}
	if out.WebDAV != nil {
		out.WebDAV.Password = ""
	}
	return out
}

// HasSecret reports whether the channel currently holds a non-empty secret for
// its kind (used to render the *_set flags in API responses).
func (c Channel) HasSecret() bool {
	switch c.Kind {
	case KindS3:
		return c.S3 != nil && strings.TrimSpace(c.S3.SecretAccessKey) != ""
	case KindWebDAV:
		return c.WebDAV != nil && strings.TrimSpace(c.WebDAV.Password) != ""
	}
	return false
}
