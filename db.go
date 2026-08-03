package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver for database/sql
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
	Claims        int    // rewards collected so far (one per referral target reached)
	HasClaimed    bool   // mirror of Claims > 0 (kept for quick stats)
	ClaimedCard   string // most recently issued card (full history lives in the cards table)
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

// errNoUnlocks is returned by issueCard when the user has no unlocked rewards
// waiting (earned rewards = referrals / target, already-collected = Claims).
var errNoUnlocks = errors.New("no unlocked rewards available")

// unlocksAvailable returns how many rewards the user can still collect:
// every ReferralTarget referrals unlock exactly one card.
func unlocksAvailable(referrals, claims, target int) int {
	if target <= 0 {
		return 0
	}
	earned := referrals / target
	if earned > claims {
		return earned - claims
	}
	return 0
}

// nextRewardIn returns how many more referrals are needed for the next card.
func nextRewardIn(referrals, target int) int {
	if target <= 0 {
		return 0
	}
	return target - referrals%target
}

var db *sql.DB

// ---------- dual-engine support: SQLite (embedded) / PostgreSQL (managed) ----------

// dbDialect identifies which SQL engine backs the bot. Queries are written
// with '?' placeholders and translated for Postgres at run time by rebind.
type dbDialect int

const (
	dialectSQLite dbDialect = iota
	dialectPostgres
)

var dialect = dialectSQLite

// UsingPostgres reports whether the bot is backed by PostgreSQL
// (DATABASE_URL was set at boot). SQLite-only machinery — PRAGMAs and the
// Telegram-file backup/restore — must be skipped then: the managed service
// persists data across redeploys, which is the whole point.
func UsingPostgres() bool { return dialect == dialectPostgres }

// rebind translates '?' bind placeholders to PostgreSQL's $1..$N form.
// Our SQL contains no literal '?' characters, so a straight scan is safe.
func rebind(query string) string {
	if dialect != dialectPostgres {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 8)
	n := 0
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			n++
			fmt.Fprintf(&b, "$%d", n)
			continue
		}
		b.WriteByte(query[i])
	}
	return b.String()
}

const schemaSQLite = `
CREATE TABLE IF NOT EXISTS users (
    id             INTEGER PRIMARY KEY,
    name           TEXT    NOT NULL DEFAULT '',
    username       TEXT    NOT NULL DEFAULT '',
    referrer       INTEGER NOT NULL DEFAULT 0,
    referred_users TEXT    NOT NULL DEFAULT '[]',
    joined_at      INTEGER NOT NULL DEFAULT 0,
    banned         INTEGER NOT NULL DEFAULT 0,
    claims         INTEGER NOT NULL DEFAULT 0,
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
    fsub_paused     INTEGER NOT NULL DEFAULT 0,
    admin_ids       TEXT    NOT NULL DEFAULT '[]',
    support_url     TEXT    NOT NULL DEFAULT '',
    howto_text      TEXT    NOT NULL DEFAULT '',
    emoji_ids       TEXT    NOT NULL DEFAULT '{}',
    announce_channels TEXT  NOT NULL DEFAULT '[]',
    announce_fsub   INTEGER NOT NULL DEFAULT 0,
    userbot_session TEXT    NOT NULL DEFAULT '',
    tz              TEXT    NOT NULL DEFAULT 'UTC'
);
CREATE TABLE IF NOT EXISTS join_requests (
    channel_id   INTEGER NOT NULL,
    user_id      INTEGER NOT NULL,
    requested_at INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (channel_id, user_id)
);
`

// schemaPostgres mirrors schemaSQLite: BIGINT for Telegram IDs / unix
// timestamps, BIGSERIAL for the card counter, TEXT JSON blobs kept as-is so
// row scanning code is byte-identical across engines.
const schemaPostgres = `
CREATE TABLE IF NOT EXISTS users (
    id             BIGINT  PRIMARY KEY,
    name           TEXT    NOT NULL DEFAULT '',
    username       TEXT    NOT NULL DEFAULT '',
    referrer       BIGINT  NOT NULL DEFAULT 0,
    referred_users TEXT    NOT NULL DEFAULT '[]',
    joined_at      BIGINT  NOT NULL DEFAULT 0,
    banned         INTEGER NOT NULL DEFAULT 0,
    claims         INTEGER NOT NULL DEFAULT 0,
    has_claimed    INTEGER NOT NULL DEFAULT 0,
    claimed_card   TEXT    NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS cards (
    id         BIGSERIAL PRIMARY KEY,
    code       TEXT    NOT NULL UNIQUE,
    status     TEXT    NOT NULL DEFAULT 'available',
    created_at BIGINT  NOT NULL DEFAULT 0,
    claimed_by BIGINT  NOT NULL DEFAULT 0,
    claimed_at BIGINT  NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS settings (
    id              INTEGER PRIMARY KEY CHECK (id = 1),
    log_chat_id     BIGINT  NOT NULL DEFAULT 0,
    fsub_channels   TEXT    NOT NULL DEFAULT '[]',
    referral_target INTEGER NOT NULL DEFAULT 5,
    claims_paused   INTEGER NOT NULL DEFAULT 0,
    fsub_paused     INTEGER NOT NULL DEFAULT 0,
    admin_ids       TEXT    NOT NULL DEFAULT '[]',
    support_url     TEXT    NOT NULL DEFAULT '',
    howto_text      TEXT    NOT NULL DEFAULT '',
    emoji_ids       TEXT    NOT NULL DEFAULT '{}',
    announce_channels TEXT  NOT NULL DEFAULT '[]',
    announce_fsub   INTEGER NOT NULL DEFAULT 0,
    userbot_session TEXT    NOT NULL DEFAULT '',
    tz              TEXT    NOT NULL DEFAULT 'UTC'
);
CREATE TABLE IF NOT EXISTS join_requests (
    channel_id   BIGINT NOT NULL,
    user_id      BIGINT NOT NULL,
    requested_at BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (channel_id, user_id)
);
`

// settingsMigrations adds newer settings columns to databases created by
// older builds. SQLite errors (duplicate column) are expected and ignored;
// the Postgres variants use IF NOT EXISTS instead.
var settingsMigrations = []string{
	`ALTER TABLE settings ADD COLUMN support_url TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE settings ADD COLUMN howto_text TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE settings ADD COLUMN emoji_ids TEXT NOT NULL DEFAULT '{}'`,
	`ALTER TABLE settings ADD COLUMN fsub_paused INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE settings ADD COLUMN announce_channels TEXT NOT NULL DEFAULT '[]'`,
	`ALTER TABLE settings ADD COLUMN announce_fsub INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE settings ADD COLUMN userbot_session TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE settings ADD COLUMN tz TEXT NOT NULL DEFAULT 'UTC'`,
}

var settingsMigrationsPG = []string{
	`ALTER TABLE settings ADD COLUMN IF NOT EXISTS support_url TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE settings ADD COLUMN IF NOT EXISTS howto_text TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE settings ADD COLUMN IF NOT EXISTS emoji_ids TEXT NOT NULL DEFAULT '{}'`,
	`ALTER TABLE settings ADD COLUMN IF NOT EXISTS fsub_paused INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE settings ADD COLUMN IF NOT EXISTS announce_channels TEXT NOT NULL DEFAULT '[]'`,
	`ALTER TABLE settings ADD COLUMN IF NOT EXISTS announce_fsub INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE settings ADD COLUMN IF NOT EXISTS userbot_session TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE settings ADD COLUMN IF NOT EXISTS tz TEXT NOT NULL DEFAULT 'UTC'`,
}

// userMigrations brings older user tables to the repeat-reward model:
// a claims counter, seeded from the legacy single-claim flag.
var userMigrations = []string{
	`ALTER TABLE users ADD COLUMN claims INTEGER NOT NULL DEFAULT 0`,
}

var userMigrationsPG = []string{
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS claims INTEGER NOT NULL DEFAULT 0`,
}

// userDataMigrations runs guarded data fixes after column migrations.
const userClaimsBackfill = `UPDATE users SET claims = 1 WHERE has_claimed = 1 AND claims = 0`

// initDB opens the bot's database: a managed PostgreSQL instance when
// DATABASE_URL is set (Railway's PostgreSQL plugin injects it
// automatically), otherwise the embedded local SQLite file.
func initDB(path string) error {
	if url := strings.TrimSpace(os.Getenv("DATABASE_URL")); url != "" {
		return initPostgres(url)
	}
	return initSQLite(path)
}

// initSQLite opens (creating if needed) the local SQLite database and ensures the schema.
func initSQLite(path string) error {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"

	var err error
	dialect = dialectSQLite
	db, err = sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	if err = db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %v", err)
	}
	// SQLite handles a single writer best
	db.SetMaxOpenConns(1)

	if _, err = db.Exec(schemaSQLite); err != nil {
		return fmt.Errorf("failed to initialise schema: %v", err)
	}
	for _, m := range settingsMigrations {
		_, _ = db.Exec(m) // duplicate-column errors are expected, ignored
	}
	for _, m := range userMigrations {
		_, _ = db.Exec(m) // duplicate-column errors are expected, ignored
	}
	// Backfill: users who claimed under the legacy once-per-user model
	// already spent one unlock.
	if _, err = db.Exec(userClaimsBackfill); err != nil {
		return fmt.Errorf("failed to backfill claims: %v", err)
	}
	return nil
}

// initPostgres connects to the managed PostgreSQL (Railway plugin) and
// ensures the schema. Data then survives every redeploy — no volume, no
// Telegram-file restore needed.
func initPostgres(url string) error {
	dialect = dialectPostgres

	var err error
	db, err = sql.Open("pgx", url)
	if err != nil {
		dialect = dialectSQLite
		return fmt.Errorf("failed to open postgres: %v", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)

	// The plugin can cold-start next to the service — be patient.
	for i := 0; i < 5; i++ {
		if err = db.Ping(); err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		return fmt.Errorf("failed to ping postgres: %v", err)
	}

	if _, err = db.Exec(schemaPostgres); err != nil {
		return fmt.Errorf("failed to initialise postgres schema: %v", err)
	}
	for _, m := range settingsMigrationsPG {
		if _, err = db.Exec(m); err != nil {
			return fmt.Errorf("postgres settings migration failed: %v", err)
		}
	}
	for _, m := range userMigrationsPG {
		if _, err = db.Exec(m); err != nil {
			return fmt.Errorf("postgres user migration failed: %v", err)
		}
	}
	if _, err = db.Exec(userClaimsBackfill); err != nil {
		return fmt.Errorf("failed to backfill claims: %v", err)
	}
	log.Printf("PostgreSQL connected — data now persists across redeploys")
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
		&joined, &banned, &u.Claims, &hasClaimed, &u.ClaimedCard)
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

const userCols = "id, name, username, referrer, referred_users, joined_at, banned, claims, has_claimed, claimed_card"
const cardCols = "id, code, status, created_at, claimed_by, claimed_at"

// ---------- Users ----------

func addUser(user User) error {
	var count int
	err := db.QueryRow(rebind("SELECT COUNT(1) FROM users WHERE id = ?"), user.ID).Scan(&count)
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
		rebind("INSERT INTO users (id, name, username, referrer, joined_at) VALUES (?, ?, ?, ?, ?)"),
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
	_, err = db.Exec(rebind("UPDATE users SET referred_users = ? WHERE id = ?"), string(data), referrerID)
	if err != nil {
		return fmt.Errorf("failed to update referrer's referred users: %v", err)
	}
	return nil
}

func getUser(userID int64) (*User, error) {
	return scanUser(db.QueryRow(rebind("SELECT "+userCols+" FROM users WHERE id = ?"), userID))
}

// updateUserProfile refreshes the stored name/username (best effort).
func updateUserProfile(userID int64, name, username string) {
	_, err := db.Exec(rebind("UPDATE users SET name = ?, username = ? WHERE id = ?"), name, username, userID)
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
	rows, err := db.Query(rebind("SELECT "+userCols+" FROM users ORDER BY joined_at DESC LIMIT ?"), limit)
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
	res, err := db.Exec(rebind("UPDATE users SET banned = ? WHERE id = ?"), flag, userID)
	if err != nil {
		return fmt.Errorf("failed to update ban status for user %d: %v", userID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("user with ID %d does not exist", userID)
	}
	return nil
}

// resetUserClaim clears a user's spent claims so every reward their
// referrals have earned becomes collectable again (admin tool).
func resetUserClaim(userID int64) error {
	res, err := db.Exec(rebind("UPDATE users SET claims = 0, has_claimed = 0, claimed_card = '' WHERE id = ?"), userID)
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
			if _, uerr := db.Exec(rebind("UPDATE users SET referred_users = ? WHERE id = ?"), string(data), u.Referrer); uerr != nil {
				fmt.Printf("failed to unlink user %d from referrer %d: %v\n", userID, u.Referrer, uerr)
			}
		}
	}

	res, err := db.Exec(rebind("DELETE FROM users WHERE id = ?"), userID)
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

	// "Ignore duplicates" is dialect-specific: SQLite OR IGNORE / Postgres ON CONFLICT.
	insertCard := "INSERT OR IGNORE INTO cards (code, status, created_at) VALUES (?, ?, ?)"
	if UsingPostgres() {
		insertCard = "INSERT INTO cards (code, status, created_at) VALUES ($1, $2, $3) ON CONFLICT (code) DO NOTHING"
	}
	stmt, err := tx.Prepare(insertCard)
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
	err := db.QueryRow(rebind("SELECT COUNT(1) FROM cards WHERE status = ?"), CardAvailable).Scan(&n)
	return n, err
}

func countClaimedCards() (int64, error) {
	var n int64
	err := db.QueryRow(rebind("SELECT COUNT(1) FROM cards WHERE status = ?"), CardClaimed).Scan(&n)
	return n, err
}

// issueCard atomically grants ONE unlocked reward card to a user.
//
// Reward model: every `target` referrals unlock exactly one card, repeatable
// without a lifetime cap (user's example: target 5 → 25 referrals = 5 cards).
// A physical card code is still issued exactly once, system-wide, forever.
//
// Hard guarantees, enforced by conditional writes inside a single transaction:
//   - the same card can never be issued twice (card-side gate),
//   - a user never collects more cards than unlocks earned, even across
//     concurrent double-taps/devices (optimistic user-side gate),
//   - if the stock is empty nothing is spent — the unlock stays available
//     so the user can claim after a restock.
//
// Returns errNoStock when the stock is empty and errNoUnlocks when every
// earned reward has already been collected.
func issueCard(userID int64, target int) (*Card, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %v", err)
	}
	defer tx.Rollback()

	// 1) Load the user's referral count and spent claims.
	var refJSON string
	var claims int
	err = tx.QueryRow(rebind("SELECT referred_users, claims FROM users WHERE id = ?"), userID).Scan(&refJSON, &claims)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user with ID %d does not exist", userID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to check user: %v", err)
	}
	var referrals []int64
	if err = json.Unmarshal([]byte(refJSON), &referrals); err != nil {
		return nil, fmt.Errorf("failed to decode referrals for %d: %v", userID, err)
	}
	if unlocksAvailable(len(referrals), claims, target) <= 0 {
		return nil, errNoUnlocks
	}

	// 2) Per-user gate: exactly one concurrent flow can spend one unlock
	//    (claims is only incremented from the value this flow observed).
	res, err := tx.Exec(rebind("UPDATE users SET claims = claims + 1, has_claimed = 1 WHERE id = ? AND claims = ?"), userID, claims)
	if err != nil {
		return nil, fmt.Errorf("failed to lock user claim: %v", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, errNoUnlocks // lost the race — state changed under us
	}

	// 3) Oldest available card.
	var (
		cardID  int64
		code    string
		created int64
	)
	err = tx.QueryRow(rebind(
		"SELECT id, code, created_at FROM cards WHERE status = ? ORDER BY created_at LIMIT 1"),
		CardAvailable,
	).Scan(&cardID, &code, &created)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errNoStock // rollback also un-spends the unlock
		}
		return nil, fmt.Errorf("failed to fetch card: %v", err)
	}

	// 4) Per-card gate: exactly one flow can flip available -> claimed.
	now := time.Now().Unix()
	res, err = tx.Exec(rebind(
		"UPDATE cards SET status = ?, claimed_by = ?, claimed_at = ? WHERE id = ? AND status = ?"),
		CardClaimed, userID, now, cardID, CardAvailable,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to claim card: %v", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, errNoStock // lost the race — behaves like empty stock for this attempt
	}

	// 5) Mirror the latest issued card on the user in the SAME transaction,
	//    so a card can never be "lost" between claiming and delivery.
	if _, err = tx.Exec(rebind("UPDATE users SET claimed_card = ? WHERE id = ?"), code, userID); err != nil {
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

// getUserCards returns the cards a user has collected, most recent first.
func getUserCards(userID int64, limit int64) ([]Card, error) {
	rows, err := db.Query(rebind(
		"SELECT "+cardCols+" FROM cards WHERE claimed_by = ? ORDER BY claimed_at DESC, id DESC LIMIT ?"),
		userID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve user cards: %v", err)
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

// getRecentClaims returns the latest claimed cards (most recent first).
func getRecentClaims(limit int64) ([]Card, error) {
	rows, err := db.Query(rebind(
		"SELECT "+cardCols+" FROM cards WHERE status = ? ORDER BY claimed_at DESC, id DESC LIMIT ?"),
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
	res, err := db.Exec(rebind("DELETE FROM cards WHERE status = ?"), CardClaimed)
	if err != nil {
		return 0, fmt.Errorf("failed to clear claimed cards: %v", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// saveJoinRequest records a pending admin-approval join request (private
// channels/groups with "join by request"). Stored upsert-style so repeat
// requests just refresh the timestamp.
func saveJoinRequest(channelID, userID int64) error {
	q := `INSERT OR REPLACE INTO join_requests (channel_id, user_id, requested_at) VALUES (?, ?, ?)`
	if dialect == dialectPostgres {
		q = `INSERT INTO join_requests (channel_id, user_id, requested_at) VALUES ($1, $2, $3)
		     ON CONFLICT (channel_id, user_id) DO UPDATE SET requested_at = EXCLUDED.requested_at`
	}
	_, err := db.Exec(q, channelID, userID, time.Now().Unix())
	return err
}

// hasJoinRequest reports whether the user has a recorded pending join
// request for this channel.
func hasJoinRequest(channelID, userID int64) bool {
	var n int
	err := db.QueryRow(rebind(
		`SELECT COUNT(1) FROM join_requests WHERE channel_id = ? AND user_id = ?`),
		channelID, userID).Scan(&n)
	return err == nil && n > 0
}

// deleteJoinRequest drops a stored request (e.g. the user is now a real
// member, so the pending marker is obsolete).
func deleteJoinRequest(channelID, userID int64) {
	_, _ = db.Exec(rebind(
		`DELETE FROM join_requests WHERE channel_id = ? AND user_id = ?`),
		channelID, userID)
}
