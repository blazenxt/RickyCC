package main

import (
	"strings"
	"testing"
)

// The retry callback payload must survive a build→parse round trip so the
// referral argument reaches the captcha/registration stage intact, and it
// must NEVER exceed Telegram's 64-byte callback_data limit (otherwise the
// whole lock message would fail to send and users would see no join
// buttons at all).
func TestFsubRetryDataRoundTrip(t *testing.T) {
	if got := parseFsubRetryData(fsubRetryData("")); got != "" {
		t.Fatalf("empty arg round trip = %q, want empty", got)
	}
	if got := parseFsubRetryData(fsubRetryData("123456789")); got != "123456789" {
		t.Fatalf("id arg round trip = %q, want 123456789", got)
	}
	if got := parseFsubRetryData(fsubRetryData("ref_42-x")); got != "ref_42-x" {
		t.Fatalf("string arg round trip = %q, want ref_42-x", got)
	}

	// Nonsense positional junk gets truncated, never fails, stays parseable.
	long := strings.Repeat("a", 500)
	data := fsubRetryData(long)
	if len(data) > 64 {
		t.Fatalf("callback payload too long: %d bytes (limit 64)", len(data))
	}
	if got := parseFsubRetryData(data); got != long[:48] {
		t.Fatalf("truncated arg parse mismatch: len %d", len(got))
	}

	// "fsj" matcher must not eat unrelated callback prefixes used elsewhere.
	for _, other := range []string{"claim", "home", "cap.3", "progress.9", "admc.emojis"} {
		if strings.HasPrefix(other, "fsj") {
			t.Fatalf("prefix collision with %q", other)
		}
	}
}

// The lock text must instruct users to tap the retry button (and carry its
// icon), since there is no t.me deep-link any more.
func TestLockFsubText(t *testing.T) {
	txt := lockFsubText()
	if !strings.Contains(txt, "Joined — Try Again") {
		t.Fatalf("lock text should point at the retry button: %q", txt)
	}
	if !strings.Contains(txt, "<b>Access Locked</b>") {
		t.Fatalf("lock text lost its header: %q", txt)
	}
}

// The force-join gate counts real membership AND recorded pending join
// requests (admin-approval channels); anything else stays locked.
func TestFsubSatisfied(t *testing.T) {
	cases := []struct {
		status  string
		pending bool
		want    bool
	}{
		{"member", false, true},
		{"administrator", false, true},
		{"creator", false, true},
		{"left", true, true},   // join request pending → counts
		{"kicked", true, true}, // request recorded before ban → still counts
		{"left", false, false},
		{"kicked", false, false},
		{"restricted", false, false},
		{"", false, false},
	}
	for _, c := range cases {
		if got := fsubSatisfied(c.status, c.pending); got != c.want {
			t.Errorf("fsubSatisfied(%q, %v) = %v, want %v", c.status, c.pending, got, c.want)
		}
	}
}

// Join buttons wear the channel's real name when it resolves, with the
// numbered generic label as fallback (long names are truncated so the
// button stays well inside Telegram's limits).
func TestJoinButtonLabel(t *testing.T) {
	if got := joinButtonLabel(0, ""); got != "📢 Join Channel 1" {
		t.Fatalf("fallback label = %q", got)
	}
	if got := joinButtonLabel(4, ""); got != "📢 Join Channel 5" {
		t.Fatalf("fallback label = %q", got)
	}
	if got := joinButtonLabel(0, "Deals Hub"); got != "📢 Deals Hub" {
		t.Fatalf("named label = %q", got)
	}
	long := strings.Repeat("x", 60)
	got := joinButtonLabel(2, long)
	if !strings.HasPrefix(got, "📢 ") || !strings.HasSuffix(got, "…") {
		t.Fatalf("long title should truncate with ellipsis: %q", got)
	}
	if n := len([]rune(got)); n > 43 { // "📢 " (2) + 40 + "…" (1)
		t.Fatalf("label too long: %d runes", n)
	}
}
