package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/conversation"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/callbackquery"

	_ "github.com/joho/godotenv/autoload"
	_ "modernc.org/sqlite"
)

const (
	// BrandName is displayed in bot messages.
	BrandName = "⚕️ PREMIUM CARD"
)

var (
	WebhookURL     string
	Port           string
	secretToken    string
	OwnerID        int64
	allowedUpdates = []string{"message", "callback_query"}
)

func main() {
	var err error
	token := os.Getenv("TOKEN")
	if token == "" {
		log.Fatal("TOKEN is not set")
	}

	OwnerID, err = strconv.ParseInt(os.Getenv("OWNER_ID"), 10, 64)
	if err != nil {
		log.Fatal("OWNER_ID is not set")
	}

	// LOGGER_ID is optional — it seeds the log chat, which can be changed
	// any time from the admin panel settings.
	envLogChatID, logErr := strconv.ParseInt(strings.TrimSpace(os.Getenv("LOGGER_ID")), 10, 64)
	if logErr != nil {
		log.Println("LOGGER_ID not set/invalid — configure the log chat later via /admin → Settings")
		envLogChatID = 0
	}

	// FSUB_IDS (comma-separated) seeds the force-join channels on first boot.
	var envFsubIDs []int64
	fsubEnv := strings.TrimSpace(os.Getenv("FSUB_IDS"))
	for _, part := range strings.Split(fsubEnv, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			log.Printf("skipping invalid FSUB_IDS entry %q: %v", part, err)
			continue
		}
		envFsubIDs = append(envFsubIDs, id)
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "bot.db"
	}

	secretToken = os.Getenv("SECRET_TOKEN")
	if secretToken == "" {
		secretToken = "OopsNoSECRET_TOKENFoundTimeToCallSherlock"
	}

	WebhookURL = os.Getenv("WEBHOOK_URL")
	Port = os.Getenv("PORT")

	dbFilePath = dbPath
	backupChatID = envLogChatID

	bot, err := gotgbot.NewBot(token, &gotgbot.BotOpts{
		BotClient: &gotgbot.BaseBotClient{
			Client: http.Client{},
			DefaultRequestOpts: &gotgbot.RequestOpts{
				Timeout: gotgbot.DefaultTimeout,
				APIURL:  gotgbot.DefaultAPIURL,
			},
		},
	})

	if err != nil {
		log.Fatal(err)
	}

	// Ephemeral hosts (Railway w/o volume, Heroku, Render free) wipe bot.db on
	// every redeploy — restore the newest pinned backup from the LOGGER_ID
	// chat BEFORE the database is opened.
	maybeRestoreDB(bot, token, envLogChatID, dbPath)

	if err := initDB(dbPath); err != nil {
		log.Fatalf("Failed to initialise database: %v", err)
	}
	log.Printf("Database ready (SQLite: %s)", dbPath)
	loadConfig(envLogChatID, envFsubIDs)

	// Fresh deploy + bot has emoji rights (Fragment username / Premium
	// owner): light up the whole UI — message icons AND button icons — with
	// the curated premium set automatically. No-op when the owner already
	// configured slots or the probe fails.
	preloadPremiumEmojiSet(bot, OwnerID)

	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{
		Error: func(b *gotgbot.Bot, ctx *ext.Context, err error) ext.DispatcherAction {
			log.Println("an error occurred while handling update:", err.Error())
			return ext.DispatcherActionNoop
		},
		MaxRoutines: ext.DefaultMaxRoutines,
	})

	dispatcher.AddHandler(handlers.NewCommand("start", start))
	dispatcher.AddHandler(handlers.NewCommand("help", help))
	dispatcher.AddHandler(handlers.NewCommand("progress", progressCmd))
	dispatcher.AddHandler(handlers.NewCommand("info", info))
	dispatcher.AddHandler(handlers.NewCommand("addcard", addCard))
	dispatcher.AddHandler(handlers.NewCommand("addcode", addCard)) // legacy alias
	dispatcher.AddHandler(handlers.NewCommand("stock", stock))
	dispatcher.AddHandler(handlers.NewCommand("stats", stats))
	dispatcher.AddHandler(handlers.NewCommand("broadcast", broadcast))
	dispatcher.AddHandler(handlers.NewCommand("backupdb", backupCmd))

	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("progress"), progressCallback))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("claim"), claim))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("home"), home))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("cap."), captchaCallback))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("fsj"), fsubRetryCallback))

	// Admin panel
	dispatcher.AddHandler(handlers.NewCommand("admin", adminCmd))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("admp."), adminCallback))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("admu."), adminUserCallback))

	// Admin panel conversations (find user / add cards)
	dispatcher.AddHandler(handlers.NewConversation(
		[]ext.Handler{handlers.NewCallback(callbackquery.Prefix("admc.finduser"), adminFindUserStart)},
		map[string][]ext.Handler{
			admStateFindUser: {handlers.NewMessage(anyText, adminFindUserMessage)},
		},
		&handlers.ConversationOpts{
			Exits:        []ext.Handler{handlers.NewCommand("cancel", adminCancel)},
			StateStorage: conversation.NewInMemoryStorage(conversation.KeyStrategySenderAndChat),
			AllowReEntry: true,
		},
	))

	dispatcher.AddHandler(handlers.NewConversation(
		[]ext.Handler{handlers.NewCallback(callbackquery.Prefix("admc.addcodes"), adminAddCardsStart)},
		map[string][]ext.Handler{
			admStateAddCards: {handlers.NewMessage(anyText, adminAddCardsMessage)},
		},
		&handlers.ConversationOpts{
			Exits:        []ext.Handler{handlers.NewCommand("cancel", adminCancel)},
			StateStorage: conversation.NewInMemoryStorage(conversation.KeyStrategySenderAndChat),
			AllowReEntry: true,
		},
	))

	// Admin panel settings conversations (log chat / force-join add / referral target / admin add / support link / howto text)
	dispatcher.AddHandler(handlers.NewConversation(
		[]ext.Handler{
			handlers.NewCallback(callbackquery.Prefix("admc.logset"), adminLogSetStart),
			handlers.NewCallback(callbackquery.Prefix("admc.fsubadd"), adminFsubAddStart),
			handlers.NewCallback(callbackquery.Prefix("admc.target"), adminTargetStart),
			handlers.NewCallback(callbackquery.Prefix("admc.adminadd"), adminAdminAddStart),
			handlers.NewCallback(callbackquery.Prefix("admc.support"), adminSupportStart),
			handlers.NewCallback(callbackquery.Prefix("admc.howto"), adminHowtoStart),
			handlers.NewCallback(callbackquery.Prefix("admc.emojis"), adminEmojisStart),
		},
		map[string][]ext.Handler{
			admStateLogSet:   {handlers.NewMessage(anyText, adminLogSetMessage)},
			admStateFsubAdd:  {handlers.NewMessage(anyText, adminFsubAddMessage)},
			admStateTarget:   {handlers.NewMessage(anyText, adminTargetMessage)},
			admStateAdminAdd: {handlers.NewMessage(anyText, adminAdminAddMessage)},
			admStateSupport:  {handlers.NewMessage(anyText, adminSupportMessage)},
			admStateHowto:    {handlers.NewMessage(anyText, adminHowtoMessage)},
			admStateEmojis:   {handlers.NewMessage(anyText, adminEmojisMessage)},
		},
		&handlers.ConversationOpts{
			Exits:        []ext.Handler{handlers.NewCommand("cancel", adminCancel)},
			StateStorage: conversation.NewInMemoryStorage(conversation.KeyStrategySenderAndChat),
			AllowReEntry: true,
		},
	))

	updater := ext.NewUpdater(dispatcher, nil)

	if WebhookURL != "" && Port != "" {
		_, err := bot.SetWebhook(WebhookURL+token, &gotgbot.SetWebhookOpts{
			MaxConnections:     40,
			DropPendingUpdates: true,
			SecretToken:        secretToken,
			AllowedUpdates:     allowedUpdates,
		})

		if err != nil {
			panic("failed to set webhook: " + err.Error())
		}

		err = updater.StartWebhook(bot, token, ext.WebhookOpts{
			ListenAddr:  "0.0.0.0:" + Port,
			SecretToken: secretToken,
		})
		if err != nil {
			log.Fatal(err)
			return
		}
	} else {
		err = updater.StartPolling(bot, &ext.PollingOpts{
			DropPendingUpdates: true,
			GetUpdatesOpts: &gotgbot.GetUpdatesOpts{
				Timeout:        9,
				AllowedUpdates: allowedUpdates,
				RequestOpts: &gotgbot.RequestOpts{
					Timeout: time.Second * 10,
				},
			},
		})

		if err != nil {
			log.Fatal(err)
		}

		// PaaS health checks (Railway / Render web service / Koyeb) expect an
		// HTTP listener on $PORT even when the bot works via long polling.
		if Port != "" {
			go startHealthServer(Port)
		}
	}

	log.Printf("%s has been started...\n", bot.User.Username)

	startDBBackupTicker(bot, envLogChatID)
	if restoredFromBackup && envLogChatID != 0 {
		_, _ = bot.SendMessage(envLogChatID,
			"♻️ <b>Database restored</b> from the pinned backup — all users, cards and settings are back after this redeploy.",
			&gotgbot.SendMessageOpts{ParseMode: "HTML"})
	}
	updater.Idle()
}

// startHealthServer answers platform health checks with 200 OK on "/".
// Only used in polling mode when PORT is set (webhook mode already listens).
func startHealthServer(port string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	})
	log.Printf("Health-check server listening on :%s", port)
	if err := http.ListenAndServe("0.0.0.0:"+port, mux); err != nil {
		log.Printf("health-check server stopped: %v", err)
	}
}

// ---------- UI helpers ----------

// progressBar renders a 🟩/⬜ bar of length `target`.
// progressBar renders a fixed-width bar so even huge referral targets display sanely.
func progressBar(done, target int) string {
	const width = 10
	if target <= 0 {
		return ""
	}
	if done > target {
		done = target
	}
	if done < 0 {
		done = 0
	}
	filled := done * width / target
	return strings.Repeat("🟩", filled) + strings.Repeat("⬜", width-filled)
}

// mainKeyboard is the primary inline keyboard shown on /start and home.
func mainKeyboard(b *gotgbot.Bot, userId int64) gotgbot.InlineKeyboardMarkup {
	referUrl := fmt.Sprintf("https://t.me/%s?start=%d", b.User.Username, userId)
	m := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{
				{
					Text: "👤 Owner",
					Url:  fmt.Sprintf("tg://user?id=%d", OwnerID),
				},
			},
			{
				{
					Text: "🔗 Refer & Earn",
					Url:  fmt.Sprintf("https://t.me/share/url?url=%s", referUrl),
				},
			},
			{
				{
					Text:         "📊 My Progress",
					CallbackData: fmt.Sprintf("progress.%d", userId),
				},
				{
					Text:         "🎁 Claim Reward",
					CallbackData: "claim",
				},
			},
		},
	}
	return *decorateButtons(&m)
}

func homeKeyboard() gotgbot.InlineKeyboardMarkup {
	m := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{
				{
					Text:         "🏠 Home",
					CallbackData: "home",
				},
			},
		},
	}
	return *decorateButtons(&m)
}

func welcomeText(firstName string, u *User, isNew bool) string {
	done := len(u.ReferredUsers)
	target := ReferralTarget
	rem := 0
	if target > 0 {
		rem = done % target
	}
	greeting := icon("wave") + " <b>Welcome back"
	if isNew {
		greeting = icon("party") + " <b>Welcome"
	}
	text := fmt.Sprintf(
		"%s to %s, %s!</b>\n\n"+
			"%s <b>Referrals:</b> %d\n"+
			"%s <b>Rewards claimed:</b> %d\n"+
			"%s <b>Next card:</b> %d/%d  %s\n\n",
		greeting, BrandName, esc(firstName),
		icon("users"), done, icon("gift"), u.Claims,
		icon("next"), rem, target, progressBar(rem, target))

	if ready := unlocksAvailable(done, u.Claims, target); ready > 0 {
		text += fmt.Sprintf("%s <b>%d reward(s) ready!</b> Tap %s Claim Reward below!", icon("trophy"), ready, icon("gift"))
	} else if target > 0 {
		text += fmt.Sprintf("%s Share your link — every <b>%d referrals = 1 card</b> %s\n<b>%d more</b> to your next card!", icon("link"), target, icon("gift"), nextRewardIn(done, target))
	}
	return text
}

// ---------- User commands ----------

func start(b *gotgbot.Bot, ctx *ext.Context) error {
	user := ctx.EffectiveUser
	args := ctx.Args()[1:]

	userArgs := ""
	if len(args) > 0 {
		userArgs = args[0]
	}

	isMember, err := fSub(b, user.Id, userArgs)
	if err != nil {
		_, _ = ctx.EffectiveMessage.Reply(b, "❌ An error occurred. Please try again later.", nil)
		return fmt.Errorf("start: %v", err)
	}
	if !isMember {
		return ext.EndGroups
	}

	return continueAfterFsub(b, ctx, userArgs)
}

// continueAfterFsub is the shared post-force-join path for /start and the
// "Joined — Try Again" callback: banned check, then the home screen for
// existing users, or the captcha gate for brand-new ones.
func continueAfterFsub(b *gotgbot.Bot, ctx *ext.Context, userArgs string) error {
	msg := ctx.EffectiveMessage
	user := ctx.EffectiveUser

	existingUser, err := getUser(user.Id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		log.Printf("Failed to fetch user: %v", err)
		_, _ = msg.Reply(b, "❌ An error occurred. Please try again later.\n/start", nil)
		return nil
	}

	if existingUser != nil {
		if existingUser.Banned {
			_, _ = msg.Reply(b, "🚫 You are banned from using this bot.", nil)
			return nil
		}
		updateUserProfile(user.Id, user.FirstName, user.Username)
		_, _ = msg.Reply(b, welcomeText(user.FirstName, existingUser, false), &gotgbot.SendMessageOpts{
			ReplyMarkup: mainKeyboard(b, user.Id),
			ParseMode:   "HTML",
			LinkPreviewOptions: &gotgbot.LinkPreviewOptions{
				IsDisabled: true,
			},
		})
		return nil
	}

	// ---- New user: human verification BEFORE registration ----
	// Channels are checked above; the captcha stops scripted join-farms.
	// The referral payload is preserved server-side until solved.
	return issueCaptcha(b, msg, user.Id, userArgs)
}

// completeRegistration runs after a NEW user passes force-join AND the
// captcha. It registers them (attaching the referral if present) and greets.
func completeRegistration(b *gotgbot.Bot, ctx *ext.Context, payload string) error {
	msg := ctx.EffectiveMessage
	user := ctx.EffectiveUser

	// Defense in depth: never register twice (e.g. replayed callback)
	if existing, err := getUser(user.Id); err == nil && existing != nil {
		_, _ = msg.Reply(b, welcomeText(user.FirstName, existing, false), &gotgbot.SendMessageOpts{
			ReplyMarkup: mainKeyboard(b, user.Id),
			ParseMode:   "HTML",
			LinkPreviewOptions: &gotgbot.LinkPreviewOptions{
				IsDisabled: true,
			},
		})
		return nil
	}

	newUser := User{
		ID:       user.Id,
		Name:     user.FirstName,
		Username: user.Username,
	}

	var referrerID int64
	if payload != "" {
		var err error
		referrerID, err = strconv.ParseInt(strings.TrimSpace(payload), 10, 64)
		if err != nil || referrerID <= 0 {
			_, _ = msg.Reply(b, "❌ <b>Invalid referral link!</b>\n\nPlease check the link and try again.", &gotgbot.SendMessageOpts{
				ParseMode: "HTML",
			})
			return nil
		}

		if _, err := getUser(referrerID); err != nil {
			_, _ = msg.Reply(b, "❌ <b>The referral link is not valid.</b>\n\nPlease check with the person who referred you.", &gotgbot.SendMessageOpts{
				ParseMode: "HTML",
			})
			return nil
		}

		if err := referUser(referrerID, newUser); err != nil {
			log.Printf("Failed to refer user: %v", err)
			_, _ = msg.Reply(b, "⚠️ <b>Failed to register with the referral. Please try again.</b>", &gotgbot.SendMessageOpts{
				ParseMode: "HTML",
			})
			return nil
		}

		// Notify the referrer about their progress
		if referrer, err := getUser(referrerID); err == nil {
			doneCount := len(referrer.ReferredUsers)
			target := ReferralTarget
			ready := unlocksAvailable(doneCount, referrer.Claims, target)
			remD := 0
			if target > 0 {
				remD = doneCount % target
			}
			notify := ""
			switch {
			case ready > 0 && target > 0 && doneCount%target == 0:
				notify = fmt.Sprintf(
					"%s <b>%s</b> joined via your link!\n\n%s Referrals: <b>%d</b>\n\n%s <b>New card unlocked!</b> You now have <b>%d reward(s) ready</b> — open the bot and tap %s Claim Reward!",
					icon("party"), esc(user.FirstName), icon("users"), doneCount, icon("trophy"), ready, icon("gift"))
			case ready > 0:
				notify = fmt.Sprintf(
					"%s <b>%s</b> joined via your link!\n\n%s Referrals: <b>%d</b>\n%s You still have <b>%d reward(s)</b> waiting — claim them anytime!\n%s <b>%d more</b> to your next card.",
					icon("party"), esc(user.FirstName), icon("users"), doneCount, icon("gift"), ready, icon("link"), nextRewardIn(doneCount, target))
			default:
				notify = fmt.Sprintf(
					"%s <b>%s</b> joined via your link!\n\n%s Referrals: <b>%d</b>  %s\n%s <b>%d more</b> to your next card %s",
					icon("party"), esc(user.FirstName), icon("users"), doneCount,
					progressBar(remD, target), icon("link"), nextRewardIn(doneCount, target), icon("gift"))
			}
			_, _ = b.SendMessage(referrerID, notify, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
		}
	}

	if referrerID == 0 {
		if err := addUser(newUser); err != nil {
			log.Printf("Failed to add user: %v", err)
			_, _ = msg.Reply(b, "❌ <b>Failed to register. Please try again later.</b>", &gotgbot.SendMessageOpts{
				ParseMode: "HTML",
			})
			return nil
		}
	}

	fresh, _ := getUser(user.Id)
	if fresh == nil {
		fresh = &User{ID: user.Id}
	}
	_, _ = msg.Reply(b, welcomeText(user.FirstName, fresh, true), &gotgbot.SendMessageOpts{
		ReplyMarkup: mainKeyboard(b, user.Id),
		ParseMode:   "HTML",
		LinkPreviewOptions: &gotgbot.LinkPreviewOptions{
			IsDisabled: true,
		},
	})

	return nil
}

func help(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	text := premiumize(fmt.Sprintf(`
<b>%s — Help</b>

<b>How it works</b>
1️⃣ Join our channels
2️⃣ Verify you're human 🤖 (one quick tap)
3️⃣ Refer friends with your link — every <b>%d referrals = 1 card</b> 🎁
4️⃣ Claim rewards &amp; keep going — <b>no limit!</b>
<i>Example: %d referrals → 1 card, %d referrals → 2 cards, %d referrals → 5 cards...</i>

<b>🔹 User Commands</b>
/start - 🚀 Start the bot & get your referral link
/progress - 📊 Check your referral progress
/info - ℹ️ Your account details

<b>🔸 Owner Commands</b>
/admin - ⚙️ Open the full admin panel
/addcard - ➕ Add reward cards (one per line, or reply to a list)
/stock - 📦 Check reward stock
/stats - 📊 Bot statistics
/backupdb - 💾 Upload & pin a database backup right now
/broadcast - 📢 Broadcast a message to all users

⚠️ <i>Owner commands are restricted to the bot owner.</i>
`, BrandName, ReferralTarget, ReferralTarget, ReferralTarget*2, ReferralTarget*5))

	keyboard := homeKeyboard()
	_, _ = msg.Reply(b, text, &gotgbot.SendMessageOpts{
		ParseMode:   "HTML",
		ReplyMarkup: keyboard,
	})

	return nil
}

func progressCmd(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	user := ctx.EffectiveUser

	u, err := getUser(user.Id)
	if err != nil {
		_, _ = msg.Reply(b, "❌ You're not registered yet. Use /start first.", nil)
		return nil
	}

	_, _ = msg.Reply(b, progressText(b, u), &gotgbot.SendMessageOpts{
		ParseMode:   "HTML",
		ReplyMarkup: mainKeyboard(b, user.Id),
		LinkPreviewOptions: &gotgbot.LinkPreviewOptions{
			IsDisabled: true,
		},
	})
	return nil
}

func progressText(b *gotgbot.Bot, u *User) string {
	done := len(u.ReferredUsers)
	target := ReferralTarget
	rem := 0
	if target > 0 {
		rem = done % target
	}
	ready := unlocksAvailable(done, u.Claims, target)
	referUrl := fmt.Sprintf("https://t.me/%s?start=%d", b.User.Username, u.ID)

	text := fmt.Sprintf(
		"%s <b>Your Progress</b>\n\n"+
			"%s <b>Referrals:</b> %d total\n"+
			"%s <b>Rewards claimed:</b> %d\n"+
			"%s <b>Next card:</b> %d/%d  %s\n",
		icon("stats"), icon("users"), done, icon("gift"), u.Claims,
		icon("next"), rem, target, progressBar(rem, target))

	if ready > 0 {
		text += fmt.Sprintf("\n%s <b>%d reward(s) unlocked!</b> Tap %s Claim Reward!\n", icon("trophy"), ready, icon("gift"))
	} else if target > 0 {
		text += fmt.Sprintf("\n%s <b>%d more referral(s)</b> to your next card — every %d referrals = 1 card!\n", icon("link"), nextRewardIn(done, target), target)
	}

	// Collected cards stay one tap away, copyable (latest 3)
	if u.Claims > 0 {
		if cards, err := getUserCards(u.ID, 3); err == nil && len(cards) > 0 {
			text += fmt.Sprintf("\n%s <b>Your latest card(s):</b>\n", icon("card"))
			for _, c := range cards {
				text += fmt.Sprintf("%s <code>%s</code>\n", icon("bullet"), esc(c.Card))
			}
		}
	}

	text += fmt.Sprintf("\n%s Keep sharing — referrals never expire!\nYour referral link:\n<code>%s</code>", icon("link"), referUrl)
	return text
}

func progressCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	query := ctx.CallbackQuery

	splitData := strings.Split(query.Data, ".")
	if len(splitData) < 2 {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "❌ Invalid callback data.",
			ShowAlert: true,
		})
		return nil
	}

	userId := stringToInt64(splitData[1])

	// Users may only view their own progress; the owner may view anyone's
	if query.From.Id != userId && !isAdmin(query.From.Id) {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "❌ This is not your progress.",
			ShowAlert: true,
		})
		return nil
	}

	u, err := getUser(userId)
	if err != nil {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "❌ User not found.",
			ShowAlert: true,
		})
		return nil
	}

	_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
		Text: "📊 Progress loaded.",
	})

	_, _, _ = msg.EditText(b, progressText(b, u), &gotgbot.EditMessageTextOpts{
		ParseMode:   "HTML",
		ReplyMarkup: mainKeyboard(b, userId),
	})

	return nil
}

func info(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	user := ctx.EffectiveUser
	args := ctx.Args()[1:]
	var userId int64

	if len(args) == 0 {
		userId = user.Id
	} else {
		userId = stringToInt64(args[0])
	}

	// Users may only view their own info; the owner may view anyone's
	if userId != user.Id && !isAdmin(user.Id) {
		_, _ = msg.Reply(b, "❌ You are not authorized to view other users' info.", nil)
		return nil
	}

	u, err := getUser(userId)
	if err != nil {
		_, _ = msg.Reply(b, "❌ <b>User not found.</b>\n\nPlease check the User ID and try again.", &gotgbot.SendMessageOpts{
			ParseMode: "HTML",
		})
		return nil
	}

	done := len(u.ReferredUsers)
	target := ReferralTarget
	rem := 0
	if target > 0 {
		rem = done % target
	}
	reward := fmt.Sprintf("<b>%d</b> claimed", u.Claims)
	if ready := unlocksAvailable(done, u.Claims, target); ready > 0 {
		reward += fmt.Sprintf(" • %s <b>%d ready!</b>", icon("trophy"), ready)
	}

	response := fmt.Sprintf(
		"%s <b>User Information</b>\n\n"+
			"%s <b>User ID:</b> <code>%d</code>\n"+
			"%s <b>Referrer:</b> <code>%d</code>\n"+
			"%s <b>Referrals:</b> %d total\n"+
			"%s <b>Next card:</b> %d/%d  %s\n"+
			"%s <b>Rewards:</b> %s",
		icon("person"), icon("iddot"), u.ID, icon("link"), u.Referrer,
		icon("users"), done, icon("next"), rem, target, progressBar(rem, target),
		icon("gift"), reward)

	_, _ = msg.Reply(b, response, &gotgbot.SendMessageOpts{
		ParseMode: "HTML",
	})

	return nil
}

// ---------- Claim flow ----------

func claim(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	query := ctx.Update.CallbackQuery
	user := ctx.EffectiveUser

	u, err := getUser(user.Id)
	if err != nil {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "❌ Please /start the bot first.",
			ShowAlert: true,
		})
		return nil
	}

	if u.Banned {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "🚫 You are banned from using this bot.",
			ShowAlert: true,
		})
		return nil
	}

	if ClaimsPaused {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "⏸️ Claims are temporarily paused. Please try again later!",
			ShowAlert: true,
		})
		return nil
	}

	// Re-verify channel membership at claim time
	isMember, err := fSub(b, user.Id, "")
	if err != nil {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "❌ An error occurred. Please try again later.",
			ShowAlert: true,
		})
		return nil
	}
	if !isMember {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "⚠️ Join our channels first!",
			ShowAlert: true,
		})
		return nil
	}

	// Repeat-reward gate: every ReferralTarget referrals = 1 new card.
	done := len(u.ReferredUsers)
	if ready := unlocksAvailable(done, u.Claims, ReferralTarget); ready <= 0 {
		need := nextRewardIn(done, ReferralTarget)
		alert := fmt.Sprintf("🔒 Locked! Refer %d more friend(s) to unlock your first card.", need)
		if u.Claims > 0 {
			alert = fmt.Sprintf("✅ All %d earned reward(s) collected! Refer %d more friend(s) for your next card.", u.Claims, need)
		}
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      alert,
			ShowAlert: true,
		})
		return nil
	}

	// Atomic claim — safe against double-taps and concurrent requests.
	// One card per unlock, each physical card issued exactly once — enforced in the DB.
	card, err := issueCard(user.Id, ReferralTarget)
	if err != nil {
		switch {
		case errors.Is(err, errNoStock):
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text:      "😔 Rewards are out of stock right now. Your unlock is saved — please check back soon!",
				ShowAlert: true,
			})
		case errors.Is(err, errNoUnlocks):
			// Lost a race with a duplicate tap — the unlock was already spent
			// by the other flow; the card is safe in the DB and viewable via My Progress.
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text:      "✅ Already processed — check 📊 My Progress to view your cards.",
				ShowAlert: true,
			})
		default:
			log.Printf("claim failed for user %d: %v", user.Id, err)
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text:      "❌ An error occurred. Please try again later.",
				ShowAlert: true,
			})
		}
		return nil
	}

	stockLeft, _ := countAvailableCards()

	// Reward number + any unlocks still waiting (fresh copy = post-commit state)
	rewardNo := 1
	remaining := 0
	if fresh, ferr := getUser(user.Id); ferr == nil {
		rewardNo = fresh.Claims
		remaining = unlocksAvailable(len(fresh.ReferredUsers), fresh.Claims, ReferralTarget)
	}

	_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
		Text: "🎉 Reward unlocked!",
	})

	// Deliver the card as a branded photo with caption
	caption := fmt.Sprintf(
		"%s <b>Congratulations, %s!</b>\n\n"+
			"%s <b>Reward #%d</b>\n"+
			"%s <b>Card:</b> <code>%s</code>\n"+
			"%s <b>Validity:</b> One-time USE only\n\n"+
			"%s <b>How to use:</b>\n%s",
		icon("party"), esc(user.FirstName),
		icon("trophy"), rewardNo,
		icon("card"), esc(card.Card),
		icon("validity"),
		icon("howto"), esc(getHowtoText()))
	if remaining > 0 {
		caption += fmt.Sprintf("\n\n%s <b>%d more reward(s) ready</b> — tap below to claim!", icon("gift"), remaining)
	}

	claimButtons := [][]gotgbot.InlineKeyboardButton{}
	if remaining > 0 {
		claimButtons = append(claimButtons, []gotgbot.InlineKeyboardButton{
			{Text: "🎁 Claim Next Reward", CallbackData: "claim"},
		})
	}
	if su := getSupportURL(); su != "" {
		claimButtons = append(claimButtons, []gotgbot.InlineKeyboardButton{
			{Text: "🆘 Support", Url: su},
		})
	}
	claimButtons = append(claimButtons, []gotgbot.InlineKeyboardButton{
		{Text: "📊 My Progress", CallbackData: fmt.Sprintf("progress.%d", user.Id)},
		{Text: "🏠 Home", CallbackData: "home"},
	})
	claimKeyboard := gotgbot.InlineKeyboardMarkup{InlineKeyboard: claimButtons}
	decorateButtons(&claimKeyboard)

	sent, err := b.SendPhoto(user.Id, cardPhotoInput(), &gotgbot.SendPhotoOpts{
		Caption:     caption,
		ParseMode:   "HTML",
		ReplyMarkup: claimKeyboard,
	})
	if err != nil {
		// Fall back to plain text; if a custom emoji was the reason the send
		// failed, the stripped caption (standard fallbacks) always delivers.
		log.Printf("photo delivery failed for user %d: %v", user.Id, err)
		if _, err2 := msg.Reply(b, stripTGEmoji(caption), &gotgbot.SendMessageOpts{
			ParseMode:   "HTML",
			ReplyMarkup: claimKeyboard,
		}); err2 != nil {
			log.Printf("plain-text fallback also failed for user %d: %v", user.Id, err2)
		}
	} else {
		cacheCardImageID(sent)
	}

	doneText := fmt.Sprintf(
		"%s <b>Reward #%d sent above!</b> 👆\n\nKeep it private — tap %s My Progress anytime to view your cards.",
		icon("gift"), rewardNo, icon("stats"))
	if remaining > 0 {
		doneText += fmt.Sprintf("\n\n%s <b>%d more reward(s) ready</b> — claim again!", icon("gift"), remaining)
	}
	_, _, _ = msg.EditText(b, doneText, &gotgbot.EditMessageTextOpts{
		ParseMode:   "HTML",
		ReplyMarkup: claimKeyboard,
	})

	notifyLogChat(b, fmt.Sprintf(
		"%s <b>Reward claimed</b>\n\n%s %s (<code>%d</code>)\n%s User reward #: <b>%d</b>\n%s Card ID: <code>%d</code>\n%s Stock left: <b>%d</b>",
		icon("gift"), icon("person"), esc(user.FirstName), user.Id,
		icon("trophy"), rewardNo, icon("id"), card.ID, icon("box"), stockLeft))

	return nil
}

func home(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	user := ctx.EffectiveUser
	query := ctx.CallbackQuery

	_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
		Text: "🔙 Back to Main Menu",
	})

	existingUser, err := getUser(user.Id)
	if err != nil || existingUser == nil {
		// User has never started the bot — don't crash, show an empty profile
		existingUser = &User{ID: user.Id}
	}

	_, _, _ = msg.EditText(b, welcomeText(user.FirstName, existingUser, false), &gotgbot.EditMessageTextOpts{
		ParseMode:   "HTML",
		ReplyMarkup: mainKeyboard(b, user.Id),
	})

	return nil
}

// ---------- Owner commands ----------

func addCard(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	user := ctx.EffectiveUser
	if !isAdmin(user.Id) {
		_, _ = msg.Reply(b, "❌ You are not authorized to use this command.", nil)
		return nil
	}

	// Cards come either from the replied-to message or from text after /addcard.
	// One card per line.
	raw := ""
	if reply := msg.ReplyToMessage; reply != nil && strings.TrimSpace(reply.Text) != "" {
		raw = reply.Text
	} else if text := msg.GetText(); text != "" {
		if idx := strings.Index(text, " "); idx >= 0 {
			raw = strings.TrimSpace(text[idx+1:])
		}
	}

	if raw == "" {
		_, _ = msg.Reply(b,
			"❌ No cards provided.\n\n"+
				"<b>Usage:</b>\n"+
				"• <code>/addcard CARD-ONE\nCARD-TWO</code> (one per line)\n"+
				"• Reply to a message containing the cards with <code>/addcard</code>",
			&gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return nil
	}

	added, skipped, err := addCards(strings.Split(raw, "\n"))
	if err != nil {
		_, _ = msg.Reply(b, "❌ Failed to add cards: "+CustomError(err).Error(), nil)
		return nil
	}

	total, _ := countAvailableCards()
	_, _ = msg.Reply(b, fmt.Sprintf(
		"✅ <b>Added %d card(s).</b>\n⏭️ Skipped (duplicates/empty): %d\n📦 <b>Stock available:</b> %d",
		added, skipped, total),
		&gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return nil
}

func stock(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	user := ctx.EffectiveUser
	if !isAdmin(user.Id) {
		_, _ = msg.Reply(b, "❌ You are not authorized to use this command.", nil)
		return nil
	}

	available, err1 := countAvailableCards()
	claimed, err2 := countClaimedCards()
	if err1 != nil || err2 != nil {
		_, _ = msg.Reply(b, "❌ Failed to fetch stock information.", nil)
		return nil
	}

	_, _ = msg.Reply(b, premiumize(fmt.Sprintf(
		"📦 <b>Reward Stock</b>\n\n✅ Available: <b>%d</b>\n🎁 Claimed: <b>%d</b>",
		available, claimed)),
		&gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return nil
}

func stats(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	user := ctx.EffectiveUser
	if !isAdmin(user.Id) {
		_, _ = msg.Reply(b, "❌ You are not authorized to use this command.", nil)
		return nil
	}

	allUsers, err := getAllUsers()
	if err != nil {
		_, _ = msg.Reply(b, "❌ Failed to fetch statistics: "+CustomError(err).Error(), nil)
		return nil
	}
	claimedUsers, _ := countClaimedUsers()
	available, _ := countAvailableCards()
	claimed, _ := countClaimedCards()

	text := premiumize(fmt.Sprintf(
		"📊 <b>Bot Statistics</b>\n\n"+
			"👥 Total Users: <b>%d</b>\n"+
			"🎁 Users Claimed: <b>%d</b>\n"+
			"📦 Cards Available: <b>%d</b>\n"+
			"✅ Cards Claimed: <b>%d</b>",
		len(allUsers), claimedUsers, available, claimed))
	_, _ = msg.Reply(b, text, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return nil
}

// backupCmd forces an immediate DB backup into the LOGGER_ID chat.
func backupCmd(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	user := ctx.EffectiveUser
	if !isAdmin(user.Id) {
		_, _ = msg.Reply(b, "❌ You are not authorized to use this command.", nil)
		return nil
	}
	if backupChatID == 0 {
		_, _ = msg.Reply(b,
			"⚠️ <b>LOGGER_ID is not set.</b>\n\nBackups are uploaded to the LOGGER_ID chat. Set it in your environment variables, redeploy, then run /backupdb again.",
			&gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return nil
	}
	_, _ = msg.Reply(b, "⏳ Creating a backup...", nil)
	if err := runDBBackup(b, backupChatID, "manual"); err != nil {
		_, _ = msg.Reply(b, "❌ Backup failed: "+CustomError(err).Error(), nil)
		return nil
	}
	_, _ = msg.Reply(b,
		"✅ <b>Backup uploaded & pinned!</b>\n\nThe database will restore automatically on the next redeploy. 🔄",
		&gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return nil
}

func broadcast(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	if msg.Chat.Type != "private" {
		return nil
	}

	if !isAdmin(msg.From.Id) {
		_, _ = msg.Reply(b, "❌ You must be the owner to use this command.", nil)
		return nil
	}

	reply := ctx.EffectiveMessage.ReplyToMessage
	if reply == nil {
		_, err := ctx.EffectiveMessage.Reply(b, "❌ <b>Reply to a message to broadcast</b>", &gotgbot.SendMessageOpts{ParseMode: "HTML"})
		if err != nil {
			return fmt.Errorf("error while replying to user: %v", err)
		}
		return ext.EndGroups
	}

	button := &gotgbot.InlineKeyboardMarkup{}
	if reply.ReplyMarkup != nil {
		button.InlineKeyboard = reply.ReplyMarkup.InlineKeyboard
	}
	decorateButtons(button) // re-broadcasts get the premium look too

	users, err := getAllUsers()
	if err != nil {
		_, _ = msg.Reply(b, "Error getting users.\n\n"+CustomError(err).Error(), nil)
		return err
	}

	successfulBroadcasts := 0
	for _, u := range users {
		userId := u.ID
		_, err = b.CopyMessage(userId, ctx.EffectiveMessage.Chat.Id, reply.MessageId, &gotgbot.CopyMessageOpts{ReplyMarkup: button})

		if err == nil {
			successfulBroadcasts++
		}

		time.Sleep(33 * time.Millisecond)
	}

	_, err = ctx.EffectiveMessage.Reply(b, fmt.Sprintf("✅ <b>Broadcast successfully to %d users</b>", successfulBroadcasts), &gotgbot.SendMessageOpts{ParseMode: "HTML"})
	if err != nil {
		return err
	}

	return nil
}
