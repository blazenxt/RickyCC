package main

import (
	"context"
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
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/callbackquery"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	_ "github.com/joho/godotenv/autoload"
)

const (
	// BrandName is displayed in bot messages.
	BrandName = "⚕️ PREMIUM CARD"
	// ReferralTarget is how many friends a user must refer to unlock a reward.
	ReferralTarget = 5
)

var (
	WebhookURL     string
	Port           string
	MongoDBURI     string
	secretToken    string
	OwnerID        int64
	LoggerID       int64
	FSubIds        []int64
	ctx            = context.TODO()
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

	LoggerID, err = strconv.ParseInt(os.Getenv("LOGGER_ID"), 10, 64)
	if err != nil {
		log.Fatal("LOGGER_ID is not set")
	}

	// FSUB_IDS supports multiple comma-separated channel IDs (e.g. -100xxx,-100yyy)
	fsubEnv := strings.TrimSpace(os.Getenv("FSUB_IDS"))
	for _, part := range strings.Split(fsubEnv, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			log.Fatalf("invalid FSUB_IDS entry %q: %v", part, err)
		}
		FSubIds = append(FSubIds, id)
	}

	MongoDBURI = os.Getenv("MONGO_URI")

	secretToken = os.Getenv("SECRET_TOKEN")
	if secretToken == "" {
		secretToken = "OopsNoSECRET_TOKENFoundTimeToCallSherlock"
	}

	WebhookURL = os.Getenv("WEBHOOK_URL")
	Port = os.Getenv("PORT")

	clientOptions := options.Client().ApplyURI(MongoDBURI)
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("Failed to ping MongoDB: %v", err)
	}

	fmt.Println("Connected to MongoDB")
	db := client.Database("premiumcard")
	userColl = db.Collection("users")
	codeColl = db.Collection("codes")

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
	dispatcher.AddHandler(handlers.NewCommand("addcode", addCode))
	dispatcher.AddHandler(handlers.NewCommand("stock", stock))
	dispatcher.AddHandler(handlers.NewCommand("stats", stats))
	dispatcher.AddHandler(handlers.NewCommand("broadcast", broadcast))

	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("progress"), progressCallback))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("claim"), claim))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("home"), home))

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
	}

	log.Printf("%s has been started...\n", bot.User.Username)
	updater.Idle()
}

// ---------- UI helpers ----------

// progressBar renders a 🟩/⬜ bar of length `target`.
func progressBar(done, target int) string {
	if done > target {
		done = target
	}
	if done < 0 {
		done = 0
	}
	return strings.Repeat("🟩", done) + strings.Repeat("⬜", target-done)
}

// mainKeyboard is the primary inline keyboard shown on /start and home.
func mainKeyboard(b *gotgbot.Bot, userId int64) gotgbot.InlineKeyboardMarkup {
	referUrl := fmt.Sprintf("https://t.me/%s?start=%d", b.User.Username, userId)
	return gotgbot.InlineKeyboardMarkup{
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
}

func homeKeyboard() gotgbot.InlineKeyboardMarkup {
	return gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{
				{
					Text:         "🏠 Home",
					CallbackData: "home",
				},
			},
		},
	}
}

func welcomeText(firstName string, u *User, isNew bool) string {
	done := len(u.ReferredUsers)
	greeting := "👋 <b>Welcome back"
	if isNew {
		greeting = "🎉 <b>Welcome"
	}
	text := fmt.Sprintf(
		"%s to %s, %s!</b>\n\n"+
			"👥 <b>Referrals:</b> %d/%d  %s\n\n",
		greeting, BrandName, firstName, done, ReferralTarget, progressBar(done, ReferralTarget))

	if done >= ReferralTarget {
		if u.HasClaimed {
			text += "✅ You have already claimed your reward."
		} else {
			text += "🏆 <b>Target complete!</b> Tap 🎁 Claim Reward below!"
		}
	} else {
		text += fmt.Sprintf("🔗 Share your referral link — <b>%d more</b> to unlock your Premium Card!", ReferralTarget-done)
	}
	return text
}

// ---------- User commands ----------

func start(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	user := ctx.EffectiveUser
	args := ctx.Args()[1:]

	userArgs := ""
	if len(args) > 0 {
		userArgs = args[0]
	}

	isMember, err := fSub(b, user.Id, userArgs)
	if err != nil {
		_, _ = msg.Reply(b, "❌ An error occurred. Please try again later.", nil)
		return fmt.Errorf("start: %v", err)
	}
	if !isMember {
		return ext.EndGroups
	}

	existingUser, err := getUser(user.Id)
	if err != nil && err.Error() != "mongo: no documents in result" {
		log.Printf("Failed to fetch user: %v", err)
		_, _ = msg.Reply(b, "❌ An error occurred. Please try again later.\n/start", nil)
		return nil
	}

	if existingUser != nil {
		_, _ = msg.Reply(b, welcomeText(user.FirstName, existingUser, false), &gotgbot.SendMessageOpts{
			ReplyMarkup: mainKeyboard(b, user.Id),
			ParseMode:   "HTML",
			LinkPreviewOptions: &gotgbot.LinkPreviewOptions{
				IsDisabled: true,
			},
		})
		return nil
	}

	// ---- New user registration ----
	var referrerID int64
	if len(args) > 0 {
		referrerID, err = strconv.ParseInt(strings.TrimSpace(args[0]), 10, 64)
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

		if err := referUser(referrerID, user.Id); err != nil {
			log.Printf("Failed to refer user: %v", err)
			_, _ = msg.Reply(b, "⚠️ <b>Failed to register with the referral. Please try again.</b>", &gotgbot.SendMessageOpts{
				ParseMode: "HTML",
			})
			return nil
		}

		// Notify the referrer about their progress
		if referrer, err := getUser(referrerID); err == nil {
			doneCount := len(referrer.ReferredUsers)
			notify := ""
			if doneCount >= ReferralTarget {
				notify = fmt.Sprintf(
					"🎉 <b>%s</b> joined via your link!\n\n👥 Referrals: <b>%d/%d</b> %s\n\n🏆 <b>Target complete!</b> Open the bot and tap 🎁 Claim Reward!",
					user.FirstName, doneCount, ReferralTarget, progressBar(doneCount, ReferralTarget))
			} else {
				notify = fmt.Sprintf(
					"🎉 <b>%s</b> joined via your link!\n\n👥 Referrals: <b>%d/%d</b> %s\n🔗 <b>%d more</b> to unlock your reward!",
					user.FirstName, doneCount, ReferralTarget, progressBar(doneCount, ReferralTarget), ReferralTarget-doneCount)
			}
			_, _ = b.SendMessage(referrerID, notify, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
		}
	}

	if referrerID == 0 {
		if err := addUser(User{ID: user.Id, Referrer: 0}); err != nil {
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
	text := fmt.Sprintf(`
<b>%s — Help</b>

<b>How it works</b>
1️⃣ Join our channels
2️⃣ Refer <b>%d friends</b> with your link
3️⃣ Claim your reward 🎁

<b>🔹 User Commands</b>
/start - 🚀 Start the bot & get your referral link
/progress - 📊 Check your referral progress
/info - ℹ️ Your account details

<b>🔸 Owner Commands</b>
/addcode - ➕ Add reward codes (one per line, or reply to a list)
/stock - 📦 Check reward stock
/stats - 📊 Bot statistics
/broadcast - 📢 Broadcast a message to all users

⚠️ <i>Owner commands are restricted to the bot owner.</i>
`, BrandName, ReferralTarget)

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
	text := fmt.Sprintf(
		"📊 <b>Your Progress</b>\n\n"+
			"👥 <b>Referrals:</b> %d/%d  %s\n\n",
		done, ReferralTarget, progressBar(done, ReferralTarget))

	if done >= ReferralTarget {
		if u.HasClaimed {
			text += "✅ Reward already claimed."
		} else {
			text += "🏆 <b>Target complete!</b> Tap 🎁 Claim Reward!"
		}
	} else {
		referUrl := fmt.Sprintf("https://t.me/%s?start=%d", b.User.Username, u.ID)
		text += fmt.Sprintf("🔗 <b>%d more</b> to unlock your reward.\n\nYour referral link:\n<code>%s</code>", ReferralTarget-done, referUrl)
	}
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
	if query.From.Id != userId && query.From.Id != OwnerID {
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
	if userId != user.Id && user.Id != OwnerID {
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

	claimStatus := "❌ Not claimed"
	if u.HasClaimed {
		claimStatus = "✅ Claimed"
	}
	done := len(u.ReferredUsers)

	response := fmt.Sprintf(
		"👤 <b>User Information</b>\n\n"+
			"🔹 <b>User ID:</b> <code>%d</code>\n"+
			"🔗 <b>Referrer:</b> <code>%d</code>\n"+
			"👥 <b>Referrals:</b> %d/%d  %s\n"+
			"🎁 <b>Reward:</b> %s",
		u.ID, u.Referrer, done, ReferralTarget, progressBar(done, ReferralTarget), claimStatus)

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

	if u.HasClaimed {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "✅ You already claimed your reward.",
			ShowAlert: true,
		})
		_, _, _ = msg.EditText(b, fmt.Sprintf(
			"🎁 <b>Your reward:</b>\n\n<code>%s</code>", u.ClaimedCode),
			&gotgbot.EditMessageTextOpts{
				ParseMode:   "HTML",
				ReplyMarkup: homeKeyboard(),
			})
		return nil
	}

	done := len(u.ReferredUsers)
	if done < ReferralTarget {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      fmt.Sprintf("🔒 Locked! Refer %d more friend(s) to unlock (%d/%d).", ReferralTarget-done, done, ReferralTarget),
			ShowAlert: true,
		})
		return nil
	}

	// Atomic claim — safe against double-taps and concurrent requests
	code, err := claimCodeAtomic(user.Id)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text:      "😔 Rewards are out of stock right now. Please check back soon!",
				ShowAlert: true,
			})
			return nil
		}
		log.Printf("claim failed for user %d: %v", user.Id, err)
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "❌ An error occurred. Please try again later.",
			ShowAlert: true,
		})
		return nil
	}

	if err := markUserClaimed(user.Id, code.Code); err != nil {
		// The code is already assigned; alert the admin instead of failing the user
		log.Printf("CRITICAL: user %d claimed code %s but HasClaimed flag update failed: %v", user.Id, code.ID.Hex(), err)
		_, _ = b.SendMessage(LoggerID, fmt.Sprintf(
			"⚠️ User <code>%d</code> claimed code %s but flag update failed. Please verify manually.",
			user.Id, code.ID.Hex()), &gotgbot.SendMessageOpts{ParseMode: "HTML"})
	}

	stockLeft, _ := countAvailableCodes()

	_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
		Text: "🎉 Reward unlocked!",
	})

	_, _, _ = msg.EditText(b, fmt.Sprintf(
		"🎉 <b>Congratulations, %s!</b>\n\n"+
			"Here is your <b>Premium Card</b>:\n\n"+
			"<code>%s</code>\n\n"+
			"⚠️ <i>Keep it private. Tap 📊 My Progress if you need to see it again.</i>",
		user.FirstName, code.Code),
		&gotgbot.EditMessageTextOpts{
			ParseMode: "HTML",
			ReplyMarkup: gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					{
						{Text: "📊 My Progress", CallbackData: fmt.Sprintf("progress.%d", user.Id)},
						{Text: "🏠 Home", CallbackData: "home"},
					},
				},
			},
		})

	_, _ = b.SendMessage(LoggerID, fmt.Sprintf(
		"🎁 <b>Reward claimed</b>\n\n👤 %s (<code>%d</code>)\n🆔 Code ID: <code>%s</code>\n📦 Stock left: <b>%d</b>",
		user.FirstName, user.Id, code.ID.Hex(), stockLeft),
		&gotgbot.SendMessageOpts{ParseMode: "HTML"})

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

func addCode(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	user := ctx.EffectiveUser
	if user.Id != OwnerID {
		_, _ = msg.Reply(b, "❌ You are not authorized to use this command.", nil)
		return nil
	}

	// Codes come either from the replied-to message or from text after /addcode.
	// One code per line.
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
			"❌ No codes provided.\n\n"+
				"<b>Usage:</b>\n"+
				"• <code>/addcode CODE-ONE\nCODE-TWO</code> (one per line)\n"+
				"• Reply to a message containing the codes with <code>/addcode</code>",
			&gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return nil
	}

	added, skipped, err := addCodes(strings.Split(raw, "\n"))
	if err != nil {
		_, _ = msg.Reply(b, "❌ Failed to add codes: "+CustomError(err).Error(), nil)
		return nil
	}

	total, _ := countAvailableCodes()
	_, _ = msg.Reply(b, fmt.Sprintf(
		"✅ <b>Added %d code(s).</b>\n⏭️ Skipped (duplicates/empty): %d\n📦 <b>Stock available:</b> %d",
		added, skipped, total),
		&gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return nil
}

func stock(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	user := ctx.EffectiveUser
	if user.Id != OwnerID {
		_, _ = msg.Reply(b, "❌ You are not authorized to use this command.", nil)
		return nil
	}

	available, err1 := countAvailableCodes()
	claimed, err2 := countClaimedCodes()
	if err1 != nil || err2 != nil {
		_, _ = msg.Reply(b, "❌ Failed to fetch stock information.", nil)
		return nil
	}

	_, _ = msg.Reply(b, fmt.Sprintf(
		"📦 <b>Reward Stock</b>\n\n✅ Available: <b>%d</b>\n🎁 Claimed: <b>%d</b>",
		available, claimed),
		&gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return nil
}

func stats(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	user := ctx.EffectiveUser
	if user.Id != OwnerID {
		_, _ = msg.Reply(b, "❌ You are not authorized to use this command.", nil)
		return nil
	}

	allUsers, err := getAllUsers()
	if err != nil {
		_, _ = msg.Reply(b, "❌ Failed to fetch statistics: "+CustomError(err).Error(), nil)
		return nil
	}
	claimedUsers, _ := countClaimedUsers()
	available, _ := countAvailableCodes()
	claimed, _ := countClaimedCodes()

	text := fmt.Sprintf(
		"📊 <b>Bot Statistics</b>\n\n"+
			"👥 Total Users: <b>%d</b>\n"+
			"🎁 Users Claimed: <b>%d</b>\n"+
			"📦 Codes Available: <b>%d</b>\n"+
			"✅ Codes Claimed: <b>%d</b>",
		len(allUsers), claimedUsers, available, claimed)
	_, _ = msg.Reply(b, text, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return nil
}

func broadcast(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	if msg.Chat.Type != "private" {
		return nil
	}

	if msg.From.Id != OwnerID {
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
