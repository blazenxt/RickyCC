package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// User represents a bot user
type User struct {
	ID            int64
	Name          string
	Username      string
	Referrer      int64
	ReferredUsers []int64
	JoinedAt      time.Time
	Banned        bool
	HasClaimed    bool
	ClaimedCard   string
}

// Card statuses
const (
	CardAvailable = "available"
	CardClaimed   = "claimed"
)

// Card represents a reward card in the stock
type Card struct {
	ID        int64
	Card      string
	Status    string
	CreatedAt time.Time
	ClaimedBy int64
	ClaimedAt *time.Time
}

// errNoStock is returned by issueCard when the stock is empty.
var errNoStock = errors.New("no cards left in stock")

// errAlreadyClaimed is returned by issueCard when the user already holds a card.
var errAlreadyClaimed = errors.New("user already claimed a card")

var db *sql.DB

const schema = `
CREATE TABLE IF NOT EXISTS users (
    id             INTEGER PRIMARY KEY,
    name           TEXT    NOT NULL DEFAULT '',
    username       TEXT    NOT NULL DEFAULT '',
    referrer       INTEGER NOT NULL DEFAULT 0,
    referred_users TEXT    NOT NULL DEFAULT '[]',
    joined_at      INTEGER NOT NULL DEFAULT 0,
    banned         INTEGER NOT NULL DEFAULT 0,
    has_claimed    INTEGER NOT NULL DEFAULT 0,
    claimed_card   TEXT    NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS cards (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    code       TEXT    NOT NULL UNIQUE,
    status     TEXT    NOT NULL DEFAULT 'available',
    created_at INTEGER NOT NULL DEFAULT 0,
    claimed_by INTEGER NOT NULL DEFAULT 0,
    claimed_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS settings (
    id              INTEGER PRIMARY KEY CHECK (id = 1),
    log_chat_id     INTEGER NOT NULL DEFAULT 0,
    fsub_channels   TEXT    NOT NULL DEFAULT '[]',
    referral_target INTEGER NOT NULL DEFAULT 5,
    claims_paused   INTEGER NOT NULL DEFAULT 0,
    admin_ids       TEXT    NOT NULL DEFAULT '[]',
    support_url     TEXT    NOT NULL DEFAULT '',
    howto_text      TEXT    NOT NULL DEFAULT ''
);
`

// settingsMigrations adds newer settings columns to databases created by
// older builds. Errors (duplicate column) are expected and ignored.
var settingsMigrations = []string{
	`ALTER TABLE settings ADD COLUMN support_url TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE settings ADD COLUMN howto_text TEXT NOT NULL DEFAULT ''`,
}

// initDB opens (creating if needed) the local SQLite database and ensures the schema.
func initDB(path string) error {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"

	var err error
	db, err = sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	if err = db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %v", err)
	}
	// SQLite handles a single writer best
	db.SetMaxOpenConns(1)

	if _, err = db.Exec(schema); err != nil {
		return fmt.Errorf("failed to initialise schema: %v", err)
	}
	for _, m := range settingsMigrations {
		_, _ = db.Exec(m) // duplicate-column errors are expected, ignored
	}
	return nil
}

// ---------- row scanning helpers ----------

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(s scanner) (*User, error) {
	u := &User{}
	var refJSON string
	var joined int64
	var banned, hasClaimed int

	err := s.Scan(&u.ID, &u.Name, &u.Username, &u.Referrer, &refJSON,
		&joined, &banned, &hasClaimed, &u.ClaimedCard)
	if err != nil {
		return nil, err
	}

	if err = json.Unmarshal([]byte(refJSON), &u.ReferredUsers); err != nil {
		return nil, fmt.Errorf("failed to decode referred users for %d: %v", u.ID, err)
	}
	u.JoinedAt = time.Unix(joined, 0)
	u.Banned = banned != 0
	u.HasClaimed = hasClaimed != 0
	return u, nil
}

func scanCard(s scanner) (*Card, error) {
	c := &Card{}
	var created, claimedAt int64

	err := s.Scan(&c.ID, &c.Card, &c.Status, &created, &c.ClaimedBy, &claimedAt)
	if err != nil {
		return nil, err
	}
	c.CreatedAt = time.Unix(created, 0)
	if claimedAt > 0 {
		t := time.Unix(claimedAt, 0)
		c.ClaimedAt = &t
	}
	return c, nil
}

const userCols = "id, name, username, referrer, referred_users, joined_at, banned, has_claimed, claimed_card"
const cardCols = "id, code, status, created_at, claimed_by, claimed_at"

// ---------- Users ----------

func addUser(user User) error {
	var count int
	err := db.QueryRow("SELECT COUNT(1) FROM users WHERE id = ?", user.ID).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check user existence: %v", err)
	}
	if count > 0 {
		return fmt.Errorf("user with ID %d already exists", user.ID)
	}

	if user.JoinedAt.IsZero() {
		user.JoinedAt = time.Now()
	}

	_, err = db.Exec(
		"INSERT INTO users (id, name, username, referrer, joined_at) VALUES (?, ?, ?, ?, ?)",
		user.ID, user.Name, user.Username, user.Referrer, user.JoinedAt.Unix(),
	)
	if err != nil {
		return fmt.Errorf("failed to add user: %v", err)
	}
	return nil
}

// referUser registers newUser under referrerID and links them in the
// referrer's referred_users list.
func referUser(referrerID int64, newUser User) error {
	if _, err := getUser(referrerID); err != nil {
		return fmt.Errorf("referrer with ID %d does not exist", referrerID)
	}

	newUser.Referrer = referrerID
	if err := addUser(newUser); err != nil {
		return err
	}

	referrer, err := getUser(referrerID)
	if err != nil {
		return fmt.Errorf("failed to reload referrer: %v", err)
	}
	referrer.ReferredUsers = append(referrer.ReferredUsers, newUser.ID)

	data, _ := json.Marshal(referrer.ReferredUsers)
	_, err = db.Exec("UPDATE users SET referred_users = ? WHERE id = ?", string(data), referrerID)
	if err != nil {
		return fmt.Errorf("failed to update referrer's referred users: %v", err)
	}
	return nil
}

func getUser(userID int64) (*User, error) {
	return scanUser(db.QueryRow("SELECT "+userCols+" FROM users WHERE id = ?", userID))
}

// updateUserProfile refreshes the stored name/username (best effort).
func updateUserProfile(userID int64, name, username string) {
	_, err := db.Exec("UPDATE users SET name = ?, username = ? WHERE id = ?", name, username, userID)
	if err != nil {
		fmt.Printf("failed to update profile for user %d: %v\n", userID, err)
	}
}

func countClaimedUsers() (int64, error) {
	var n int64
	err := db.QueryRow("SELECT COUNT(1) FROM users WHERE has_claimed = 1").Scan(&n)
	return n, err
}

func countAllUsers() (int64, error) {
	var n int64
	err := db.QueryRow("SELECT COUNT(1) FROM users").Scan(&n)
	return n, err
}

func countBannedUsers() (int64, error) {
	var n int64
	err := db.QueryRow("SELECT COUNT(1) FROM users WHERE banned = 1").Scan(&n)
	return n, err
}

func getAllUsers() ([]User, error) {
	rows, err := db.Query("SELECT " + userCols + " FROM users")
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve users: %v", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

// getRecentUsers returns the newest users (by join date), capped at limit.
func getRecentUsers(limit int64) ([]User, error) {
	rows, err := db.Query("SELECT "+userCols+" FROM users ORDER BY joined_at DESC LIMIT ?", limit)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve recent users: %v", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

// ---------- Admin user actions ----------

func setUserBanned(userID int64, banned bool) error {
	flag := 0
	if banned {
		flag = 1
	}
	res, err := db.Exec("UPDATE users SET banned = ? WHERE id = ?", flag, userID)
	if err != nil {
		return fmt.Errorf("failed to update ban status for user %d: %v", userID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("user with ID %d does not exist", userID)
	}
	return nil
}

// resetUserClaim clears a user's claim so they can claim a new reward.
func resetUserClaim(userID int64) error {
	res, err := db.Exec("UPDATE users SET has_claimed = 0, claimed_card = '' WHERE id = ?", userID)
	if err != nil {
		return fmt.Errorf("failed to reset claim for user %d: %v", userID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("user with ID %d does not exist", userID)
	}
	return nil
}

// deleteUser removes a user row and unlinks them from their referrer.
func deleteUser(userID int64) error {
	u, err := getUser(userID)
	if err != nil {
		return fmt.Errorf("user with ID %d does not exist", userID)
	}

	if u.Referrer > 0 {
		if ref, rerr := getUser(u.Referrer); rerr == nil {
			next := make([]int64, 0, len(ref.ReferredUsers))
			for _, id := range ref.ReferredUsers {
				if id != userID {
					next = append(next, id)
				}
			}
			data, _ := json.Marshal(next)
			if _, uerr := db.Exec("UPDATE users SET referred_users = ? WHERE id = ?", string(data), u.Referrer); uerr != nil {
				fmt.Printf("failed to unlink user %d from referrer %d: %v\n", userID, u.Referrer, uerr)
			}
		}
	}

	res, err := db.Exec("DELETE FROM users WHERE id = ?", userID)
	if err != nil {
		return fmt.Errorf("failed to delete user %d: %v", userID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("user with ID %d does not exist", userID)
	}
	return nil
}

// ---------- Reward card stock ----------

// addCards inserts new cards into the stock. Returns (added, skipped, error)
// where skipped counts non-empty lines that were duplicates (in-batch or
// already in the DB). Blank lines are ignored entirely.
func addCards(lines []string) (int, int, error) {
	seen := map[string]bool{}
	nonEmpty := 0
	var fresh []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		nonEmpty++
		if seen[l] {
			continue
		}
		seen[l] = true
		fresh = append(fresh, l)
	}
	if len(fresh) == 0 {
		return 0, 0, fmt.Errorf("no valid cards provided")
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to start transaction: %v", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("INSERT OR IGNORE INTO cards (code, status, created_at) VALUES (?, ?, ?)")
	if err != nil {
		return 0, 0, fmt.Errorf("failed to prepare insert: %v", err)
	}
	defer stmt.Close()

	added := 0
	now := time.Now().Unix()
	for _, c := range fresh {
		res, err := stmt.Exec(c, CardAvailable, now)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to insert cards: %v", err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added++
		}
	}

	if err = tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("failed to commit cards: %v", err)
	}
	return added, nonEmpty - added, nil
}

func countAvailableCards() (int64, error) {
	var n int64
	err := db.QueryRow("SELECT COUNT(1) FROM cards WHERE status = ?", CardAvailable).Scan(&n)
	return n, err
}

func countClaimedCards() (int64, error) {
	var n int64
	err := db.QueryRow("SELECT COUNT(1) FROM cards WHERE status = ?", CardClaimed).Scan(&n)
	return n, err
}

// issueCard atomically grants exactly ONE card to a user.
//
// Hard guarantees, enforced by conditional writes inside a single transaction:
//   - the same card can never be issued twice (card-side gate),
//   - a user can never receive more than one card, even across concurrent
//     double-taps/devices (user-side gate),
//   - if the stock is empty the user's flag is rolled back, so nothing gets stuck.
//
// Returns errNoStock when the stock is empty and errAlreadyClaimed when the
// user already holds a card.
func issueCard(userID int64) (*Card, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %v", err)
	}
	defer tx.Rollback()

	// 1) Per-user gate: exactly one flow can flip has_claimed 0 -> 1.
	res, err := tx.Exec("UPDATE users SET has_claimed = 1 WHERE id = ? AND has_claimed = 0", userID)
	if err != nil {
		return nil, fmt.Errorf("failed to lock user claim: %v", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Either the user doesn't exist or already claimed — tell them apart.
		var claimed int
		serr := tx.QueryRow("SELECT has_claimed FROM users WHERE id = ?", userID).Scan(&claimed)
		if serr == sql.ErrNoRows {
			return nil, fmt.Errorf("user with ID %d does not exist", userID)
		}
		if serr != nil {
			return nil, fmt.Errorf("failed to check user: %v", serr)
		}
		return nil, errAlreadyClaimed
	}

	// 2) Oldest available card.
	var (
		cardID  int64
		code    string
		created int64
	)
	err = tx.QueryRow(
		"SELECT id, code, created_at FROM cards WHERE status = ? ORDER BY created_at LIMIT 1",
		CardAvailable,
	).Scan(&cardID, &code, &created)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errNoStock
		}
		return nil, fmt.Errorf("failed to fetch card: %v", err)
	}

	// 3) Per-card gate: exactly one flow can flip available -> claimed.
	now := time.Now().Unix()
	res, err = tx.Exec(
		"UPDATE cards SET status = ?, claimed_by = ?, claimed_at = ? WHERE id = ? AND status = ?",
		CardClaimed, userID, now, cardID, CardAvailable,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to claim card: %v", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, errNoStock // lost the race — behaves like empty stock for this attempt
	}

	// 4) Record the issued card on the user in the SAME transaction, so the
	//    card can never be "lost" between claiming and delivery.
	if _, err = tx.Exec("UPDATE users SET claimed_card = ? WHERE id = ?", code, userID); err != nil {
		return nil, fmt.Errorf("failed to record claimed card: %v", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit claim: %v", err)
	}

	t := time.Unix(now, 0)
	return &Card{
		ID:        cardID,
		Card:      code,
		Status:    CardClaimed,
		CreatedAt: time.Unix(created, 0),
		ClaimedBy: userID,
		ClaimedAt: &t,
	}, nil
}

// getRecentClaims returns the latest claimed cards (most recent first).
func getRecentClaims(limit int64) ([]Card, error) {
	rows, err := db.Query(
		"SELECT "+cardCols+" FROM cards WHERE status = ? ORDER BY claimed_at DESC LIMIT ?",
		CardClaimed, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve claims: %v", err)
	}
	defer rows.Close()

	var cards []Card
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		cards = append(cards, *c)
	}
	return cards, rows.Err()
}

// clearClaimedCards permanently deletes all claimed card records.
func clearClaimedCards() (int64, error) {
	res, err := db.Exec("DELETE FROM cards WHERE status = ?", CardClaimed)
	if err != nil {
		return 0, fmt.Errorf("failed to clear claimed cards: %v", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
