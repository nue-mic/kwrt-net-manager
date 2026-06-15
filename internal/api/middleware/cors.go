package middleware

import (
	"net/http"
	"strings"
)

// CORS adds the standard CORS headers based on the allowed origins returned by
// originsFn, read per-request so an operator can change the list at runtime
// (KWRTNET_CORS_ORIGINS default, overridable via the system-config UI).
// "*" allows any origin (no credentials).
func CORS(originsFn func() []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed := originsFn()
			allowAll := IsWildcard(allowed)
			origin := r.Header.Get("Origin")
			if origin != "" {
				if allowAll {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				} else {
					for _, o := range allowed {
						if o == origin {
							w.Header().Set("Access-Control-Allow-Origin", origin)
							w.Header().Set("Vary", "Origin")
							break
						}
					}
				}
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// IsWildcard reports whether the CORS origin list is the wildcard set.
func IsWildcard(origins []string) bool {
	if len(origins) != 1 {
		return false
	}
	return strings.TrimSpace(origins[0]) == "*"
}

// NormalizeOrigins canonicalizes a CORS origin list so the HTTP middleware and
// the WebSocket OriginPatterns agree on its meaning:
//   - each entry is trimmed; blank entries are dropped;
//   - if "*" appears anywhere, the whole list collapses to ["*"]. A mixed list
//     like ["*", "https://x"] is dangerous: the HTTP side does exact-match and
//     silently ignores the "*", while the WS side feeds it to path.Match where
//     "*" matches every origin — so the two layers would disagree and the WS
//     would wildcard-allow any cross-origin upgrade. Collapsing makes both honor
//     the wildcard consistently.
//
// It returns nil when nothing meaningful remains, so callers can treat that as
// "no override / fall back to the env default" instead of "deny everything".
func NormalizeOrigins(in []string) []string {
	out := make([]string, 0, len(in))
	for _, o := range in {
		t := strings.TrimSpace(o)
		if t == "" {
			continue
		}
		if t == "*" {
			return []string{"*"}
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
