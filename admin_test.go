package main

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// Recent Users rows: one tap-to-manage button per user with stable callback
// data, a banned marker, a name fallback, and bounded length.
func TestAdmRecentUserButtons(t *testing.T) {
	users := []User{
		{ID: 11, Name: "Aarav"},
		{ID: 22, Name: "  "},                                    // blank → fallback
		{ID: 33, Name: "BannedBandit", Banned: true},            // 🚫 marker
		{ID: 44, Name: "AVeryLongDisplayNameThatMustBeTrimmed"}, // truncated
	}

	rows := admRecentUserButtons(users)
	if len(rows) != len(users) {
		t.Fatalf("expected %d rows, got %d", len(users), len(rows))
	}
	for i, row := range rows {
		if len(row) != 1 {
			t.Fatalf("row %d should have exactly one button", i)
		}
		want := "admu.view."
		if !strings.HasPrefix(row[0].CallbackData, want) {
			t.Fatalf("row %d callback %q lacks %q prefix", i, row[0].CallbackData, want)
		}
		if id := strings.TrimPrefix(row[0].CallbackData, want); id != strconv.FormatInt(users[i].ID, 10) {
			t.Fatalf("row %d callback id %q, want %q", i, id, users[i].ID)
		}
	}

	if !strings.HasPrefix(rows[0][0].Text, "👤 Aarav") {
		t.Fatalf("normal row: %q", rows[0][0].Text)
	}
	if rows[1][0].Text != "👤 User 22" {
		t.Fatalf("blank name fallback: %q", rows[1][0].Text)
	}
	if !strings.HasPrefix(rows[2][0].Text, "🚫 BannedBandit") {
		t.Fatalf("banned marker: %q", rows[2][0].Text)
	}
	// "👤 " prefix + ≤18-rune name → never a giant button label
	if got := len([]rune(rows[3][0].Text)); got > 21 {
		t.Fatalf("long name not trimmed: %d runes %q", got, rows[3][0].Text)
	}
	if len(admRecentUserButtons(nil)) != 0 {
		t.Fatal("nil users should produce no rows")
	}
}

// User manage card: ban/unban flip by state, reset+delete always wired,
// and the card body shows the essentials.
func TestAdminUserCardViewButtons(t *testing.T) {
	u := &User{
		ID:            5,
		Name:          "Test User",
		Username:      "tester",
		ReferredUsers: []int64{1, 2, 3, 4, 5},
		Claims:        1,
		JoinedAt:      time.Now(),
	}

	text, kb := adminUserCardView(u)
	if !strings.Contains(text, "<code>5</code>") || !strings.Contains(text, "Test User") || !strings.Contains(text, "@tester") {
		t.Fatalf("card text missing identity bits: %q", text)
	}
	if !strings.Contains(text, "Referrals: <b>5</b>") {
		t.Fatalf("card text missing referral count: %q", text)
	}

	var callbacks []string
	for _, row := range kb.InlineKeyboard {
		for _, b := range row {
			callbacks = append(callbacks, b.CallbackData)
		}
	}
	joinedCB := strings.Join(callbacks, " ")
	for _, want := range []string{"admu.ban.5", "admu.reset.5", "admu.del.5", "admp.users"} {
		if !strings.Contains(joinedCB, want) {
			t.Fatalf("card keyboard missing %q: %s", want, joinedCB)
		}
	}
	if strings.Contains(joinedCB, "admu.unban.5") || strings.Contains(joinedCB, "admu.view.5") {
		t.Fatalf("active user card must not show unban/view: %s", joinedCB)
	}

	// Banned user: the same button flips to Unban.
	u.Banned = true
	_, kbB := adminUserCardView(u)
	var cbB []string
	for _, row := range kbB.InlineKeyboard {
		for _, b := range row {
			cbB = append(cbB, b.CallbackData)
		}
	}
	joinedB := strings.Join(cbB, " ")
	if !strings.Contains(joinedB, "admu.unban.5") || strings.Contains(joinedB, "admu.ban.5") {
		t.Fatalf("banned card should show unban only: %s", joinedB)
	}
}

// The style engine keeps giving user-management actions the right roles
// (view = primary, unban = success, ban/reset/del = danger) — pinned here
// so the manage card never loses its colors.
func TestUserMgmtStyles(t *testing.T) {
	for data, want := range map[string]string{
		"admu.view.9":   "primary",
		"admu.unban.9":  "success",
		"admu.ban.9":    "danger",
		"admu.reset.9":  "danger",
		"admu.del.9":    "danger",
		"admu.delok.9":  "danger",
		"admc.finduser": "primary",
	} {
		if got := buttonStyle(gotgbot.InlineKeyboardButton{Text: "x", CallbackData: data}); got != want {
			t.Fatalf("%s: style=%q, want %q", data, got, want)
		}
	}
}
