package backup

import (
	"regexp"
	"strings"
	"time"
)

// timeTokens are the placeholders whose value depends on the backup instant.
// Everything before the first time token (after rendering the stable
// {schedule}/{host} tokens) is treated as the schedule's stable directory and
// used as the retention list root.
var timeTokens = []string{
	"{year}", "{YYYY}", "{month}", "{MM}", "{day}", "{DD}",
	"{date}", "{time}", "{datetime}", "{ts}",
}

// slug makes a name safe for use as a single object-key / path segment: path
// separators and control chars become '-', whitespace runs collapse to '-',
// and leading/trailing '-'/'.' are trimmed. CJK and most printable characters
// are preserved, so "每日" stays "每日". Empty input yields "default".
func slug(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		switch {
		case r == '/' || r == '\\' || r < 0x20 || r == 0x7f:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		case r == ' ' || r == '\t':
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		default:
			b.WriteRune(r)
			lastDash = false
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "default"
	}
	return out
}

// renderPath expands a path template into a concrete object key for instant t
// (rendered in t's location; callers pass UTC). The result has no leading slash
// and no duplicate slashes.
func renderPath(template, scheduleName, host string, t time.Time) string {
	r := strings.NewReplacer(
		"{schedule}", slug(scheduleName),
		"{host}", slug(host),
		"{year}", t.Format("2006"), "{YYYY}", t.Format("2006"),
		"{month}", t.Format("01"), "{MM}", t.Format("01"),
		"{day}", t.Format("02"), "{DD}", t.Format("02"),
		"{date}", t.Format("20060102"),
		"{time}", t.Format("150405"),
		"{datetime}", t.Format("20060102-150405"),
		"{ts}", t.Format("20060102-150405"),
	)
	return cleanKey(r.Replace(template))
}

// retentionPrefix derives the stable directory (ending in '/') under which a
// schedule's backups accumulate, i.e. the template truncated to just before the
// first time token. Returns "" when no stable directory can be determined (a
// time token appears in the first path segment) — callers must then skip
// retention rather than risk listing/deleting an over-broad scope.
func retentionPrefix(template, scheduleName, host string) string {
	s := strings.NewReplacer(
		"{schedule}", slug(scheduleName),
		"{host}", slug(host),
	).Replace(template)
	for _, tk := range timeTokens {
		s = strings.ReplaceAll(s, tk, "\x00")
	}
	if i := strings.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	// Cut back to the last directory boundary so we never list a partial
	// segment as a prefix. s[:j+1] keeps the trailing slash.
	if j := strings.LastIndexByte(s, '/'); j >= 0 {
		return cleanKey(s[:j+1])
	}
	return ""
}

// matcherPattern builds an anchored regexp source that matches exactly the
// object keys this schedule produces: literal text is escaped, {schedule}/{host}
// become the rendered slug, and time tokens become digit classes. Two schedules
// with the same pattern (same channel) would share a retention pool — callers
// use the pattern as a collision signature.
func matcherPattern(template, scheduleName, host string) string {
	tpl := cleanKey(template)
	var b strings.Builder
	b.WriteByte('^')
	i := 0
	for i < len(tpl) {
		if tpl[i] == '{' {
			if j := strings.IndexByte(tpl[i:], '}'); j >= 0 {
				b.WriteString(tokenPattern(tpl[i:i+j+1], scheduleName, host))
				i += j + 1
				continue
			}
		}
		b.WriteString(regexp.QuoteMeta(string(tpl[i])))
		i++
	}
	b.WriteByte('$')
	return b.String()
}

func tokenPattern(tok, scheduleName, host string) string {
	switch tok {
	case "{schedule}":
		return regexp.QuoteMeta(slug(scheduleName))
	case "{host}":
		return regexp.QuoteMeta(slug(host))
	case "{year}", "{YYYY}":
		return `\d{4}`
	case "{month}", "{MM}", "{day}", "{DD}":
		return `\d{2}`
	case "{date}":
		return `\d{8}`
	case "{time}":
		return `\d{6}`
	case "{datetime}", "{ts}":
		return `\d{8}-\d{6}`
	default:
		return regexp.QuoteMeta(tok) // unknown token: match literally
	}
}

// objectMatcher compiles matcherPattern; nil on a pathological template (callers
// then skip retention rather than risk an over-broad delete).
func objectMatcher(template, scheduleName, host string) *regexp.Regexp {
	re, err := regexp.Compile(matcherPattern(template, scheduleName, host))
	if err != nil {
		return nil
	}
	return re
}

// scopeSignature identifies a schedule's retention pool: same channel + same
// signature ⇒ the two schedules would delete each other's backups.
func scopeSignature(template, scheduleName, host string) string {
	return matcherPattern(template, scheduleName, host)
}

// cleanKey collapses duplicate slashes and strips a leading slash, yielding a
// canonical object key. A trailing slash is preserved (used by prefixes).
func cleanKey(k string) string {
	for strings.Contains(k, "//") {
		k = strings.ReplaceAll(k, "//", "/")
	}
	return strings.TrimPrefix(k, "/")
}

// joinKey joins a base prefix with a relative key into one canonical object
// key (single slashes, no leading slash).
func joinKey(prefix, key string) string {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	key = cleanKey(key)
	if prefix == "" {
		return key
	}
	if key == "" {
		return prefix
	}
	return cleanKey(prefix + "/" + key)
}
