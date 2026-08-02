package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// setupTestDB opens a fresh throwaway SQLite database for each test.
// It always uses the embedded engine directly — a DATABASE_URL leaking in
// from the environment must never reroute tests to a live server.
func setupTestDB(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	if err := initSQLite(path); err != nil {
		t.Fatalf("initDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
}

// rebind must translate ? placeholders to $N only in Postgres mode.
func TestRebind(t *testing.T) {
	defer func() { dialect = dialectSQLite }()

	dialect = dialectPostgres
	q := "UPDATE users SET claims = claims + 1, has_claimed = 1 WHERE id = ? AND claims = ? LIMIT ?"
	want := "UPDATE users SET claims = claims + 1, has_claimed = 1 WHERE id = $1 AND claims = $2 LIMIT $3"
	if got := rebind(q); got != want {
		t.Fatalf("postgres rebind:\n got %q\nwant %q", got, want)
	}
	if got := rebind("SELECT 1"); got != "SELECT 1" {
		t.Fatalf("rebind without placeholders changed the query: %q", got)
	}

	dialect = dialectSQLite
	if got := rebind("DELETE FROM users WHERE id = ?"); got != "DELETE FROM users WHERE id = ?" {
		t.Fatalf("sqlite queries must pass through untouched: %q", got)
	}

	// Postgres schema must not carry SQLite-only syntax.
	if strings.Contains(schemaPostgres, "AUTOINCREMENT") {
		t.Fatal("postgres schema contains AUTOINCREMENT")
	}
	if !strings.Contains(schemaPostgres, "BIGSERIAL PRIMARY KEY") {
		t.Fatal("postgres cards table lost its autoincrement id")
	}
}

// fakeReferredID hands out unique IDs for seeded (fake) referred users.
var fakeReferredID int64 = 900000

// seedReferrals registers n referred users under referrerID.
func seedReferrals(t *testing.T, referrerID int64, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		fakeReferredID++
		if err := referUser(referrerID, User{ID: fakeReferredID, Name: "F"}); err != nil {
			t.Fatalf("seedReferrals(%d, %d): %v", referrerID, n, err)
		}
	}
}

func addStock(t *testing.T, codes ...string) {
	t.Helper()
	if _, _, err := addCards(codes); err != nil {
		t.Fatalf("addCards: %v", err)
	}
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

func TestUnlockMath(t *testing.T) {
	cases := []struct {
		refs, claims, target, want int
	}{
		{0, 0, 5, 0},
		{4, 0, 5, 0},
		{5, 0, 5, 1},   // 5 referrals = 1 card
		{9, 0, 5, 1},   // still 1
		{10, 0, 5, 2},  // 10 = 2
		{25, 0, 5, 5},  // 5n = 5 cards (the user's exact example)
		{25, 5, 5, 0},  // all collected
		{25, 2, 5, 3},  // 3 still claimable
		{10, 20, 5, 0}, // never negative
		{10, 0, 0, 0},  // disabled target = nothing earned
	}
	for _, c := range cases {
		if got := unlocksAvailable(c.refs, c.claims, c.target); got != c.want {
			t.Fatalf("unlocksAvailable(%d, %d, %d) = %d, want %d", c.refs, c.claims, c.target, got, c.want)
		}
	}

	if got := nextRewardIn(0, 5); got != 5 {
		t.Fatalf("nextRewardIn(0, 5) = %d, want 5", got)
	}
	if got := nextRewardIn(5, 5); got != 5 {
		t.Fatalf("nextRewardIn(5, 5) = %d, want 5 (next batch)", got)
	}
	if got := nextRewardIn(7, 5); got != 3 {
		t.Fatalf("nextRewardIn(7, 5) = %d, want 3", got)
	}
	if got := nextRewardIn(7, 0); got != 0 {
		t.Fatalf("nextRewardIn(7, 0) = %d, want 0", got)
	}
}

func TestRepeatIssuePerNReferrals(t *testing.T) {
	setupTestDB(t)
	ReferralTarget = 5

	addStock(t, "CARD-1", "CARD-2", "CARD-3", "CARD-4")

	// Unregistered users can't be issued cards
	if _, err := issueCard(99, ReferralTarget); err == nil || errors.Is(err, errNoUnlocks) || errors.Is(err, errNoStock) {
		t.Fatalf("expected user-not-found error, got %v", err)
	}

	for _, id := range []int64{42, 43, 44} {
		if err := addUser(User{ID: id, Name: "U"}); err != nil {
			t.Fatalf("addUser %d: %v", id, err)
		}
	}

	// Zero referrals = zero unlocks
	if _, err := issueCard(42, ReferralTarget); !errors.Is(err, errNoUnlocks) {
		t.Fatalf("expected errNoUnlocks without referrals, got %v", err)
	}

	// 5 referrals -> 1 unlock -> CARD-1
	seedReferrals(t, 42, 5)
	card, err := issueCard(42, ReferralTarget)
	if err != nil {
		t.Fatalf("issueCard: %v", err)
	}
	if card.Card != "CARD-1" || card.ClaimedBy != 42 || card.ClaimedAt == nil {
		t.Fatalf("issue metadata wrong: %+v", card)
	}
	u, _ := getUser(42)
	if u.Claims != 1 || !u.HasClaimed || u.ClaimedCard != "CARD-1" {
		t.Fatalf("user 42 should hold CARD-1 with claims=1, got %+v", u)
	}

	// Second attempt with no fresh unlocks is rejected
	if _, err = issueCard(42, ReferralTarget); !errors.Is(err, errNoUnlocks) {
		t.Fatalf("expected errNoUnlocks on second claim, got %v", err)
	}

	// 10 referrals total -> another unlock -> a DIFFERENT card (CARD-2)
	seedReferrals(t, 42, 5)
	card2, err := issueCard(42, ReferralTarget)
	if err != nil || card2.Card != "CARD-2" {
		t.Fatalf("expected CARD-2 for 10 referrals, got %v / %+v", err, card2)
	}
	if card2.Card == card.Card {
		t.Fatal("repeat reward must be a different physical card code")
	}
	if u, _ = getUser(42); u.Claims != 2 {
		t.Fatalf("user 42 claims should be 2, got %d", u.Claims)
	}

	// History is queryable per user
	own, err := getUserCards(42, 10)
	if err != nil || len(own) != 2 || own[0].Card != "CARD-2" || own[1].Card != "CARD-1" {
		t.Fatalf("getUserCards wrong: %v %+v", err, own)
	}

	// Not enough referrals -> nothing
	seedReferrals(t, 43, 3)
	if _, err := issueCard(43, ReferralTarget); !errors.Is(err, errNoUnlocks) {
		t.Fatalf("expected errNoUnlocks for 3/5 referrals, got %v", err)
	}

	// Saved unlocks can be burned one by one (10 refs = 2 unlocks)
	seedReferrals(t, 44, 10)
	c1, err := issueCard(44, ReferralTarget)
	if err != nil {
		t.Fatalf("first claim of 2 unlocks: %v", err)
	}
	c2, err := issueCard(44, ReferralTarget)
	if err != nil {
		t.Fatalf("second claim of 2 unlocks: %v", err)
	}
	if c1.Card == c2.Card {
		t.Fatal("back-to-back claims must yield distinct cards")
	}
	if _, err = issueCard(44, ReferralTarget); !errors.Is(err, errNoUnlocks) {
		t.Fatalf("expected errNoUnlocks after burning both unlocks, got %v", err)
	}

	// OUT OF STOCK → unlock must NOT be spent (claim works after restock)
	seedReferrals(t, 43, 7) // 10 refs total = 2 unlocks; stock is empty now
	if _, err = issueCard(43, ReferralTarget); !errors.Is(err, errNoStock) {
		t.Fatalf("expected errNoStock, got %v", err)
	}
	if u, _ := getUser(43); u.Claims != 0 || u.HasClaimed {
		t.Fatal("out-of-stock must not spend unlocks or flag the user")
	}
	addStock(t, "CARD-5", "CARD-6")
	if _, err = issueCard(43, ReferralTarget); err != nil {
		t.Fatalf("restock claim 1: %v", err)
	}
	if _, err = issueCard(43, ReferralTarget); err != nil {
		t.Fatalf("restock claim 2: %v", err)
	}
	if u, _ := getUser(43); u.Claims != 2 {
		t.Fatalf("user 43 claims should be 2 after restock, got %d", u.Claims)
	}

	// Admin reset re-enables already-earned unlocks
	if err := resetUserClaim(43); err != nil {
		t.Fatalf("resetUserClaim: %v", err)
	}
	if u, _ := getUser(43); u.Claims != 0 || u.HasClaimed {
		t.Fatal("reset should clear claims and the mirror flag")
	}
	addStock(t, "CARD-7")
	if _, err = issueCard(43, ReferralTarget); err != nil {
		t.Fatalf("claim after reset: %v", err)
	}
}

func TestIssueConcurrency(t *testing.T) {
	setupTestDB(t)
	ReferralTarget = 5

	// 20 users with one unlock each, only 5 cards — exact guarantees must hold:
	// every success is a different card, nobody exceeds their earned unlocks.
	const users, stock = 20, 5
	var codes []string
	for i := 0; i < stock; i++ {
		codes = append(codes, fmt.Sprintf("CARD-%d", i))
	}
	addStock(t, codes...)
	for i := 0; i < users; i++ {
		id := int64(1000 + i)
		if err := addUser(User{ID: id, Name: "U"}); err != nil {
			t.Fatalf("addUser: %v", err)
		}
		seedReferrals(t, id, 5)
	}

	fire := func() []string {
		results := make(chan string, users)
		var wg sync.WaitGroup
		for i := 0; i < users; i++ {
			wg.Add(1)
			go func(id int64) {
				defer wg.Done()
				if c, err := issueCard(id, ReferralTarget); err == nil {
					results <- c.Card
				} else {
					results <- ""
				}
			}(int64(1000 + i))
		}
		wg.Wait()
		close(results)

		got := []string{}
		for r := range results {
			if r != "" {
				got = append(got, r)
			}
		}
		return got
	}
	assertDistinct := func(cards []string, label string) {
		t.Helper()
		seen := map[string]bool{}
		for _, c := range cards {
			if seen[c] {
				t.Fatalf("%s: card %s issued twice — delivery guarantee broken!", label, c)
			}
			seen[c] = true
		}
	}

	// Wave 1: exactly `stock` successes, each card exactly once.
	wave1 := fire()
	assertDistinct(wave1, "wave1")
	if len(wave1) != stock {
		t.Fatalf("wave1: expected %d successes, got %d", stock, len(wave1))
	}
	if avail, _ := countAvailableCards(); avail != 0 {
		t.Fatalf("wave1: stock should be empty, got %d", avail)
	}
	claims, _ := countClaimedCards()
	claimers, _ := countClaimedUsers()
	if claims != int64(stock) || claimers != int64(stock) {
		t.Fatalf("wave1: claimed counts wrong: cards=%d users=%d", claims, claimers)
	}

	// Wave 2: restock 5 → the unlocks of wave-1 losers are STILL valid,
	// so exactly 5 of them now collect their saved reward.
	addStock(t, "EXTRA-1", "EXTRA-2", "EXTRA-3", "EXTRA-4", "EXTRA-5")
	wave2 := fire()
	assertDistinct(wave2, "wave2")
	if len(wave2) != 5 {
		t.Fatalf("wave2: expected 5 successes (saved unlocks), got %d", len(wave2))
	}

	// Wave 3: restock 10 → remaining 10 users collect too.
	var rest []string
	for i := 0; i < 10; i++ {
		rest = append(rest, fmt.Sprintf("FINAL-%d", i))
	}
	addStock(t, rest...)
	wave3 := fire()
	assertDistinct(wave3, "wave3")
	if len(wave3) != 10 {
		t.Fatalf("wave3: expected 10 successes, got %d", len(wave3))
	}

	// Now every user holds exactly ONE card for exactly ONE earned unlock.
	for i := 0; i < users; i++ {
		u, _ := getUser(int64(1000 + i))
		if u.Claims != 1 {
			t.Fatalf("user %d claims=%d, want exactly 1 (one unlock each)", u.ID, u.Claims)
		}
	}

	// Wave 4: restock again, but nobody has a fresh unlock — ZERO issues.
	addStock(t, "LOCKED-1", "LOCKED-2")
	if wave4 := fire(); len(wave4) != 0 {
		t.Fatalf("wave4: no unlocks left but cards got issued: %v", wave4)
	}

	// Wave 5: everyone earns a SECOND unlock (10 refs total) → all 20 can
	// collect a second, different card.
	for i := 0; i < users; i++ {
		seedReferrals(t, int64(1000+i), 5)
	}
	var extra []string
	for i := 0; i < users; i++ {
		extra = append(extra, fmt.Sprintf("SECOND-%d", i))
	}
	addStock(t, extra...)
	wave5 := fire()
	assertDistinct(wave5, "wave5")
	if len(wave5) != users {
		t.Fatalf("wave5: expected %d successes, got %d", users, len(wave5))
	}
	for i := 0; i < users; i++ {
		u, _ := getUser(int64(1000 + i))
		if u.Claims != 2 {
			t.Fatalf("user %d claims=%d, want 2 after second unlock", u.ID, u.Claims)
		}
		own, _ := getUserCards(u.ID, 5)
		if len(own) != 2 || own[0].Card == own[1].Card {
			t.Fatalf("user %d history wrong: %+v", u.ID, own)
		}
	}
}

func TestAdminUserActions(t *testing.T) {
	setupTestDB(t)
	ReferralTarget = 5

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

// Pending join requests (admin-approval force-join channels) must round
// trip: save → visible, re-save → no error/duplicate, delete → gone, and
// rows must stay scoped per (channel, user) pair.
func TestJoinRequests(t *testing.T) {
	setupTestDB(t)

	if hasJoinRequest(-1001, 42) {
		t.Fatal("unexpected join request before saving")
	}
	if err := saveJoinRequest(-1001, 42); err != nil {
		t.Fatalf("saveJoinRequest: %v", err)
	}
	if !hasJoinRequest(-1001, 42) {
		t.Fatal("join request missing after saving")
	}

	// re-saving the same pair must behave as an upsert, not an error
	if err := saveJoinRequest(-1001, 42); err != nil {
		t.Fatalf("saveJoinRequest duplicate: %v", err)
	}

	if hasJoinRequest(-1001, 43) || hasJoinRequest(-1002, 42) {
		t.Fatal("join request leaked across users/channels")
	}

	deleteJoinRequest(-1001, 42)
	if hasJoinRequest(-1001, 42) {
		t.Fatal("join request still present after delete")
	}
}
