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
// the request: the value of the configured id header when it is valid, with
// the configured name header's value as the display name (the id value doubles
// as the name when no name header is configured or its value is invalid).
// headers is the lowercased request-header map built by
// ConvertToBifrostContext; idHeader and nameHeader are normalized header
// names (nameHeader may be empty).
func AttributionIdentityFromHeaders(headers map[string]string, idHeader, nameHeader string) (AttributionIdentity, bool) {
	if idHeader == "" {
		return AttributionIdentity{}, false
	}
	raw, ok := headers[idHeader]
	if !ok || !AttributionValueOK(raw) {
		return AttributionIdentity{}, false
	}
	id := strings.TrimSpace(raw)
	name := id
	if nameHeader != "" && nameHeader != idHeader {
		if rawName, ok := headers[nameHeader]; ok && AttributionValueOK(rawName) {
			name = strings.TrimSpace(rawName)
		}
	}
	return AttributionIdentity{ID: id, Name: name}, true
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
