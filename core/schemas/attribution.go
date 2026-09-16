package schemas

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxAttributionValueLength caps caller-asserted reporting attribution labels.
// Log user_id/user_name columns and span attributes are fixed-width, and an
// unbounded header value would otherwise be persisted verbatim.
const MaxAttributionValueLength = 128

// AttributionValueOK reports whether a raw attribution header value is usable
// as a reporting label: trimmed non-empty, within MaxAttributionValueLength
// runes, valid UTF-8, and free of CR/LF (a header value must never smuggle a
// line break into a log field or span attribute).
func AttributionValueOK(raw string) bool {
	v := strings.TrimSpace(raw)
	if v == "" || utf8.RuneCountInString(v) > MaxAttributionValueLength || !utf8.ValidString(v) {
		return false
	}
	return !strings.ContainsAny(v, "\r\n")
}

// AttributionIdentity is a request-scoped, reporting-only user label captured
// from a configured inbound header. It feeds log user_id/user_name columns and
// bifrost.user.* span attributes only — it is NOT an authenticated identity
// and must never populate BifrostContextKeyUserID/UserName/UserEmail, which
// drive credential lookup, grants, and pricing overrides.
type AttributionIdentity struct {
	ID   string
	Name string
}

// AttributionIdentityFromHeaders returns the reporting identity asserted by
// the request under the configured attribution header names: the first listed
// header carrying a valid value wins, and the id header doubles as the name
// when no separate name header carries one. Entries prefixed with "=" are
// name-only captures — eligible for the name, never for the id. headers is
// the lowercased request-header map built by ConvertToBifrostContext.
func AttributionIdentityFromHeaders(headers map[string]string, configured []string) (AttributionIdentity, bool) {
	idHeaders := make([]string, 0, len(configured))
	nameOnly := make([]string, 0, len(configured))
	for _, name := range configured {
		if strings.HasPrefix(name, "=") {
			nameOnly = append(nameOnly, name)
		} else {
			idHeaders = append(idHeaders, name)
		}
	}
	id, ok := firstAttributionHeaderValue(headers, idHeaders)
	if !ok {
		return AttributionIdentity{}, false
	}
	name, nameOK := firstAttributionHeaderValue(headers, nameOnly)
	if !nameOK {
		name = id
	}
	return AttributionIdentity{ID: id, Name: name}, true
}

// firstAttributionHeaderValue returns the first valid, trimmed value found
// under the given header names, in config order. A name prefixed with "=" is
// capture-only: its value is eligible but it never names the id source.
func firstAttributionHeaderValue(headers map[string]string, names []string) (string, bool) {
	for _, name := range names {
		name = strings.TrimPrefix(name, "=")
		if raw, ok := headers[name]; ok && AttributionValueOK(raw) {
			return strings.TrimSpace(raw), true
		}
	}
	return "", false
}

// NormalizeAttributionHeader validates a configured attribution header name:
// a syntactically valid HTTP token (RFC 9110), lowercased for lookup against
// the lowercased request-header map.
func NormalizeAttributionHeader(raw string) (string, bool) {
	name := strings.ToLower(strings.TrimSpace(raw))
	if name == "" || len(name) > 128 {
		return "", false
	}
	for _, r := range name {
		if r > unicode.MaxASCII || !isHTTPTokenChar(byte(r)) {
			return "", false
		}
	}
	return name, true
}

func isHTTPTokenChar(c byte) bool {
	if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
		return true
	}
	switch c {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return false
}

// IsAttributionHeaderAllowed reports whether a normalized header name may be
// configured as an attribution source. Credential-bearing, transport-reserved,
// and hop-by-hop headers are refused so an attribution config can neither
// exfiltrate secrets into log columns nor shadow a header Bifrost itself
// consumes (x-bf-*, session/cookie, etc.). The name must already be normalized
// via NormalizeAttributionHeader.
func IsAttributionHeaderAllowed(name string) bool {
	if IsSensitiveHeader(name) {
		return false
	}
	if strings.HasPrefix(name, "x-bf-") {
		return false
	}
	switch name {
	case "host", "content-length", "connection", "transfer-encoding", "forwarded", "x-forwarded-for", "x-real-ip":
		return false
	}
	return true
}
