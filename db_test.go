package main

import (
	"errors"
	"path/filepath"
	"testing"
)

// setupTestDB opens a fresh throwaway SQLite database for each test.
func setupTestDB(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	if err := initDB(path); err != nil {
		t.Fatalf("initDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
}

func TestUserAndReferral(t *testing.T) {
	setupTestDB(t)

	if err := addUser(User{ID: 1, Name: "Ref"}); err != nil {
		t.Fatalf("addUser referrer: %v", err)
	}
	if err := referUser(1, User{ID: 2, Name: "Newbie"}); err != nil {
		t.Fatalf("referUser: %v", err)
	}

	ref, err := getUser(1)
	if err != nil {
		t.Fatalf("getUser: %v", err)
	}
	if len(ref.ReferredUsers) != 1 || ref.ReferredUsers[0] != 2 {
		t.Fatalf("expected referred_users [2], got %v", ref.ReferredUsers)
	}

	newbie, _ := getUser(2)
	if newbie.Referrer != 1 {
		t.Fatalf("expected referrer 1, got %d", newbie.Referrer)
	}
	if newbie.JoinedAt.IsZero() {
		t.Fatal("joined_at should be set automatically")
	}

	// Self-protection: duplicate add must fail
	if err := addUser(User{ID: 1}); err == nil {
		t.Fatal("expected duplicate addUser to fail")
	}
}

func TestCardClaimAtomicity(t *testing.T) {
	setupTestDB(t)

	added, skipped, err := addCards([]string{"CARD-1", "CARD-2", "CARD-1", "", "  "})
	if err != nil {
		t.Fatalf("addCards: %v", err)
	}
	if added != 2 || skipped != 1 {
		t.Fatalf("expected added=2 skipped=1, got added=%d skipped=%d", added, skipped)
	}

	// Duplicate stock import is fully skipped
	added, skipped, _ = addCards([]string{"CARD-1", "CARD-2"})
	if added != 0 || skipped != 2 {
		t.Fatalf("expected added=0 skipped=2, got added=%d skipped=%d", added, skipped)
	}

	card, err := claimCardAtomic(42)
	if err != nil {
		t.Fatalf("claimCardAtomic: %v", err)
	}
	if card.Card != "CARD-1" { // oldest first
		t.Fatalf("expected CARD-1, got %s", card.Card)
	}
	if card.ClaimedBy != 42 || card.ClaimedAt == nil {
		t.Fatalf("claim metadata wrong: %+v", card)
	}

	if avail, _ := countAvailableCards(); avail != 1 {
		t.Fatalf("expected 1 left, got %d", avail)
	}

	// Double-claim prevention: same card never handed out twice
	card2, _ := claimCardAtomic(43)
	if card2.Card != "CARD-2" {
		t.Fatalf("expected CARD-2, got %s", card2.Card)
	}

	if _, err := claimCardAtomic(44); !errors.Is(err, errNoStock) {
		t.Fatalf("expected errNoStock, got %v", err)
	}
}

func TestAdminUserActions(t *testing.T) {
	setupTestDB(t)

	_ = addUser(User{ID: 1, Name: "Boss"})
	_ = referUser(1, User{ID: 2, Name: "Friend"})

	if err := setUserBanned(2, true); err != nil {
		t.Fatalf("setUserBanned: %v", err)
	}
	u, _ := getUser(2)
	if !u.Banned {
		t.Fatal("expected user 2 to be banned")
	}

	if err := resetUserClaim(2); err != nil {
		t.Fatalf("resetUserClaim: %v", err)
	}

	if err := deleteUser(2); err != nil {
		t.Fatalf("deleteUser: %v", err)
	}
	if _, err := getUser(2); err == nil {
		t.Fatal("expected user 2 to be gone")
	}
	ref, _ := getUser(1)
	if len(ref.ReferredUsers) != 0 {
		t.Fatalf("deleteUser should unlink from referrer, got %v", ref.ReferredUsers)
	}
}

func TestSettings(t *testing.T) {
	setupTestDB(t)

	loadConfig(777, []int64{-1001, -1002})

	if getLogChat() != 777 {
		t.Fatalf("expected log chat 777, got %d", getLogChat())
	}
	if got := getFsubChannels(); len(got) != 2 || got[0] != -1001 || got[1] != -1002 {
		t.Fatalf("fsub seed mismatch: %v", got)
	}

	// Persist + reload from the same file (simulates restart)
	if err := setLogChat(555); err != nil {
		t.Fatalf("setLogChat: %v", err)
	}
	loadConfig(0, nil)
	if getLogChat() != 555 {
		t.Fatalf("log chat should persist across reloads, got %d", getLogChat())
	}

	// Channels add/remove
	if added, _ := addFsubChannel(-1001); added {
		t.Fatal("duplicate channel should not be added")
	}
	if added, _ := addFsubChannel(-1003); !added {
		t.Fatal("expected new channel to be added")
	}
	if removed, _ := removeFsubChannel(-1002); !removed {
		t.Fatal("expected channel to be removed")
	}
	if got := getFsubChannels(); len(got) != 2 || got[0] != -1001 || got[1] != -1003 {
		t.Fatalf("fsub mismatch after edits: %v", got)
	}

	// Admins
	OwnerID = 999
	if !isAdmin(999) || isAdmin(1) {
		t.Fatal("isAdmin owner logic broken")
	}
	if added, _ := addAdminID(111); !added {
		t.Fatal("expected admin add to succeed")
	}
	if !isAdmin(111) {
		t.Fatal("111 should be admin now")
	}
	if added, _ := addAdminID(111); added {
		t.Fatal("duplicate admin add should return false")
	}
	if removed, _ := removeAdminID(111); !removed || isAdmin(111) {
		t.Fatal("admin removal broken")
	}

	// Referral target + claims pause
	if err := setReferralTarget(10); err != nil || ReferralTarget != 10 {
		t.Fatal("referral target update broken")
	}
	if err := setClaimsPaused(true); err != nil || !ClaimsPaused {
		t.Fatal("claims pause broken")
	}

	// Support link + how-to text
	if err := setSupportURL("https://t.me/SupportExample"); err != nil {
		t.Fatalf("setSupportURL: %v", err)
	}
	if err := setHowtoText("Redeem at example.com — one use only."); err != nil {
		t.Fatalf("setHowtoText: %v", err)
	}

	loadConfig(0, nil)
	if ReferralTarget != 10 || !ClaimsPaused {
		t.Fatal("settings should persist across reloads")
	}
	if getSupportURL() != "https://t.me/SupportExample" {
		t.Fatalf("support URL should persist, got %q", getSupportURL())
	}
	if getHowtoText() != "Redeem at example.com — one use only." {
		t.Fatalf("how-to text should persist, got %q", getHowtoText())
	}

	// Clearing restores defaults
	if err := setHowtoText(""); err != nil {
		t.Fatalf("clear howto: %v", err)
	}
	if getHowtoText() != defaultHowto {
		t.Fatal("clearing how-to should restore the default text")
	}
}
