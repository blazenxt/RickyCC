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
