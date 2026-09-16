package schemas

import (
	"strings"
	"testing"
)

func TestAttributionValueOK(t *testing.T) {
	valid := []string{"alice", " user-42 ", "user@example.com", "αβγ-user"}
	for _, v := range valid {
		if !AttributionValueOK(v) {
			t.Errorf("AttributionValueOK(%q) = false, want true", v)
		}
	}
	invalid := []string{
		"",
		"   ",
		"a\rb",
		"a\nb",
		"a\r\nb",
		strings.Repeat("x", MaxAttributionValueLength+1),
		string([]byte{0xff, 0xfe}), // invalid UTF-8
	}
	for _, v := range invalid {
		if AttributionValueOK(v) {
			t.Errorf("AttributionValueOK(%q) = true, want false", v)
		}
	}
	if !AttributionValueOK(strings.Repeat("x", MaxAttributionValueLength)) {
		t.Error("value at exactly the cap should be accepted")
	}
}

func TestNormalizeAttributionHeader(t *testing.T) {
	name, ok := NormalizeAttributionHeader(" X-User-ID ")
	if !ok || name != "x-user-id" {
		t.Errorf("NormalizeAttributionHeader = %q, %v; want x-user-id, true", name, ok)
	}
	for _, bad := range []string{"", "  ", "x user", "x\tuser", "user: id", "usér", strings.Repeat("h", 129)} {
		if _, ok := NormalizeAttributionHeader(bad); ok {
			t.Errorf("NormalizeAttributionHeader(%q) = true, want false", bad)
		}
	}
}

func TestIsAttributionHeaderAllowed(t *testing.T) {
	for _, allowed := range []string{"x-user-id", "x-user-name", "x-auth0-user", "x-neptune-user"} {
		if !IsAttributionHeaderAllowed(allowed) {
			t.Errorf("IsAttributionHeaderAllowed(%q) = false, want true", allowed)
		}
	}
	for _, denied := range []string{
		"authorization", "proxy-authorization", "cookie", "set-cookie",
		"x-api-key", "x-bf-vk", "x-bf-user", "x-bf-user-id",
		"cf-access-jwt-assertion", "x-amzn-oidc-data", "x-session-token",
		"host", "content-length", "connection", "transfer-encoding",
		"forwarded", "x-forwarded-for", "x-real-ip",
	} {
		if IsAttributionHeaderAllowed(denied) {
			t.Errorf("IsAttributionHeaderAllowed(%q) = true, want false", denied)
		}
	}
}

func TestAttributionIdentityFromHeaders(t *testing.T) {
	headers := map[string]string{
		"x-user-id":   "alice",
		"x-user-name": "Alice A.",
		"x-email":     "alice@example.com",
		"x-empty":     "   ",
		"x-toobig":    strings.Repeat("x", MaxAttributionValueLength+1),
	}

	// First configured header with a valid value wins.
	id, ok := AttributionIdentityFromHeaders(headers, []string{"x-user-id", "x-email"})
	if !ok || id.ID != "alice" || id.Name != "alice" {
		t.Errorf("identity = %+v, %v; want {alice alice}, true", id, ok)
	}
	// '='-prefixed entry supplies the name only.
	id, ok = AttributionIdentityFromHeaders(headers, []string{"x-user-id", "=x-user-name"})
	if !ok || id.ID != "alice" || id.Name != "Alice A." {
		t.Errorf("identity = %+v, %v; want {alice Alice A.}, true", id, ok)
	}
	// Name-only entries never supply the id.
	if _, ok = AttributionIdentityFromHeaders(headers, []string{"=x-user-name"}); ok {
		t.Error("name-only config must not produce an identity")
	}
	// Invalid values are skipped; a later valid header still wins.
	id, ok = AttributionIdentityFromHeaders(headers, []string{"x-toobig", "x-empty", "x-email"})
	if !ok || id.ID != "alice@example.com" {
		t.Errorf("identity = %+v, %v; want id alice@example.com, true", id, ok)
	}
	// No configured header present: no identity.
	if _, ok = AttributionIdentityFromHeaders(headers, []string{"x-absent"}); ok {
		t.Error("absent headers must not produce an identity")
	}
	// Nothing configured: no identity.
	if _, ok = AttributionIdentityFromHeaders(headers, nil); ok {
		t.Error("empty config must not produce an identity")
	}
}

func TestReportingUserIDNamePrecedence(t *testing.T) {
	ctx := NewBifrostContext(nil, NoDeadline)

	// Neither authenticated nor reporting: empty.
	if got := ctx.ReportingUserID(); got != "" {
		t.Errorf("ReportingUserID = %q, want empty", got)
	}
	if got := ctx.ReportingUserName(); got != "" {
		t.Errorf("ReportingUserName = %q, want empty", got)
	}

	// Reporting-only: used for reporting, never visible as authenticated identity.
	ctx.SetValue(BifrostContextKeyReportingUserID, "hdr-user")
	if got := ctx.ReportingUserID(); got != "hdr-user" {
		t.Errorf("ReportingUserID = %q, want hdr-user", got)
	}
	if got := ctx.ReportingUserName(); got != "hdr-user" {
		t.Errorf("ReportingUserName = %q, want hdr-user (id fallback)", got)
	}
	if v, _ := ctx.Value(BifrostContextKeyUserID).(string); v != "" {
		t.Errorf("authenticated BifrostContextKeyUserID must stay empty, got %q", v)
	}
	if v, _ := ctx.Value(BifrostContextKeyUserName).(string); v != "" {
		t.Errorf("authenticated BifrostContextKeyUserName must stay empty, got %q", v)
	}
	if mode := ctx.MCPAuthMode(); mode != MCPAuthModeNone {
		t.Errorf("MCPAuthMode = %v, want MCPAuthModeNone — a reporting label must not authenticate", mode)
	}

	// Authenticated identity wins over reporting labels.
	ctx.SetValue(BifrostContextKeyUserID, "auth-user")
	ctx.SetValue(BifrostContextKeyUserName, "Auth User")
	if got := ctx.ReportingUserID(); got != "auth-user" {
		t.Errorf("ReportingUserID = %q, want auth-user", got)
	}
	if got := ctx.ReportingUserName(); got != "Auth User" {
		t.Errorf("ReportingUserName = %q, want Auth User", got)
	}
	if mode := ctx.MCPAuthMode(); mode != MCPAuthModeUser {
		t.Errorf("MCPAuthMode = %v, want MCPAuthModeUser", mode)
	}
}
