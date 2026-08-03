package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	tgauth "github.com/gotd/td/telegram/auth"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// ─────────────────────────────────────────────────────────────────────────────
// MTProto premium editor (userbot) — the "Telethon trick", in Go.
//
// Telegram lets BOTS post custom emoji in private/group/supergroup chats when
// the owner has Premium, but CHANNEL posts require a paid Fragment username.
// However, a PREMIUM USER may use custom emoji anywhere — and channel admins
// with "Edit messages" rights can edit other posters' messages. So:
//
//	bot posts the announcement (plain Unicode, always delivered)
//	→ the owner's Premium account (this MTProto client) detects the post
//	→ edits it in place, swapping Unicode emojis for real custom-emoji entities
//
// Effect: premium emojis in channels with ZERO purchase. Groups/DMs keep using
// the native Bot-API path; this ONLY patches channels (where edits are legal).
//
// Requirements: Telegram API credentials (my.telegram.org → API_ID/API_HASH)
// and a one-time /userbot login (phone → OTP → optional 2FA password) done
// right inside this bot's DM with the owner. The session is stored in the
// settings table — treat it like a password: IT GRANTS FULL ACCOUNT ACCESS.
// ─────────────────────────────────────────────────────────────────────────────

// userbot session persistence (settings table; base64-encoded auth key).
type dbSessionStorage struct{}

func (dbSessionStorage) LoadSession(ctx context.Context) ([]byte, error) {
	var enc string
	err := db.QueryRow("SELECT userbot_session FROM settings WHERE id = 1").Scan(&enc)
	if err != nil || enc == "" {
		return nil, session.ErrNotFound
	}
	return base64.StdEncoding.DecodeString(enc)
}

func (dbSessionStorage) StoreSession(_ context.Context, data []byte) error {
	_, err := db.Exec(rebind("UPDATE settings SET userbot_session = ? WHERE id = 1"),
		base64.StdEncoding.EncodeToString(data))
	return err
}

// clearUserbotSession wipes the stored session (logout path).
func clearUserbotSession() {
	_, _ = db.Exec(rebind("UPDATE settings SET userbot_session = '' WHERE id = 1"))
}

// hasUserbotSession reports whether a stored session exists (boot resume).
func hasUserbotSession() bool {
	var enc string
	err := db.QueryRow("SELECT userbot_session FROM settings WHERE id = 1").Scan(&enc)
	return err == nil && enc != ""
}

// Note on design: the login conversation handlers NEVER block waiting for
// Telegram — several handler instances can be in flight at once (phone,
// code, 2FA password are separate messages), and a blocked waiter timing
// out would abort() a session that JUST succeeded. Instead the auth
// goroutine DMs prompts/verdicts itself; handlers only forward inputs.
type ubPhase int

const (
	ubPhaseIdle ubPhase = iota
	ubPhaseCode
	ubPhasePass
)

// ubManager owns the MTProto client lifecycle + the premium editor.
type ubManager struct {
	mu       sync.Mutex
	appID    int
	appHash  string
	botID    int64
	bot      *gotgbot.Bot // for DM replies during login
	owner    int64
	client   *telegram.Client
	disp     tg.UpdateDispatcher
	updates  *updates.Manager
	runCtx   context.Context
	runStop  context.CancelFunc
	running  bool
	loggedIn string // "Name (@username)" once authenticated
	phase    ubPhase
	inputCh  chan string // conversation handler → auth goroutine

	// editor bookkeeping
	recentPosts map[int64]map[int]time.Time // chat → msgID → when (bot's own posts)
	edited      map[string]bool             // "chat:msg" already patched
	recentMu    sync.Mutex
}

var ubMgr = &ubManager{
	phase:       ubPhaseIdle,
	recentPosts: map[int64]map[int]time.Time{},
	edited:      map[string]bool{},
}

func (m *ubManager) configure(appID int, appHash string, b *gotgbot.Bot, owner int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appID, m.appHash, m.bot, m.owner = appID, appHash, b, owner
}

func (m *ubManager) setBotID(id int64) {
	m.mu.Lock()
	m.botID = id
	m.mu.Unlock()
}

// setActor points login verdict DMs at whoever started /userbot
// (owner or developer); boot default is OwnerID (for resume notices).
func (m *ubManager) setActor(id int64) {
	m.mu.Lock()
	m.owner = id
	m.mu.Unlock()
}

func (m *ubManager) credsOK() bool { return m.appID != 0 && m.appHash != "" }

// isRunning reports whether the MTProto client is up and authenticated.
func (m *ubManager) isRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

func (m *ubManager) statusLine() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return fmt.Sprintf("🟢 <b>ON</b> — logged in as <b>%s</b>, patching channel posts with premium emojis.", esc(m.loggedIn))
	}
	return "🔴 <b>OFF</b> — channel posts keep standard emoji."
}

// ── login conversation (runs inside the bot DM) ─────────────────────────────

const ubStateInput = "UB_INPUT"

// userbotCommand backs /userbot: status + login kickoff (owner/developer only).
func userbotCommand(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	if msg.From == nil || !isOwner(msg.From.Id) {
		_, _ = msg.Reply(b, "❌ Owner only.", nil)
		return nil
	}

	// Phone numbers, OTPs and 2FA passwords must NEVER travel through a
	// group chat — the whole login flow is DM-only.
	if ctx.EffectiveChat == nil || ctx.EffectiveChat.Type != "private" {
		_, _ = msg.Reply(b, "🔐 This login only works in <b>DM</b> — never share your phone/OTP in a group. Send me /userbot in a private chat.",
			&gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return nil
	}

	if !ubMgr.credsOK() {
		_, _ = msg.Reply(b,
			"⚠️ <b>API credentials missing.</b>\n\nSet <code>API_ID</code> and <code>API_HASH</code> (from my.telegram.org → API development tools) in the env and redeploy, then run /userbot again.",
			&gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return nil
	}

	if ubMgr.isRunning() {
		kb := gotgbot.InlineKeyboardMarkup{InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{{Text: "🔌 Logout", CallbackData: "ubl.logout"}},
			{{Text: "🔄 Refresh", CallbackData: "ubl.status"}},
		}}
		decorateButtons(&kb)
		_, _ = msg.Reply(b, premiumize(
			"🤖 <b>Premium Channel Editor</b>\n\n"+ubMgr.statusLine()+
				"\n\nEvery announcement the bot posts to your channels gets edited in-place by your Premium account — real custom emoji, no Fragment username needed."),
			&gotgbot.SendMessageOpts{ParseMode: "HTML", ReplyMarkup: kb})
		return nil
	}

	// Start the login conversation — verdict DMs go to whoever invoked it
	// (owner OR developer), not a fixed recipient.
	ubMgr.setActor(msg.From.Id)
	ubMgr.beginFlow()
	_, _ = msg.Reply(b,
		"🤖 <b>Premium Channel Editor — login</b>\n\n"+
			"Send the <b>phone number</b> of the Premium account (with country code, e.g. <code>+919876543210</code>). Telegram will send it a login code.\n\n"+
			"<i>This account must be an <b>admin with Edit-messages rights</b> in the announcement channels. The stored session grants full account access — cancel anytime with /cancel.</i>",
		&gotgbot.SendMessageOpts{ParseMode: "HTML", ReplyMarkup: admConvBackBtn("settings")})
	return handlers.NextConversationState(ubStateInput)
}

// beginFlow resets per-login state (non-blocking if a run already exists).
func (m *ubManager) beginFlow() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inputCh = make(chan string)
	m.phase = ubPhaseIdle
}

// provide hands conversation input to the auth goroutine; false when no
// login step is waiting (stale/duplicate message).
func (m *ubManager) provide(s string) bool {
	m.mu.Lock()
	ch := m.inputCh
	m.mu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case ch <- s:
		return true
	default:
		return false
	}
}

// userbotInputMessage receives phone → OTP → 2FA password inputs. It never
// blocks for a verdict (see the design note above) — the run goroutine
// sends the prompts and the final result as regular DMs.
func userbotInputMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	if msg.From == nil || !isOwner(msg.From.Id) {
		return handlers.EndConversation()
	}
	text := strings.TrimSpace(msg.GetText())
	if text == "" {
		return nil
	}

	// phone steps start the client run; code/pass feed it.
	m := ubMgr
	m.mu.Lock()
	startPhone := m.phase == ubPhaseIdle && !m.running
	m.mu.Unlock()

	if startPhone {
		if !looksLikePhone(text) {
			_, _ = msg.Reply(b, "❌ That doesn't look like a phone number. Use international format: <code>+919876543210</code>",
				&gotgbot.SendMessageOpts{ParseMode: "HTML"})
			return nil
		}
		if err := m.startRun(text); err != nil {
			_, _ = msg.Reply(b, "❌ Couldn't start the login client: "+esc(err.Error()), nil)
			return handlers.EndConversation()
		}
		_, _ = msg.Reply(b, "📨 Login client started — contacting Telegram. Send the code here as soon as it arrives.", nil)
		return nil
	}

	if !m.provide(text) {
		_, _ = msg.Reply(b, "⚠️ One moment… the login client isn't ready yet. Send it again, or /cancel.", nil)
		return nil
	}
	_, _ = msg.Reply(b, "⏳ Checking…", nil)
	return nil
}

// looksLikePhone is a loose sanity check before bothering Telegram.
func looksLikePhone(s string) bool {
	s = strings.TrimPrefix(s, "+")
	if len(s) < 8 || len(s) > 15 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// ── MTProto lifecycle ───────────────────────────────────────────────────────

// startRun spins up a fresh client (login flow in the run goroutine).
func (m *ubManager) startRun(phone string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.credsOK() {
		return errors.New("API_ID/API_HASH not configured")
	}
	if m.running {
		return errors.New("already running")
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.runCtx, m.runStop = ctx, cancel
	m.phase = ubPhaseCode

	go m.run(phone, true)
	return nil
}

// autoResume is called at boot: silently restarts the editor when a stored
// session + API credentials exist. Never prompts — a revoked session just
// flips the editor OFF with a log line (owner re-runs /userbot).
func (m *ubManager) autoResume() {
	if !m.credsOK() || !hasUserbotSession() {
		return
	}
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.runCtx, m.runStop = ctx, cancel
	m.mu.Unlock()
	log.Printf("🤖 userbot: resuming stored session…")
	go m.run("", false)
}

// abort kills the client run + clears pending channels (logout/timeout).
func (m *ubManager) abort() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runStop != nil {
		m.runStop()
	}
	m.running = false
	m.loggedIn = ""
	m.phase = ubPhaseIdle
	m.inputCh = nil
}

// logout: abort + wipe the stored session.
func (m *ubManager) logout() {
	m.abort()
	clearUserbotSession()
}

// run owns the MTProto client for its whole lifetime.
func (m *ubManager) run(phone string, interactive bool) {
	defer func() {
		m.mu.Lock()
		m.running = false
		m.loggedIn = ""
		m.mu.Unlock()
	}()

	m.disp = tg.NewUpdateDispatcher()
	m.disp.OnNewChannelMessage(m.onChannelMessage)
	m.updates = updates.New(updates.Config{Handler: m.disp})

	client := telegram.NewClient(m.appID, m.appHash, telegram.Options{
		SessionStorage: dbSessionStorage{},
		UpdateHandler:  m.updates,
	})
	m.mu.Lock()
	m.client = client
	m.mu.Unlock()

	ctx := m.runCtx
	err := client.Run(ctx, func(ctx context.Context) error {
		if err := m.authenticate(ctx, client, phone, interactive); err != nil {
			if errors.Is(err, context.Canceled) {
				return err // owner pressed /cancel — stay quiet
			}
			if interactive {
				m.dm("❌ <b>Login failed</b>: " + esc(err.Error()) + "\n\nStart over with /userbot.")
			} else {
				log.Printf("🤖 userbot: stored session no longer valid (%v) — /userbot to re-login", err)
			}
			return err
		}

		self, err := client.Self(ctx)
		if err != nil {
			return err
		}
		name := self.FirstName
		if self.Username != "" {
			name += " (@" + self.Username + ")"
		}
		m.mu.Lock()
		m.running = true
		m.loggedIn = name
		m.mu.Unlock()
		log.Printf("🤖 userbot ON as %s — premium editor watching announcements", name)

		if interactive {
			m.dm(premiumize(fmt.Sprintf(
				"✅ <b>Editor ON!</b>\n\nLogged in as <b>%s</b>.\n\nFrom now on, every announcement the bot posts to your channels gets edited by this account within 1-2 seconds — real premium emojis. The bot DM/group flow stays exactly the same.\n\n<i>Login complete — press /cancel to close this conversation.</i>",
				esc(name))))
		} else {
			m.dm(premiumize(
				"🤖 <b>Premium Editor resumed</b> — logged in as <b>" + esc(name) + "</b>, channel posts upgrade automatically."))
		}

		// Updates loop; returns when ctx is cancelled (logout/shutdown).
		return m.updates.Run(ctx, client.API(), self.ID, updates.AuthOptions{})
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("🤖 userbot stopped: %v", err)
	}
}

// dm sends the owner a bot DM (HTML parse mode), swallowing errors — the
// MTProto goroutine uses it for every login prompt/verdict.
func (m *ubManager) dm(html string) {
	m.mu.Lock()
	b, owner := m.bot, m.owner
	m.mu.Unlock()
	if b == nil || owner == 0 {
		return
	}
	_, _ = b.SendMessage(owner, html, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
}

// waitInput blocks for the next conversation input (or cancellation).
func (m *ubManager) waitInput(ctx context.Context) (string, error) {
	m.mu.Lock()
	ch := m.inputCh
	m.mu.Unlock()
	if ch == nil {
		return "", errors.New("login aborted")
	}
	select {
	case s := <-ch:
		return s, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// authenticate performs status→code→2FA using conversation input; already
// authorized sessions short-circuit.
func (m *ubManager) authenticate(ctx context.Context, client *telegram.Client, phone string, interactive bool) error {
	st, err := client.Auth().Status(ctx)
	if err != nil {
		return err
	}
	if st.Authorized {
		return nil
	}
	if !interactive {
		return errors.New("not authorized")
	}

	sent, err := client.Auth().SendCode(ctx, phone, tgauth.SendCodeOptions{})
	if err != nil {
		return fmt.Errorf("send code: %w", err)
	}
	sc, ok := sent.(*tg.AuthSentCode)
	if !ok {
		return errors.New("unexpected sent-code type (already authorized?)")
	}

	m.dm("📨 <b>Login code sent</b> to that account's Telegram app.\n\nSend the code here exactly as shown (e.g. <code>12345</code>).")

	for attempt := 0; attempt < 3; attempt++ {
		code, err := m.waitInput(ctx)
		if err != nil {
			return err
		}
		code = strings.TrimSpace(code)
		_, signErr := client.Auth().SignIn(ctx, phone, code, sc.PhoneCodeHash)
		switch {
		case signErr == nil:
			return nil
		case errors.Is(signErr, tgauth.ErrPasswordAuthNeeded):
			return m.authenticatePassword(ctx, client)
		case attempt < 2:
			m.dm("❌ That code looks wrong. Check the latest code in the Telegram app and <b>send it again</b>.")
		default:
			return fmt.Errorf("sign in: %w", signErr)
		}
	}
	return errors.New("code attempts exhausted")
}

// authenticatePassword handles the 2FA step (up to 3 tries).
func (m *ubManager) authenticatePassword(ctx context.Context, client *telegram.Client) error {
	m.mu.Lock()
	m.phase = ubPhasePass
	m.mu.Unlock()
	m.dm("🔐 <b>Two-step verification is on</b> — send your cloud password.\n\n<i>(The password is only used for this login; it is never stored.)</i>")
	for attempt := 0; attempt < 3; attempt++ {
		pass, err := m.waitInput(ctx)
		if err != nil {
			return err
		}
		if _, err := client.Auth().Password(ctx, pass); err == nil {
			return nil
		} else if attempt < 2 {
			m.dm("❌ That password looks wrong. <b>Send it again</b>.")
		} else {
			return fmt.Errorf("password: %w", err)
		}
	}
	return errors.New("password attempts exhausted")
}

// ── premium editor ──────────────────────────────────────────────────────────

// notePosted is called by the announcer for every message the bot itself
// posts to an alert channel — the safest possible edit filter.
func (m *ubManager) notePosted(chatID int64, msgID int) {
	m.recentMu.Lock()
	defer m.recentMu.Unlock()
	chat := m.recentPosts[chatID]
	if chat == nil {
		chat = map[int]time.Time{}
		m.recentPosts[chatID] = chat
	}
	chat[msgID] = time.Now()
	// prune (keep memory bounded on busy channels)
	if len(chat) > 500 {
		cutoff := time.Now().Add(-10 * time.Minute)
		for id, at := range chat {
			if at.Before(cutoff) {
				delete(chat, id)
			}
		}
		// hard cap: a same-second flood of 500+ posts can't grow the map
		// forever — drop the oldest tracked posts first
		if len(chat) > 500 {
			type kv struct {
				id int
				at time.Time
			}
			ids := make([]kv, 0, len(chat))
			for id, at := range chat {
				ids = append(ids, kv{id, at})
			}
			sort.Slice(ids, func(i, j int) bool { return ids[i].at.Before(ids[j].at) })
			for i := 0; len(chat) > 500 && i < len(ids); i++ {
				delete(chat, ids[i].id)
			}
		}
	}
}

// wasPostedByBot reports whether this exact message came from our own
// announcer (recentPosts) or from the bot account per the update's FromID.
func (m *ubManager) wasPostedByBot(msg *tg.Message) bool {
	peer, ok := msg.PeerID.(*tg.PeerChannel)
	if !ok {
		return false
	}
	m.recentMu.Lock()
	defer m.recentMu.Unlock()
	if chat := m.recentPosts[peer.ChannelID]; chat != nil {
		if _, ok := chat[msg.ID]; ok {
			return true
		}
	}
	m.mu.Lock()
	botID := m.botID
	m.mu.Unlock()
	if botID == 0 {
		return false
	}
	if from, ok := msg.FromID.(*tg.PeerUser); ok {
		return from.UserID == botID
	}
	return false
}

// editorTargets is the live set of announcement destinations (same rule as
// the broadcast: alert channels ∪ force-join channels when relay is on).
func editorTargets() map[int64]bool {
	out := map[int64]bool{}
	for _, id := range stockNotifyTargets(getAnnounceChannels(), getFsubChannels(), getAnnounceFsub()) {
		out[id] = true
	}
	return out
}

// onChannelMessage is the updates-dispatcher hook: filter → entities → edit.
func (m *ubManager) onChannelMessage(ctx context.Context, e tg.Entities, upd *tg.UpdateNewChannelMessage) error {
	msg, ok := upd.Message.(*tg.Message)
	if !ok {
		return nil
	}
	peer, ok := msg.PeerID.(*tg.PeerChannel)
	if !ok {
		return nil
	}
	if !editorTargets()[peer.ChannelID] {
		return nil
	}
	if !m.wasPostedByBot(msg) {
		return nil
	}
	key := fmt.Sprintf("%d:%d", peer.ChannelID, msg.ID)
	m.recentMu.Lock()
	if m.edited[key] {
		m.recentMu.Unlock()
		return nil
	}
	m.recentMu.Unlock()

	entities := premiumEntities(msg.Message)
	if len(entities) == 0 {
		return nil // nothing to upgrade (already fine)
	}

	// Access hash from the update's own channel entity (present for posts).
	ch := e.Channels[peer.ChannelID]
	if ch == nil {
		log.Printf("🤖 editor: no access hash for channel %d yet, skipping msg %d", peer.ChannelID, msg.ID)
		return nil
	}
	m.mu.Lock()
	client := m.client
	m.mu.Unlock()
	if client == nil {
		return nil
	}

	req := &tg.MessagesEditMessageRequest{
		Peer: &tg.InputPeerChannel{ChannelID: peer.ChannelID, AccessHash: ch.AccessHash},
		ID:   msg.ID,
	}
	req.SetMessage(msg.Message)
	req.SetEntities(entities)
	req.SetNoWebpage(true)

	err := m.editWithFloodRetry(ctx, client, req)
	m.recentMu.Lock()
	if err == nil {
		m.edited[key] = true
		if len(m.edited) > 2000 {
			m.edited = map[string]bool{key: true}
		}
	}
	m.recentMu.Unlock()

	if err != nil {
		log.Printf("🤖 editor: edit failed for %s: %v", key, err)
	} else {
		log.Printf("✨ editor: premium-ified %s (%d emoji entities)", key, len(entities))
	}
	return nil
}

// editWithFloodRetry runs the edit, sleeping through one FLOOD_WAIT.
func (m *ubManager) editWithFloodRetry(ctx context.Context, client *telegram.Client, req *tg.MessagesEditMessageRequest) error {
	_, err := client.API().MessagesEditMessage(ctx, req)
	if err == nil {
		return nil
	}
	// FloodWait sleeps the server-mandated duration when (and only when)
	// the error is FLOOD_WAIT — returns ctx.Err() if cancelled mid-sleep.
	ok, waitErr := tgerr.FloodWait(ctx, err)
	if !ok {
		return waitErr
	}
	log.Printf("🤖 editor: FLOOD_WAIT slept out — retrying edit once")
	_, err = client.API().MessagesEditMessage(ctx, req)
	return err
}

// utf16Units counts UTF-16 code units (BMP rune = 1, astral rune = 2) —
// MTProto entity offsets are measured in these, not bytes or runes.
func utf16Units(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// premiumEntities converts every registry emoji occurrence in text into a
// custom-emoji entity (UTF-16 offsets, per MTProto rules). Pure function.
func premiumEntities(text string) []tg.MessageEntityClass {
	if text == "" {
		return nil
	}
	mapping := getEmojiIDs()
	if len(mapping) == 0 {
		return nil
	}
	docByGlyph := map[string]int64{}
	for slot, id := range mapping {
		def := iconDefaults[slot]
		if def == "" || !isEmojiID(id) {
			continue
		}
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			continue
		}
		docByGlyph[def] = n
	}
	if len(docByGlyph) == 0 {
		return nil
	}
	// longest-first so overlapping prefixes resolve deterministically
	glyphs := make([]string, 0, len(docByGlyph))
	for g := range docByGlyph {
		glyphs = append(glyphs, g)
	}
	sort.Slice(glyphs, func(i, j int) bool { return len(glyphs[i]) > len(glyphs[j]) })

	var out []tg.MessageEntityClass
	var utf16off, byteOff int
	for byteOff < len(text) {
		matched := false
		for _, g := range glyphs {
			if strings.HasPrefix(text[byteOff:], g) {
				l := utf16Units(g)
				out = append(out, &tg.MessageEntityCustomEmoji{
					Offset:     utf16off,
					Length:     l,
					DocumentID: docByGlyph[g],
				})
				utf16off += l
				byteOff += len(g)
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		_, size := utf8.DecodeRuneInString(text[byteOff:])
		utf16off += utf16Units(text[byteOff : byteOff+size])
		byteOff += size
	}
	return out
}

// userbotCancel backs /cancel inside the login conversation: abort the
// pending MTProto run (if any) so its auth goroutine isn't parked on
// waitInput forever, then render the generic cancel reply.
func userbotCancel(b *gotgbot.Bot, ctx *ext.Context) error {
	ubMgr.abort()
	return adminCancel(b, ctx)
}

// userbotConversationBack does the same for the 🔙 Back button: kill the
// pending login before the panel re-renders.
func userbotConversationBack(b *gotgbot.Bot, ctx *ext.Context) error {
	ubMgr.abort()
	return adminConversationBack(b, ctx)
}

// ── status / logout callback ────────────────────────────────────────────────

// userbotCallback handles ubl.* buttons on the /userbot status message.
func userbotCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	query := ctx.CallbackQuery
	if !isOwner(query.From.Id) {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "❌ Owner only.", ShowAlert: true})
		return nil
	}
	switch strings.TrimPrefix(query.Data, "ubl.") {
	case "logout":
		ubMgr.logout()
		log.Printf("userbot logged out by owner")
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "🔌 Logged out — session wiped."})
		_, _, _ = ctx.EffectiveMessage.EditText(b, premiumize(
			"🤖 <b>Premium Channel Editor</b>\n\n"+ubMgr.statusLine()+"\n\nRun /userbot again whenever you want to log back in."),
			&gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
	case "status":
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "🔄 Refreshed"})
		kb := gotgbot.InlineKeyboardMarkup{InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{{Text: "🔌 Logout", CallbackData: "ubl.logout"}},
			{{Text: "🔄 Refresh", CallbackData: "ubl.status"}},
		}}
		decorateButtons(&kb)
		_, _, _ = ctx.EffectiveMessage.EditText(b, premiumize(
			"🤖 <b>Premium Channel Editor</b>\n\n"+ubMgr.statusLine()),
			&gotgbot.EditMessageTextOpts{ParseMode: "HTML", ReplyMarkup: kb})
	}
	return nil
}
