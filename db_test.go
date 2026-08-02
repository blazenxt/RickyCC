package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
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

func TestCardDedupImport(t *testing.T) {
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
}

func TestIssueOnceGuarantee(t *testing.T) {
	setupTestDB(t)

	if _, _, err := addCards([]string{"CARD-1", "CARD-2"}); err != nil {
		t.Fatalf("addCards: %v", err)
	}

	// Unregistered users can't be issued cards
	if _, err := issueCard(99); err == nil || errors.Is(err, errAlreadyClaimed) {
		t.Fatalf("expected user-not-found error, got %v", err)
	}

	for _, id := range []int64{42, 43, 44} {
		if err := addUser(User{ID: id, Name: "U"}); err != nil {
			t.Fatalf("addUser %d: %v", id, err)
		}
	}

	// First issue: oldest card first, recorded on the user
	card, err := issueCard(42)
	if err != nil {
		t.Fatalf("issueCard: %v", err)
	}
	if card.Card != "CARD-1" || card.ClaimedBy != 42 || card.ClaimedAt == nil {
		t.Fatalf("issue metadata wrong: %+v", card)
	}
	u, _ := getUser(42)
	if !u.HasClaimed || u.ClaimedCard != "CARD-1" {
		t.Fatalf("user 42 should hold CARD-1, got %+v", u)
	}

	// ONE CARD PER USER: second issue for same user is rejected
	if _, err = issueCard(42); !errors.Is(err, errAlreadyClaimed) {
		t.Fatalf("expected errAlreadyClaimed, got %v", err)
	}

	// Other users still get their own (different) card
	card2, err := issueCard(43)
	if err != nil || card2.Card != "CARD-2" {
		t.Fatalf("expected CARD-2 for user 43, got %v / %+v", err, card2)
	}
	if avail, _ := countAvailableCards(); avail != 0 {
		t.Fatalf("stock should be empty, got %d", avail)
	}

	// OUT OF STOCK → flag must roll back so the user can claim after restock
	if _, err = issueCard(44); !errors.Is(err, errNoStock) {
		t.Fatalf("expected errNoStock, got %v", err)
	}
	u, _ = getUser(44)
	if u.HasClaimed {
		t.Fatal("out-of-stock must not leave user flagged as claimed")
	}

	// Restock → same user can now claim
	if _, _, err := addCards([]string{"CARD-3"}); err != nil {
		t.Fatalf("addCards: %v", err)
	}
	card3, err := issueCard(44)
	if err != nil || card3.Card != "CARD-3" {
		t.Fatalf("restock claim failed: %v / %+v", err, card3)
	}
}

func TestIssueConcurrency(t *testing.T) {
	setupTestDB(t)

	// 20 users, 5 cards — every user must get at most one card,
	// and no card may ever land with two users. Ever.
	const users, stock = 20, 5
	var codes []string
	for i := 0; i < stock; i++ {
		codes = append(codes, fmt.Sprintf("CARD-%d", i))
	}
	if _, _, err := addCards(codes); err != nil {
		t.Fatalf("addCards: %v", err)
	}
	for i := 0; i < users; i++ {
		if err := addUser(User{ID: int64(1000 + i), Name: "U"}); err != nil {
			t.Fatalf("addUser: %v", err)
		}
	}

	type result struct{ card, errText string }
	results := make(chan result, users)
	var wg sync.WaitGroup
	for i := 0; i < users; i++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			c, err := issueCard(id)
			res := result{}
			if err == nil {
				res.card = c.Card
			} else {
				res.errText = err.Error()
			}
			results <- res
		}(int64(1000 + i))
	}
	wg.Wait()
	close(results)

	issued := map[string]int{}
	succeeded := 0
	for r := range results {
		if r.card != "" {
			succeeded++
			issued[r.card]++
		}
	}
	if succeeded != stock {
		t.Fatalf("expected exactly %d successful issues, got %d", stock, succeeded)
	}
	for code, n := range issued {
		if n != 1 {
			t.Fatalf("card %s issued %d times — delivery guarantee broken!", code, n)
		}
	}

	// No card left, everyone else must have been told "no stock"
	if avail, _ := countAvailableCards(); avail != 0 {
		t.Fatalf("stock should be empty, got %d", avail)
	}
	claims, _ := countClaimedCards()
	claimers, _ := countClaimedUsers()
	if claims != int64(stock) || claimers != int64(stock) {
		t.Fatalf("claimed counts wrong: cards=%d users=%d", claims, claimers)
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
