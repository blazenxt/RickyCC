package main

import (
	"strings"
	"testing"
)

// The announcement must keep the agreed layout: header, exactly two
// divider rules, service = brand (gift cards only), batch size, total
// stock, and the uploader's name — HTML-escaped.
func TestStockNotifyText(t *testing.T) {
	text := stockNotifyText(12, 57, "A & B <admin>")

	if !strings.Contains(text, "✅ <b>Stock Updated!</b>") {
		t.Fatalf("header missing: %q", text)
	}
	if got := strings.Count(text, stockNotifyDivider); got != 2 {
		t.Fatalf("expected 2 dividers, got %d: %q", got, text)
	}
	for _, want := range []string{
		"🛒 Service: <b>" + esc(BrandName) + "</b>",
		"📦 Added: <b>12</b>",
		"📊 Total Stock: <b>57</b>",
		"👤 Uploaded by: <b>A &amp; B &lt;admin&gt;</b>",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %q", want, text)
		}
	}
	if strings.Contains(text, "\n\n\n") {
		t.Fatalf("triple blank line in layout: %q", text)
	}
}

// The callback deep-link must always carry the owner-specified referral id.
func TestStockOpenURL(t *testing.T) {
	if got, want := stockOpenURL("RickyBot"), "https://t.me/RickyBot?start=8726642457"; got != want {
		t.Fatalf("stockOpenURL = %q, want %q", got, want)
	}
}

// One green CTA button wired to the stockopen callback.
func TestStockNotifyKeyboard(t *testing.T) {
	kb := stockNotifyKeyboard()
	if len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 1 {
		t.Fatalf("expected a single-button keyboard: %+v", kb)
	}
	btn := kb.InlineKeyboard[0][0]
	if btn.CallbackData != "stockopen" {
		t.Fatalf("callback = %q, want stockopen", btn.CallbackData)
	}
	if btn.Style != "success" {
		t.Fatalf("style = %q, want success", btn.Style)
	}
	if btn.Url != "" {
		t.Fatalf("must stay a callback button (no URL), got %q", btn.Url)
	}
}

// Announcement targets = configured alert channels (+ force-join channels
// when the relay is on), deduped and sorted for stable output.
func TestStockNotifyTargets(t *testing.T) {
	cases := []struct {
		name     string
		announce []int64
		fsub     []int64
		relay    bool
		want     []int64
	}{
		{"none, relay off", nil, []int64{-1001}, false, []int64{}},
		{"none, relay on", nil, []int64{-1001, -1002}, true, []int64{-1002, -1001}},
		{"some, relay off", []int64{-1003}, []int64{-1001}, false, []int64{-1003}},
		{"overlap deduped", []int64{-1001, -1003}, []int64{-1001, -1002}, true, []int64{-1003, -1002, -1001}},
		{"empty everything", nil, nil, true, []int64{}},
	}
	for _, c := range cases {
		got := stockNotifyTargets(c.announce, c.fsub, c.relay)
		if len(got) != len(c.want) {
			t.Fatalf("%s: got %v, want %v", c.name, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%s: got %v, want %v", c.name, got, c.want)
			}
		}
	}
}

// Alert destinations + relay toggle persist across config reloads.
func TestAnnounceChannelsConfig(t *testing.T) {
	setupTestDB(t)
	loadConfig(0, nil)

	added, err := addAnnounceChannel(-100111)
	if err != nil || !added {
		t.Fatalf("add: added=%v err=%v", added, err)
	}
	if dup, _ := addAnnounceChannel(-100111); dup {
		t.Fatal("duplicate add should report not-added")
	}
	if _, err := addAnnounceChannel(-100222); err != nil {
		t.Fatalf("add2: %v", err)
	}
	if err := setAnnounceFsub(true); err != nil {
		t.Fatalf("setAnnounceFsub: %v", err)
	}

	loadConfig(0, nil) // simulate restart
	if got := getAnnounceChannels(); len(got) != 2 {
		t.Fatalf("channels after reload: %v", got)
	}
	if !getAnnounceFsub() {
		t.Fatal("relay state must survive a reload")
	}

	removed, err := removeAnnounceChannel(-100111)
	if err != nil || !removed {
		t.Fatalf("remove: removed=%v err=%v", removed, err)
	}
	if again, _ := removeAnnounceChannel(-100111); again {
		t.Fatal("removing twice should report not-removed")
	}
	if err := clearAnnounceChannels(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := getAnnounceChannels(); len(got) != 0 {
		t.Fatalf("after clear: %v", got)
	}
	if err := setAnnounceFsub(false); err != nil {
		t.Fatalf("relay off: %v", err)
	}
	loadConfig(0, nil)
	if getAnnounceFsub() {
		t.Fatal("relay-off must survive a reload")
	}
}
