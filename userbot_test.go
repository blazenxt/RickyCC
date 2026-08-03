package main

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/tg"
)

// ── phone sanity check ──────────────────────────────────────────────────────

func TestLooksLikePhone(t *testing.T) {
	valid := []string{"+919876543210", "919876543210", "+14155552671", "+4433333333333"}
	for _, s := range valid {
		if !looksLikePhone(s) {
			t.Fatalf("should accept %q", s)
		}
	}
	invalid := []string{"", "+", "12345", "+12345678901234567890", "+91abc543210", "98765 43210", "+91-9876543210"}
	for _, s := range invalid {
		if looksLikePhone(s) {
			t.Fatalf("should reject %q", s)
		}
	}
}

// ── premiumEntities: UTF-16 offset math ─────────────────────────────────────

// ent flattens the entity list to {offset, length, docID} for easy compare.
type ent struct {
	off int
	l   int
	doc int64
}

func flatten(entities []tg.MessageEntityClass) []ent {
	var out []ent
	for _, e := range entities {
		ce, ok := e.(*tg.MessageEntityCustomEmoji)
		if !ok {
			continue
		}
		out = append(out, ent{ce.Offset, ce.Length, ce.DocumentID})
	}
	return out
}

func withCustomIcons(t *testing.T, icons map[string]string) {
	t.Helper()
	prev := customIcons
	customIcons = icons
	t.Cleanup(func() { customIcons = prev })
}

func TestPremiumEntitiesOffsets(t *testing.T) {
	const (
		okID   = int64(6206185428702206246)
		bellID = int64(6206508629286196237)
		num1ID = int64(6206328647414212014)
	)
	withCustomIcons(t, map[string]string{
		"ok":   "6206185428702206246", // ✅ = 1 UTF-16 unit (BMP)
		"bell": "6206508629286196237", // 🔔 = 2 units (astral)
		"num1": "6206328647414212014", // 1️⃣ = 3 units (digit+VS16+keycap)
	})

	cases := []struct {
		name string
		text string
		want []ent
	}{
		{"empty", "", nil},
		{"no emoji", "hello world", nil},
		{"unmapped glyph only", "🔥 fire", nil},
		{"solo", "✅", []ent{{0, 1, okID}}},
		{"mid text", "ok ✅ done", []ent{{3, 1, okID}}},
		{"two emojis adjacent", "✅🔔", []ent{{0, 1, okID}, {1, 2, bellID}}},
		{"astral before emoji", "😀✅", []ent{{2, 1, okID}}}, // 😀 = 2 units
		{"keycap length", "1️⃣ x", []ent{{0, 3, num1ID}}},
		{"astral + keycap", "a🔔1️⃣z✅", []ent{{1, 2, bellID}, {3, 3, num1ID}, {7, 1, okID}}},
		{"repeated", "✅ ✅", []ent{{0, 1, okID}, {2, 1, okID}}},
	}
	for _, tc := range cases {
		got := flatten(premiumEntities(tc.text))
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: premiumEntities(%q) = %#v, want %#v", tc.name, tc.text, got, tc.want)
		}
	}
}

func TestPremiumEntitiesNoMapping(t *testing.T) {
	withCustomIcons(t, map[string]string{})
	if got := premiumEntities("✅ stock"); got != nil {
		t.Fatalf("empty registry should give nil, got %#v", got)
	}

	// malformed custom-emoji IDs are skipped, not trusted
	withCustomIcons(t, map[string]string{
		"ok":   "abc",   // not numeric
		"bell": "123",   // too short
		"mega": "6206x", // junk
	})
	if got := premiumEntities("✅ 🔔 📢"); got != nil {
		t.Fatalf("all-invalid registry should give nil, got %#v", got)
	}
}

func TestUTF16Units(t *testing.T) {
	cases := map[string]int{
		"":    0,
		"abc": 3,
		"✅":   1, // BMP
		"⚠️":  2, // BMP + VS16
		"🔔":   2, // astral → surrogate pair
		"😀":   2, // astral
		"1️⃣": 3, // digit + VS16 + combining keycap
		"a🔔b": 4,
	}
	for s, want := range cases {
		if got := utf16Units(s); got != want {
			t.Fatalf("utf16Units(%q) = %d, want %d", s, got, want)
		}
	}
}

// ── session storage round-trip ──────────────────────────────────────────────

func TestDBSessionStorageRoundTrip(t *testing.T) {
	setupTestDB(t)
	loadConfig(0, nil)

	ctx := context.Background()
	st := dbSessionStorage{}

	if _, err := st.LoadSession(ctx); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("fresh DB: LoadSession err = %v, want session.ErrNotFound", err)
	}
	if hasUserbotSession() {
		t.Fatal("fresh DB should report no userbot session")
	}

	payload := []byte("fake-auth-key-bytes-\x00\x01\x02")
	if err := st.StoreSession(ctx, payload); err != nil {
		t.Fatalf("StoreSession: %v", err)
	}
	got, err := st.LoadSession(ctx)
	if err != nil {
		t.Fatalf("LoadSession after store: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("round-trip mismatch: got %v want %v", got, payload)
	}
	if !hasUserbotSession() {
		t.Fatal("stored session should report present")
	}

	clearUserbotSession()
	if hasUserbotSession() {
		t.Fatal("cleared session should report absent")
	}
	if _, err := st.LoadSession(ctx); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("after clear: LoadSession err = %v, want session.ErrNotFound", err)
	}
}

// ── editor targets = announcement destinations ──────────────────────────────

func TestEditorTargetsUnion(t *testing.T) {
	setupTestDB(t)
	loadConfig(0, nil)

	if _, err := addAnnounceChannel(-1001); err != nil {
		t.Fatalf("addAnnounceChannel -1001: %v", err)
	}
	if _, err := addAnnounceChannel(-1002); err != nil {
		t.Fatalf("addAnnounceChannel -1002: %v", err)
	}
	if err := saveFsubChannels([]int64{-1002, -1003}); err != nil {
		t.Fatalf("saveFsubChannels: %v", err)
	}

	if err := setAnnounceFsub(false); err != nil {
		t.Fatalf("setAnnounceFsub(false): %v", err)
	}
	got := editorTargets()
	want := map[int64]bool{-1001: true, -1002: true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("relay off: targets = %v, want %v", got, want)
	}

	if err := setAnnounceFsub(true); err != nil {
		t.Fatalf("setAnnounceFsub(true): %v", err)
	}
	got = editorTargets()
	want = map[int64]bool{-1001: true, -1002: true, -1003: true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("relay on: targets = %v, want %v", got, want)
	}
}

// ── own-post detection (the edit filter) ────────────────────────────────────

func TestWasPostedByBot(t *testing.T) {
	m := &ubManager{recentPosts: map[int64]map[int]time.Time{}, edited: map[string]bool{}}
	m.setBotID(999)

	inChannel := &tg.Message{ID: 7, PeerID: &tg.PeerChannel{ChannelID: 42}}
	if m.wasPostedByBot(inChannel) {
		t.Fatal("unknown post must not be claimed")
	}

	// announcer tracked the post → recognised
	m.notePosted(42, 7)
	if !m.wasPostedByBot(inChannel) {
		t.Fatal("tracked post must be recognised")
	}
	other := &tg.Message{ID: 8, PeerID: &tg.PeerChannel{ChannelID: 42}}
	if m.wasPostedByBot(other) {
		t.Fatal("untracked message ID must be rejected")
	}

	// fallback: the update itself names the bot as sender
	fromBot := &tg.Message{ID: 9, PeerID: &tg.PeerChannel{ChannelID: 43}, FromID: &tg.PeerUser{UserID: 999}}
	if !m.wasPostedByBot(fromBot) {
		t.Fatal("message from bot account must be recognised")
	}
	fromOther := &tg.Message{ID: 10, PeerID: &tg.PeerChannel{ChannelID: 43}, FromID: &tg.PeerUser{UserID: 111}}
	if m.wasPostedByBot(fromOther) {
		t.Fatal("message from another user must be rejected")
	}

	// non-channel peers never match
	dmStyle := &tg.Message{ID: 11, PeerID: &tg.PeerUser{UserID: 5}, FromID: &tg.PeerUser{UserID: 999}}
	if m.wasPostedByBot(dmStyle) {
		t.Fatal("non-channel peer must be rejected")
	}
}

func TestNotePostedPrunes(t *testing.T) {
	m := &ubManager{recentPosts: map[int64]map[int]time.Time{}, edited: map[string]bool{}}
	for i := 1; i <= 600; i++ {
		m.notePosted(50, i)
	}
	m.recentMu.Lock()
	n := len(m.recentPosts[50])
	m.recentMu.Unlock()
	if n > 500 {
		t.Fatalf("recentPosts should stay bounded, got %d", n)
	}
	// newest is always kept
	if !m.wasPostedByBot(&tg.Message{ID: 600, PeerID: &tg.PeerChannel{ChannelID: 50}}) {
		t.Fatal("newest tracked post must survive pruning")
	}
}
