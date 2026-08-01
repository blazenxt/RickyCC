package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
)

// Conversation states for the admin panel
const (
	admStateFindUser = "ADM_FIND_USER"
	admStateAddCodes = "ADM_ADD_CODES"
)

const admTimeFmt = "02 Jan 06 15:04"

func isOwner(id int64) bool { return id == OwnerID }

func admBtn(text, data string) gotgbot.InlineKeyboardButton {
	return gotgbot.InlineKeyboardButton{Text: text, CallbackData: data}
}

func admBackBtn() []gotgbot.InlineKeyboardButton {
	return []gotgbot.InlineKeyboardButton{admBtn("🔙 Back to Panel", "admp.home")}
}

// ---------- Panel entry ----------

// adminCmd handles /admin — owner only.
func adminCmd(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	if !isOwner(ctx.EffectiveUser.Id) {
		_, _ = msg.Reply(b, "❌ You are not authorized to use this command.", nil)
		return nil
	}

	_, err := msg.Reply(b, admPanelText(), &gotgbot.SendMessageOpts{
		ParseMode:   "HTML",
		ReplyMarkup: admPanelKeyboard(),
	})
	return err
}

func admPanelText() string {
	return fmt.Sprintf("⚙️ <b>ADMIN PANEL</b> — %s\n\nSelect a section:", BrandName)
}

func admPanelKeyboard() gotgbot.InlineKeyboardMarkup {
	return gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{admBtn("📊 Dashboard", "admp.dash")},
			{admBtn("👥 Users", "admp.users"), admBtn("🎟️ Codes", "admp.codes")},
			{admBtn("📢 Broadcast", "admp.bcast")},
			{admBtn("❌ Close Panel", "admp.close")},
		},
	}
}

// admEdit is a small helper to swap the panel view.
func admEdit(b *gotgbot.Bot, msg *gotgbot.Message, text string, kb gotgbot.InlineKeyboardMarkup) {
	_, _, err := msg.EditText(b, text, &gotgbot.EditMessageTextOpts{
		ParseMode:   "HTML",
		ReplyMarkup: kb,
	})
	if err != nil {
		log.Printf("admin panel edit failed: %v", err)
	}
}

// ---------- Panel router (admp.*) ----------

func adminCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	query := ctx.CallbackQuery

	if !isOwner(query.From.Id) {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "❌ This panel is for the bot owner only.",
			ShowAlert: true,
		})
		return nil
	}

	switch strings.TrimPrefix(query.Data, "admp.") {
	case "home":
		_, _ = query.Answer(b, nil)
		admEdit(b, msg, admPanelText(), admPanelKeyboard())

	case "dash":
		_, _ = query.Answer(b, nil)
		total, _ := countAllUsers()
		banned, _ := countBannedUsers()
		claimedU, _ := countClaimedUsers()
		avail, _ := countAvailableCodes()
		claimedC, _ := countClaimedCodes()

		admEdit(b, msg, fmt.Sprintf(
			"📊 <b>Dashboard</b>\n\n"+
				"👥 Total users: <b>%d</b>\n"+
				"🚫 Banned: <b>%d</b>\n"+
				"🎁 Users claimed: <b>%d</b>\n\n"+
				"🎟️ Codes available: <b>%d</b>\n"+
				"✅ Codes claimed: <b>%d</b>\n\n"+
				"🕒 <i>Refreshed %s</i>",
			total, banned, claimedU, avail, claimedC, time.Now().Format("15:04:05")),
			gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					{admBtn("🔄 Refresh", "admp.dash")},
					admBackBtn(),
				},
			})

	case "users":
		_, _ = query.Answer(b, nil)
		total, _ := countAllUsers()
		banned, _ := countBannedUsers()
		claimedU, _ := countClaimedUsers()

		admEdit(b, msg, fmt.Sprintf(
			"👥 <b>User Management</b>\n\n"+
				"Total: <b>%d</b>  ·  🚫 Banned: <b>%d</b>  ·  🎁 Claimed: <b>%d</b>",
			total, banned, claimedU),
			gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					{admBtn("🔍 Find User", "admc.finduser")},
					{admBtn("🆕 Recent Users", "admp.recent")},
					admBackBtn(),
				},
			})

	case "recent":
		_, _ = query.Answer(b, nil)
		users, err := getRecentUsers(10)
		if err != nil {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text: "❌ " + CustomError(err).Error(), ShowAlert: true})
			return nil
		}

		var sb strings.Builder
		sb.WriteString("🆕 <b>Newest users</b>\n\n")
		if len(users) == 0 {
			sb.WriteString("<i>No users yet.</i>")
		}
		for _, u := range users {
			name := esc(u.Name)
			if name == "" {
				name = "—"
			}
			flags := ""
			if u.HasClaimed {
				flags += " 🎁"
			}
			if u.Banned {
				flags += " 🚫"
			}
			fmt.Fprintf(&sb, "• %s — <code>%d</code> — %d/%d%s\n",
				name, u.ID, len(u.ReferredUsers), ReferralTarget, flags)
		}
		sb.WriteString("\n🎁 claimed · 🚫 banned — use 🔍 Find User for actions")

		admEdit(b, msg, sb.String(), gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{admBtn("🔍 Find User", "admc.finduser")},
				{admBtn("🔙 Users", "admp.users")},
			},
		})

	case "codes":
		avail, _ := countAvailableCodes()
		claimedC, _ := countClaimedCodes()
		_, _ = query.Answer(b, nil)

		admEdit(b, msg, fmt.Sprintf(
			"🎟️ <b>Code Stock</b>\n\n"+
				"✅ Available: <b>%d</b>\n"+
				"🎁 Claimed: <b>%d</b>\n"+
				"📦 Total: <b>%d</b>",
			avail, claimedC, avail+claimedC),
			gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					{admBtn("➕ Add Codes", "admc.addcodes")},
					{admBtn("🧾 Recent Claims", "admp.claims")},
					{admBtn("🧹 Clear Claimed", "admp.clear")},
					admBackBtn(),
				},
			})

	case "claims":
		_, _ = query.Answer(b, nil)
		claims, err := getRecentClaims(10)
		if err != nil {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text: "❌ " + CustomError(err).Error(), ShowAlert: true})
			return nil
		}

		var sb strings.Builder
		sb.WriteString("🧾 <b>Latest claimed codes</b>\n\n")
		if len(claims) == 0 {
			sb.WriteString("<i>Nothing claimed yet.</i>")
		}
		for _, c := range claims {
			when := "—"
			if c.ClaimedAt != nil {
				when = c.ClaimedAt.Format(admTimeFmt)
			}
			fmt.Fprintf(&sb, "• <code>%s</code>\n  → <code>%d</code> · %s\n",
				esc(truncate(c.Code, 30)), c.ClaimedBy, when)
		}

		admEdit(b, msg, sb.String(), gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{admBtn("🔙 Codes", "admp.codes")},
			},
		})

	case "clear":
		claimedC, _ := countClaimedCodes()
		_, _ = query.Answer(b, nil)
		admEdit(b, msg, fmt.Sprintf(
			"🧹 <b>Clear claimed codes?</b>\n\n"+
				"This will permanently delete <b>%d</b> claimed code record(s).\n"+
				"<i>Claim history will be lost.</i>",
			claimedC),
			gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					{admBtn("✅ Yes, delete them", "admp.clearok")},
					{admBtn("🔙 Cancel", "admp.codes")},
				},
			})

	case "clearok":
		deleted, err := clearClaimedCodes()
		if err != nil {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text: "❌ " + CustomError(err).Error(), ShowAlert: true})
			return nil
		}
		log.Printf("admin cleared %d claimed codes", deleted)
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: fmt.Sprintf("🧹 Deleted %d record(s).", deleted)})
		admEdit(b, msg, fmt.Sprintf("🧹 Deleted <b>%d</b> claimed code record(s).", deleted),
			gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					{admBtn("🔙 Codes", "admp.codes")},
				},
			})

	case "bcast":
		total, _ := countAllUsers()
		_, _ = query.Answer(b, nil)
		admEdit(b, msg, fmt.Sprintf(
			"📢 <b>Broadcast</b>\n\n"+
				"Reply to any message with <code>/broadcast</code> to send it to all <b>%d</b> users.\n\n"+
				"Works with text, photos, videos, buttons — anything you can reply to.",
			total),
			gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					admBackBtn(),
				},
			})

	case "close":
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "✅ Panel closed."})
		_, _, _ = msg.EditText(b, "✅ <b>Admin panel closed.</b> Reopen with /admin",
			&gotgbot.EditMessageTextOpts{ParseMode: "HTML"})

	default:
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: "❌ Unknown action.", ShowAlert: true})
	}

	return nil
}

// ---------- User actions (admu.<action>.<id>) ----------

func adminUserCardView(u *User) (string, gotgbot.InlineKeyboardMarkup) {
	claim := "❌ Not claimed"
	if u.HasClaimed {
		claim = fmt.Sprintf("✅ Claimed:\n<code>%s</code>", esc(truncate(u.ClaimedCode, 50)))
	}
	status := "✅ Active"
	if u.Banned {
		status = "🚫 Banned"
	}
	name := esc(u.Name)
	if name == "" {
		name = "—"
	}
	uname := ""
	if u.Username != "" {
		uname = "(@" + esc(u.Username) + ")"
	}
	joined := "—"
	if !u.JoinedAt.IsZero() {
		joined = u.JoinedAt.Format(admTimeFmt)
	}

	text := fmt.Sprintf(
		"👤 <b>User Card</b>\n\n"+
			"🆔 ID: <code>%d</code>\n"+
			"📛 Name: %s %s\n"+
			"🔗 Referrer: <code>%d</code>\n"+
			"👥 Referrals: <b>%d/%d</b>\n"+
			"🎁 Reward: %s\n"+
			"📅 Joined: %s\n"+
			"🔰 Status: <b>%s</b>",
		u.ID, name, uname, u.Referrer,
		len(u.ReferredUsers), ReferralTarget,
		claim, joined, status)

	banBtn := admBtn("🚫 Ban", fmt.Sprintf("admu.ban.%d", u.ID))
	if u.Banned {
		banBtn = admBtn("✅ Unban", fmt.Sprintf("admu.unban.%d", u.ID))
	}

	kb := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{banBtn, admBtn("🔄 Reset Claim", fmt.Sprintf("admu.reset.%d", u.ID))},
			{admBtn("🗑️ Delete User", fmt.Sprintf("admu.del.%d", u.ID))},
			{admBtn("🔙 Users", "admp.users")},
		},
	}
	return text, kb
}

func adminUserCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	query := ctx.CallbackQuery

	if !isOwner(query.From.Id) {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "❌ This panel is for the bot owner only.",
			ShowAlert: true,
		})
		return nil
	}

	parts := strings.Split(query.Data, ".")
	if len(parts) != 3 {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: "❌ Invalid callback data.", ShowAlert: true})
		return nil
	}
	action := parts[1]
	uid := stringToInt64(parts[2])
	if uid <= 0 {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: "❌ Invalid user ID.", ShowAlert: true})
		return nil
	}

	switch action {
	case "ban":
		if uid == OwnerID {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text: "❌ You can't ban yourself.", ShowAlert: true})
			return nil
		}
		if err := setUserBanned(uid, true); err != nil {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text: "❌ " + CustomError(err).Error(), ShowAlert: true})
			return nil
		}
		log.Printf("admin banned user %d", uid)
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "🚫 User banned."})

	case "unban":
		if err := setUserBanned(uid, false); err != nil {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text: "❌ " + CustomError(err).Error(), ShowAlert: true})
			return nil
		}
		log.Printf("admin unbanned user %d", uid)
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "✅ User unbanned."})

	case "reset":
		if err := resetUserClaim(uid); err != nil {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text: "❌ " + CustomError(err).Error(), ShowAlert: true})
			return nil
		}
		log.Printf("admin reset claim for user %d", uid)
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "🔄 Claim reset — user can claim again."})

	case "del":
		// Confirmation step
		u, err := getUser(uid)
		if err != nil {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text: "❌ User not found.", ShowAlert: true})
			return nil
		}
		_, _ = query.Answer(b, nil)
		admEdit(b, msg, fmt.Sprintf(
			"🗑️ <b>Delete user?</b>\n\n"+
				"🆔 <code>%d</code> — %s\n"+
				"👥 Referrals: %d\n\n"+
				"<i>This removes the user from the database (and from their referrer's list). They can re-register via /start.</i>",
			u.ID, esc(u.Name), len(u.ReferredUsers)),
			gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					{admBtn("✅ Yes, delete", fmt.Sprintf("admu.delok.%d", u.ID))},
					{admBtn("🔙 Cancel", "admp.users")},
				},
			})
		return nil

	case "delok":
		if err := deleteUser(uid); err != nil {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text: "❌ " + CustomError(err).Error(), ShowAlert: true})
			return nil
		}
		log.Printf("admin deleted user %d", uid)
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "🗑️ User deleted."})
		admEdit(b, msg, fmt.Sprintf("🗑️ User <code>%d</code> deleted.", uid),
			gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					{admBtn("🔙 Users", "admp.users")},
				},
			})
		return nil

	default:
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: "❌ Unknown action.", ShowAlert: true})
		return nil
	}

	// Refresh the user card after ban/unban/reset
	u, err := getUser(uid)
	if err != nil {
		admEdit(b, msg, "❌ User no longer exists.",
			gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					{admBtn("🔙 Users", "admp.users")},
				},
			})
		return nil
	}
	text, kb := adminUserCardView(u)
	admEdit(b, msg, text, kb)
	return nil
}

// ---------- Conversations: find user & add codes ----------

// adminFindUserStart is the entry point for the find-user conversation.
func adminFindUserStart(b *gotgbot.Bot, ctx *ext.Context) error {
	query := ctx.CallbackQuery
	if !isOwner(query.From.Id) {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "❌ Owner only.",
			ShowAlert: true,
		})
		return handlers.EndConversation()
	}

	_, _ = query.Answer(b, nil)
	_, _, _ = ctx.EffectiveMessage.EditText(b,
		"🔍 <b>Find User</b>\n\nSend the user's <b>Telegram ID</b> now.\n\n/cancel to abort.",
		&gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
	return handlers.NextConversationState(admStateFindUser)
}

func adminFindUserMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	if msg.From == nil || !isOwner(msg.From.Id) {
		return handlers.EndConversation()
	}

	uid := stringToInt64(strings.TrimSpace(msg.GetText()))
	if uid <= 0 {
		_, _ = msg.Reply(b, "❌ Invalid ID. Send a numeric Telegram user ID, or /cancel.", nil)
		return nil // stay in the conversation
	}

	u, err := getUser(uid)
	if err != nil {
		_, _ = msg.Reply(b, "❌ User not found in the database. Try another ID, or /cancel.", nil)
		return nil
	}

	text, kb := adminUserCardView(u)
	_, _ = msg.Reply(b, text, &gotgbot.SendMessageOpts{
		ParseMode:   "HTML",
		ReplyMarkup: kb,
	})
	return handlers.EndConversation()
}

// adminAddCodesStart is the entry point for the add-codes conversation.
func adminAddCodesStart(b *gotgbot.Bot, ctx *ext.Context) error {
	query := ctx.CallbackQuery
	if !isOwner(query.From.Id) {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "❌ Owner only.",
			ShowAlert: true,
		})
		return handlers.EndConversation()
	}

	avail, _ := countAvailableCodes()
	_, _ = query.Answer(b, nil)
	_, _, _ = ctx.EffectiveMessage.EditText(b, fmt.Sprintf(
		"➕ <b>Add Codes</b>\n\n"+
			"Send the codes now — <b>one per line</b>. Duplicates are skipped automatically.\n\n"+
			"📦 Current stock: <b>%d</b>\n\n"+
			"/cancel to abort.",
		avail),
		&gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
	return handlers.NextConversationState(admStateAddCodes)
}

func adminAddCodesMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	if msg.From == nil || !isOwner(msg.From.Id) {
		return handlers.EndConversation()
	}

	added, skipped, err := addCodes(strings.Split(msg.GetText(), "\n"))
	if err != nil {
		_, _ = msg.Reply(b, "❌ Failed to add codes: "+CustomError(err).Error(), nil)
		return handlers.EndConversation()
	}

	log.Printf("admin added %d codes (%d skipped)", added, skipped)
	total, _ := countAvailableCodes()
	_, _ = msg.Reply(b, fmt.Sprintf(
		"✅ <b>Added %d code(s).</b>\n⏭️ Skipped (duplicates/empty): %d\n📦 <b>Stock available:</b> %d",
		added, skipped, total),
		&gotgbot.SendMessageOpts{
			ParseMode: "HTML",
			ReplyMarkup: gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					{admBtn("➕ Add More", "admc.addcodes")},
					{admBtn("⚙️ Panel", "admp.home")},
				},
			},
		})
	return handlers.EndConversation()
}

func adminCancel(b *gotgbot.Bot, ctx *ext.Context) error {
	_, _ = ctx.EffectiveMessage.Reply(b, "❌ Cancelled. Open the panel anytime with /admin", nil)
	return handlers.EndConversation()
}
