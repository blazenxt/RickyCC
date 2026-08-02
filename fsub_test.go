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
