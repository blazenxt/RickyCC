package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// Automatic database backup & restore.
//
// Hosting platforms with ephemeral filesystems (Railway without a volume,
// Heroku, Render free) wipe bot.db on every redeploy. To survive that, the
// bot periodically uploads bot.db as a document into the LOGGER_ID chat and
// keeps the LATEST backup pinned. On boot, if the local DB file doesn't
// exist, the newest pinned backup is downloaded and restored automatically
// — before initDB() ever runs.
//
// The backup chat is ALWAYS the LOGGER_ID env var (not the panel-managed log
// chat), because env config is the only state guaranteed to survive a wipe.
//
// Requirements: bot must be able to post in that chat and pin messages
// (admin with pin rights in channels/groups).

const (
	backupPrefix   = "premiumcard-backup"
	backupInterval = 30 * time.Minute
	backupFirstRun = 90 * time.Second // first backup right after boot
)

var (
	// backupChatID is the LOGGER_ID env value, set in main().
	backupChatID int64

	// dbFilePath is the SQLite path, set in main() before initDB().
	dbFilePath string

	backupMu      sync.Mutex
	lastBackupPin int64

	// restoredFromBackup is set by maybeRestoreDB for the boot log line.
	restoredFromBackup bool
)

// runDBBackup checkpoints the WAL, copies bot.db, uploads it to the backup
// chat and pins it (replacing our previous pin).
func runDBBackup(b *gotgbot.Bot, chatID int64, reason string) error {
	// Managed PostgreSQL persists across redeploys — a Telegram-pinned SQLite
	// file neither exists nor is needed there.
	if UsingPostgres() {
		log.Printf("db backup skipped (%s): PostgreSQL manages persistence", reason)
		return errors.New("backups not needed: running on managed PostgreSQL (persists across redeploys)")
	}
	if chatID == 0 {
		return errors.New("no backup chat configured (LOGGER_ID is not set)")
	}
	if db == nil {
		return errors.New("database not initialised")
	}

	// Fold the WAL into the main file so the copy is complete.
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return fmt.Errorf("wal checkpoint failed: %v", err)
	}
	data, err := os.ReadFile(dbFilePath)
	if err != nil {
		return fmt.Errorf("failed to read database: %v", err)
	}

	name := fmt.Sprintf("%s-%s.db", backupPrefix, time.Now().UTC().Format("20060102-150405"))
	sent, err := b.SendDocument(chatID, gotgbot.InputFileByReader(name, bytes.NewReader(data)),
		&gotgbot.SendDocumentOpts{
			Caption: premiumize(fmt.Sprintf(
				"💾 <b>%s — database backup</b> <i>(%s)</i>\n\n"+
					"🕒 %s UTC\n📦 %.1f KB\n\n"+
					"<i>Kept pinned — the bot restores from this file automatically on the next deploy.</i>",
				BrandName, reason,
				time.Now().UTC().Format("02 Jan 2006 15:04"), float64(len(data))/1024)),
			ParseMode: "HTML",
		})
	if err != nil {
		return fmt.Errorf("could not upload backup: %v", err)
	}

	backupMu.Lock()
	prev := lastBackupPin
	lastBackupPin = sent.MessageId
	backupMu.Unlock()

	// Keep the pin slot tidy: unpin our older backup (best effort).
	if prev != 0 {
		_, _ = b.UnpinChatMessage(chatID, &gotgbot.UnpinChatMessageOpts{MessageId: &prev})
	}
	if _, err := b.PinChatMessage(chatID, sent.MessageId,
		&gotgbot.PinChatMessageOpts{DisableNotification: true}); err != nil {
		return fmt.Errorf("backup uploaded but PIN FAILED — make the bot an admin with pin rights in the backup chat (auto-restore works off the pin): %v", err)
	}

	log.Printf("💾 DB backup uploaded & pinned (%s, %.1f KB)", reason, float64(len(data))/1024)
	return nil
}

// startDBBackupTicker runs scheduled backups forever (first one right after boot).
func startDBBackupTicker(b *gotgbot.Bot, chatID int64) {
	if UsingPostgres() {
		log.Printf("db backup ticker disabled: PostgreSQL manages persistence")
		return
	}
	if chatID == 0 {
		log.Println("💾 auto-backup disabled — set LOGGER_ID (and pin rights) to enable DB backup & auto-restore")
		return
	}
	log.Printf("💾 auto-backup enabled → chat %d, every %s", chatID, backupInterval)

	go func() {
		time.Sleep(backupFirstRun)
		if err := runDBBackup(b, chatID, "boot"); err != nil {
			log.Printf("boot backup failed: %v", err)
		}
		t := time.NewTicker(backupInterval)
		defer t.Stop()
		for range t.C {
			if err := runDBBackup(b, chatID, "scheduled"); err != nil {
				log.Printf("scheduled backup failed: %v", err)
			}
		}
	}()
}

// maybeRestoreDB runs BEFORE initDB: if the local DB file is absent (fresh
// container), it downloads the newest pinned backup from the backup chat.
// Any failure is logged and the bot simply starts with a fresh database.
func maybeRestoreDB(b *gotgbot.Bot, token string, chatID int64, path string) {
	// PostgreSQL mode: no local file exists to restore — the managed
	// database keeps everything.
	if UsingPostgres() {
		return
	}
	if _, err := os.Stat(path); err == nil {
		return // local database already exists — nothing to restore
	}
	if chatID == 0 {
		log.Println("fresh database, no backup chat configured (LOGGER_ID unset) — starting empty")
		return
	}

	chat, err := b.GetChat(chatID, nil)
	if err != nil {
		log.Printf("db restore: cannot read backup chat %d: %v", chatID, err)
		return
	}
	pinned := chat.PinnedMessage
	if pinned == nil || pinned.Document == nil ||
		!strings.HasPrefix(pinned.Document.FileName, backupPrefix+"-") {
		log.Println("db restore: no pinned backup found — starting empty")
		return
	}

	file, err := b.GetFile(pinned.Document.FileId, nil)
	if err != nil || file.FilePath == "" {
		log.Printf("db restore: GetFile failed: %v", err)
		return
	}

	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", token, file.FilePath)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		log.Printf("db restore: download failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("db restore: download returned %s", resp.Status)
		return
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("db restore: read failed: %v", err)
		return
	}

	// Sanity: SQLite files start with this exact header.
	if len(data) < 100 || string(data[:16]) != "SQLite format 3\x00" {
		log.Println("db restore: pinned file is not a SQLite database — refusing to restore")
		return
	}

	tmp := path + ".restore"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		log.Printf("db restore: write failed: %v", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("db restore: rename failed: %v", err)
		return
	}

	restoredFromBackup = true
	log.Printf("♻️ database restored from pinned backup %s (%.1f KB)",
		pinned.Document.FileName, float64(len(data))/1024)
}
