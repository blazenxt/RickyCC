package main

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
)

// Conversation states for the admin panel
const (
	admStateFindUser = "ADM_FIND_USER"
	admStateAddCards = "ADM_ADD_CODES"
	admStateFsubAdd  = "ADM_FSUB_ADD"
	admStateLogSet   = "ADM_LOG_SET"
	admStateTarget   = "ADM_TARGET"
	admStateAdminAdd = "ADM_ADMIN_ADD"
	admStateSupport  = "ADM_SUPPORT"
	admStateHowto    = "ADM_HOWTO"
	admStateEmojis   = "ADM_EMOJIS"
)

const admTimeFmt = "02 Jan 06 15:04"

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
	if !isAdmin(ctx.EffectiveUser.Id) {
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
	m := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{admBtn("📊 Dashboard", "admp.dash")},
			{admBtn("👥 Users", "admp.users"), admBtn("🎟️ Cards", "admp.codes")},
			{admBtn("🛠 Settings", "admp.settings"), admBtn("📢 Broadcast", "admp.bcast")},
			{admBtn("👑 Admins", "admp.admins"), admBtn("❌ Close", "admp.close")},
		},
	}
	return *decorateButtons(&m)
}

// admEdit is a small helper to swap the panel view. Text and BUTTONS both
// pass through the premium-emoji layer, so every panel screen inherits the
// owner's custom set automatically.
func admEdit(b *gotgbot.Bot, msg *gotgbot.Message, text string, kb gotgbot.InlineKeyboardMarkup) {
	_, _, err := msg.EditText(b, premiumize(text), &gotgbot.EditMessageTextOpts{
		ParseMode:   "HTML",
		ReplyMarkup: *decorateButtons(&kb),
	})
	if err != nil {
		log.Printf("admin panel edit failed: %v", err)
	}
}

// ---------- Settings views ----------

func admSettingsView() (string, gotgbot.InlineKeyboardMarkup) {
	chans := getFsubChannels()
	fsum := "<i>none — open access</i>"
	if len(chans) > 0 {
		var sb strings.Builder
		for i, c := range chans {
			fmt.Fprintf(&sb, "%d. <code>%d</code>\n", i+1, c)
		}
		fsum = strings.TrimRight(sb.String(), "\n")
	}

	logStr := "<i>not set</i>"
	if id := getLogChat(); id != 0 {
		logStr = fmt.Sprintf("<code>%d</code>", id)
	}

	supportStr := "<i>not set — button hidden</i>"
	if u := getSupportURL(); u != "" {
		supportStr = fmt.Sprintf("<code>%s</code>", esc(u))
	}

	claimStr := "🟢 Open"
	toggleLabel := "⏸️ Pause Claims"
	if ClaimsPaused {
		claimStr = "⏸️ Paused"
		toggleLabel = "▶️ Resume Claims"
	}

	emojiStr := "<i>standard emojis</i>"
	if n := len(getEmojiIDs()); n > 0 {
		emojiStr = fmt.Sprintf("<b>%d custom</b>", n)
	}

	text := fmt.Sprintf(
		"🛠 <b>Bot Settings</b>\n\n"+
			"📢 <b>Force-join channels:</b>\n%s\n\n"+
			"🪵 <b>Log chat:</b> %s\n"+
			"🎯 <b>Referral target:</b> <b>%d</b>\n"+
			"🎁 <b>Claims:</b> %s\n"+
			"🆘 <b>Support link:</b> %s\n"+
			"📖 <b>How-to text:</b> <i>%s</i>\n"+
			"🎨 <b>Custom emojis:</b> %s\n\n"+
			"<i>Changes apply instantly and persist across restarts.</i>",
		fsum, logStr, ReferralTarget, claimStr, supportStr, esc(truncate(getHowtoText(), 60)), emojiStr)

	kb := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{admBtn("📢 Force-Join Setup", "admp.fsub")},
			{admBtn("🪵 Set Log Chat", "admc.logset")},
			{admBtn("🎯 Referral Target", "admc.target"), admBtn(toggleLabel, "admp.claimstoggle")},
			{admBtn("🆘 Support Link", "admc.support"), admBtn("📖 How-to Text", "admc.howto")},
			{admBtn("🎨 Custom Emojis", "admc.emojis"), admBtn("⚡ Load Premium Set", "admp.emojipremium")},
			admBackBtn(),
		},
	}
	return text, kb
}

func admFsubView() (string, gotgbot.InlineKeyboardMarkup) {
	chans := getFsubChannels()

	var sb strings.Builder
	sb.WriteString("📢 <b>Force-Join Setup</b>\n\n")
	if len(chans) == 0 {
		sb.WriteString("<i>No channels set — anyone can use the bot without joining.</i>\n")
	} else {
		sb.WriteString("Users must join <b>ALL</b> of these:\n\n")
		for i, c := range chans {
			fmt.Fprintf(&sb, "%d. <code>%d</code>\n", i+1, c)
		}
	}
	sb.WriteString("\n<i>The bot must be admin in each channel (invite permission).</i>")

	rows := [][]gotgbot.InlineKeyboardButton{}
	for _, c := range chans {
		rows = append(rows, []gotgbot.InlineKeyboardButton{
			admBtn(fmt.Sprintf("❌ Remove %d", c), fmt.Sprintf("admp.fsubdel.%d", c)),
		})
	}
	rows = append(rows, []gotgbot.InlineKeyboardButton{admBtn("➕ Add Channel", "admc.fsubadd")})
	if len(chans) > 0 {
		rows = append(rows, []gotgbot.InlineKeyboardButton{admBtn("🧹 Clear All", "admp.fsubclear")})
	}
	rows = append(rows, []gotgbot.InlineKeyboardButton{admBtn("🔙 Settings", "admp.settings")})

	return sb.String(), gotgbot.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func admAdminsView() (string, gotgbot.InlineKeyboardMarkup) {
	ids := getAdminIDs()

	var sb strings.Builder
	sb.WriteString("👑 <b>Admin Management</b>\n\n")
	fmt.Fprintf(&sb, "🔐 Owner: <code>%d</code>\n\n", OwnerID)
	if len(ids) == 0 {
		sb.WriteString("<i>No extra admins yet.</i>\n")
	} else {
		fmt.Fprintf(&sb, "Admins: <b>%d</b>\n", len(ids))
		for i, a := range ids {
			fmt.Fprintf(&sb, "%d. <code>%d</code>\n", i+1, a)
		}
	}
	sb.WriteString("\n<i>Admins can use the full panel. Only the owner (OWNER_ID in env) can add/remove admins.</i>")

	rows := [][]gotgbot.InlineKeyboardButton{}
	for _, a := range ids {
		rows = append(rows, []gotgbot.InlineKeyboardButton{
			admBtn(fmt.Sprintf("❌ Remove %d", a), fmt.Sprintf("admp.admindel.%d", a)),
		})
	}
	rows = append(rows, []gotgbot.InlineKeyboardButton{admBtn("➕ Add Admin", "admc.adminadd")})
	rows = append(rows, admBackBtn())

	return sb.String(), gotgbot.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// ---------- Panel router (admp.*) ----------

func adminCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	query := ctx.CallbackQuery

	if !isAdmin(query.From.Id) {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "❌ This panel is for the bot owner only.",
			ShowAlert: true,
		})
		return nil
	}

	action := strings.TrimPrefix(query.Data, "admp.")

	// Admin removal carries an ID: admp.admindel.<id> (owner only)
	if idStr, ok := strings.CutPrefix(action, "admindel."); ok {
		if !isOwner(query.From.Id) {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text:      "🔐 Only the bot owner can manage admins.",
				ShowAlert: true,
			})
			return nil
		}
		adminID := stringToInt64(idStr)
		removed, err := removeAdminID(adminID)
		if err != nil {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text: "❌ " + CustomError(err).Error(), ShowAlert: true})
			return nil
		}
		if !removed {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text: "⚠️ Not in the admin list.", ShowAlert: true})
			return nil
		}
		log.Printf("owner removed admin %d", adminID)
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "❌ Admin removed."})
		text, kb := admAdminsView()
		admEdit(b, msg, text, kb)
		return nil
	}

	// Force-join channel removal carries an ID: admp.fsubdel.<id>
	if idStr, ok := strings.CutPrefix(action, "fsubdel."); ok {
		chatID := stringToInt64(idStr)
		removed, err := removeFsubChannel(chatID)
		if err != nil {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text: "❌ " + CustomError(err).Error(), ShowAlert: true})
			return nil
		}
		if !removed {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text: "⚠️ Channel not in the list.", ShowAlert: true})
			return nil
		}
		log.Printf("admin removed force-join channel %d", chatID)
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "❌ Channel removed."})
		text, kb := admFsubView()
		admEdit(b, msg, text, kb)
		return nil
	}

	switch action {
	case "admins":
		if !isOwner(query.From.Id) {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text:      "🔐 Only the bot owner can manage admins.",
				ShowAlert: true,
			})
			return nil
		}
		_, _ = query.Answer(b, nil)
		text, kb := admAdminsView()
		admEdit(b, msg, text, kb)

	case "settings":
		_, _ = query.Answer(b, nil)
		text, kb := admSettingsView()
		admEdit(b, msg, text, kb)

	case "fsub":
		_, _ = query.Answer(b, nil)
		text, kb := admFsubView()
		admEdit(b, msg, text, kb)

	case "fsubclear":
		if err := clearFsubChannels(); err != nil {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text: "❌ " + CustomError(err).Error(), ShowAlert: true})
			return nil
		}
		log.Printf("admin cleared all force-join channels")
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: "🧹 Cleared — bot is now open access.", ShowAlert: true})
		text, kb := admFsubView()
		admEdit(b, msg, text, kb)

	case "claimstoggle":
		newState := !ClaimsPaused
		if err := setClaimsPaused(newState); err != nil {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text: "❌ " + CustomError(err).Error(), ShowAlert: true})
			return nil
		}
		label := "▶️ Claims resumed."
		if newState {
			label = "⏸️ Claims paused — users can't claim until resumed."
		}
		log.Printf("admin toggled claims: paused=%v", newState)
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: label, ShowAlert: newState})
		text, kb := admSettingsView()
		admEdit(b, msg, text, kb)

	case "emojipremium":
		// One-tap curated premium look — gated behind a live probe so it can
		// never break message delivery on bots without public-emoji rights.
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "⏳ Probing premium emoji support…"})

		probe := map[string]string{}
		n := 0
		for s, id := range premiumEmojiDefaults {
			probe[s] = id
			n++
			if n >= 3 {
				break
			}
		}
		if bad := validateEmojiIDs(b, query.From.Id, probe); len(bad) > 0 {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text:      "❌ This bot can't send public custom emojis — it needs an extra username bought on Fragment. Use a bot-owned pack or plain emojis instead.",
				ShowAlert: true,
			})
			return nil
		}

		next := make(map[string]string, len(premiumEmojiDefaults))
		for s, id := range premiumEmojiDefaults {
			next[s] = id
		}
		if err := setEmojiIDs(next); err != nil {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text: "❌ " + CustomError(err).Error(), ShowAlert: true})
			return nil
		}
		log.Printf("admin loaded the premium emoji set (%d slots)", len(next))
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      fmt.Sprintf("⚡ Premium set loaded — %d icons upgraded!", len(next)),
			ShowAlert: true,
		})
		text, kb := admSettingsView()
		admEdit(b, msg, text, kb)

	case "home":
		_, _ = query.Answer(b, nil)
		admEdit(b, msg, admPanelText(), admPanelKeyboard())

	case "dash":
		_, _ = query.Answer(b, nil)
		total, _ := countAllUsers()
		banned, _ := countBannedUsers()
		claimedU, _ := countClaimedUsers()
		avail, _ := countAvailableCards()
		claimedC, _ := countClaimedCards()

		admEdit(b, msg, fmt.Sprintf(
			icon("stats")+" <b>Dashboard</b>\n\n"+
				icon("users")+" Total users: <b>%d</b>\n"+
				icon("ban")+" Banned: <b>%d</b>\n"+
				icon("gift")+" Users claimed: <b>%d</b>\n\n"+
				icon("ticket")+" Cards available: <b>%d</b>\n"+
				icon("ok")+" Cards claimed: <b>%d</b>\n\n"+
				icon("clock")+" <i>Refreshed %s</i>",
			total, banned, claimedU, avail, claimedC, time.Now().Format("15:04:05")),
			gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					{admBtn("🔄 Refresh", "admp.dash")},
					admBackBtn(),
				},
			})

	case "users":
		_, _ = query.Answer(b, nil)
		text, kb := admUsersView()
		admEdit(b, msg, text, kb)

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
			fmt.Fprintf(&sb, "• %s — <code>%d</code> — 👥%d 🎁×%d%s\n",
				name, u.ID, len(u.ReferredUsers), u.Claims, flags)
		}
		sb.WriteString("\n<i>Tap a user below to manage them.</i> 🎁 has rewards · 🚫 banned")

		rows := admRecentUserButtons(users)
		rows = append(rows, []gotgbot.InlineKeyboardButton{
			admBtn("🔍 Find User", "admc.finduser"),
			admBtn("🔙 Users", "admp.users"),
		})
		admEdit(b, msg, sb.String(), gotgbot.InlineKeyboardMarkup{InlineKeyboard: rows})

	case "codes":
		_, _ = query.Answer(b, nil)
		text, kb := admCodesView()
		admEdit(b, msg, text, kb)

	case "claims":
		_, _ = query.Answer(b, nil)
		claims, err := getRecentClaims(10)
		if err != nil {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text: "❌ " + CustomError(err).Error(), ShowAlert: true})
			return nil
		}

		var sb strings.Builder
		sb.WriteString("🧾 <b>Latest claimed cards</b>\n\n")
		if len(claims) == 0 {
			sb.WriteString("<i>Nothing claimed yet.</i>")
		}
		for _, c := range claims {
			when := "—"
			if c.ClaimedAt != nil {
				when = c.ClaimedAt.Format(admTimeFmt)
			}
			fmt.Fprintf(&sb, "• <code>%s</code>\n  → <code>%d</code> · %s\n",
				esc(truncate(c.Card, 30)), c.ClaimedBy, when)
		}

		admEdit(b, msg, sb.String(), gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{admBtn("🔙 Cards", "admp.codes")},
			},
		})

	case "clear":
		claimedC, _ := countClaimedCards()
		_, _ = query.Answer(b, nil)
		admEdit(b, msg, fmt.Sprintf(
			"🧹 <b>Clear claimed cards?</b>\n\n"+
				"This will permanently delete <b>%d</b> claimed card record(s).\n"+
				"<i>Claim history will be lost.</i>",
			claimedC),
			gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					{admBtn("✅ Yes, delete them", "admp.clearok")},
					{admBtn("🔙 Cancel", "admp.codes")},
				},
			})

	case "clearok":
		deleted, err := clearClaimedCards()
		if err != nil {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text: "❌ " + CustomError(err).Error(), ShowAlert: true})
			return nil
		}
		log.Printf("admin cleared %d claimed cards", deleted)
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: fmt.Sprintf("🧹 Deleted %d record(s).", deleted)})
		admEdit(b, msg, fmt.Sprintf("🧹 Deleted <b>%d</b> claimed card record(s).", deleted),
			gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					{admBtn("🔙 Cards", "admp.codes")},
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

// admRecentUserButtons renders one tap-to-manage row per user for the
// Recent Users screen — the list is no longer a dead end (fixed the
// "flow breaks midway" gap: admins had to memorise the ID and re-run
// 🔍 Find User just to act on someone). Read-only action: admu.view.*
// simply renders the user's manage card.
func admUsersView() (string, gotgbot.InlineKeyboardMarkup) {
	total, _ := countAllUsers()
	banned, _ := countBannedUsers()
	claimedU, _ := countClaimedUsers()
	return fmt.Sprintf(
			"👥 <b>User Management</b>\n\n"+
				"Total: <b>%d</b>  ·  🚫 Banned: <b>%d</b>  ·  🎁 Claimed: <b>%d</b>",
			total, banned, claimedU), gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{admBtn("🔍 Find User", "admc.finduser")},
				{admBtn("🆕 Recent Users", "admp.recent")},
				admBackBtn(),
			},
		}
}

func admCodesView() (string, gotgbot.InlineKeyboardMarkup) {
	avail, _ := countAvailableCards()
	claimedC, _ := countClaimedCards()
	return fmt.Sprintf(
			"🎟️ <b>Card Stock</b>\n\n"+
				"✅ Available: <b>%d</b>\n"+
				"🎁 Claimed: <b>%d</b>\n"+
				"📦 Total: <b>%d</b>",
			avail, claimedC, avail+claimedC), gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{admBtn("➕ Add Cards", "admc.addcodes")},
				{admBtn("🧾 Recent Claims", "admp.claims")},
				{admBtn("🧹 Clear Claimed", "admp.clear")},
				admBackBtn(),
			},
		}
}

func admRecentUserButtons(users []User) [][]gotgbot.InlineKeyboardButton {
	rows := make([][]gotgbot.InlineKeyboardButton, 0, len(users))
	for _, u := range users {
		name := strings.TrimSpace(u.Name)
		if name == "" {
			name = fmt.Sprintf("User %d", u.ID)
		}
		name = truncate(name, 18)
		label := "👤 " + name
		if u.Banned {
			label = "🚫 " + name
		}
		rows = append(rows, []gotgbot.InlineKeyboardButton{
			admBtn(label, fmt.Sprintf("admu.view.%d", u.ID)),
		})
	}
	return rows
}

func adminUserCardView(u *User) (string, gotgbot.InlineKeyboardMarkup) {
	claim := fmt.Sprintf("<b>%d</b> claimed", u.Claims)
	if u.Claims > 0 {
		claim += fmt.Sprintf("\n└ Latest: <code>%s</code>", esc(truncate(u.ClaimedCard, 40)))
	}
	if ready := unlocksAvailable(len(u.ReferredUsers), u.Claims, ReferralTarget); ready > 0 {
		claim += fmt.Sprintf("\n└ %s <b>%d ready</b> to collect", icon("trophy"), ready)
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
		icon("person")+" <b>User Card</b>\n\n"+
			icon("id")+" ID: <code>%d</code>\n"+
			icon("name")+" Name: %s %s\n"+
			icon("link")+" Referrer: <code>%d</code>\n"+
			icon("users")+" Referrals: <b>%d</b> (1 card / %d refs)\n"+
			icon("gift")+" Rewards: %s\n"+
			icon("cal")+" Joined: %s\n"+
			icon("shield")+" Status: <b>%s</b>",
		u.ID, name, uname, u.Referrer,
		len(u.ReferredUsers), ReferralTarget,
		claim, joined, status)

	banBtn := admBtn("🚫 Ban", fmt.Sprintf("admu.ban.%d", u.ID))
	if u.Banned {
		banBtn = admBtn("✅ Unban", fmt.Sprintf("admu.unban.%d", u.ID))
	}

	kb := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{banBtn, admBtn("🔄 Reset Claims", fmt.Sprintf("admu.reset.%d", u.ID))},
			{admBtn("🗑️ Delete User", fmt.Sprintf("admu.del.%d", u.ID))},
			{admBtn("🔙 Users", "admp.users")},
		},
	}
	return text, kb
}

func adminUserCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	query := ctx.CallbackQuery

	if !isAdmin(query.From.Id) {
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
	case "view":
		// Read-only: render the manage card (mutations go through the
		// dedicated actions below, which then refresh this same view).
		u, err := getUser(uid)
		if err != nil {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text:      "❌ User not found — they may have been deleted.",
				ShowAlert: true})
			return nil
		}
		_, _ = query.Answer(b, nil)
		text, kb := adminUserCardView(u)
		admEdit(b, msg, text, kb)
		return nil

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
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "🔄 Claims reset — all earned rewards can be collected again."})

	case "del":
		if uid == OwnerID {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text:      "❌ You can't delete yourself.",
				ShowAlert: true})
			return nil
		}
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
		if uid == OwnerID {
			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text:      "❌ You can't delete yourself.",
				ShowAlert: true})
			return nil
		}
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

// ---------- Conversations: find user & add cards ----------

// adminFindUserStart is the entry point for the find-user conversation.
func adminFindUserStart(b *gotgbot.Bot, ctx *ext.Context) error {
	query := ctx.CallbackQuery
	if !isAdmin(query.From.Id) {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "❌ Owner only.",
			ShowAlert: true,
		})
		return handlers.EndConversation()
	}

	_, _ = query.Answer(b, nil)
	_, _, _ = ctx.EffectiveMessage.EditText(b, premiumize(
		"🔍 <b>Find User</b>\n\nSend the user's <b>Telegram ID</b> now.\n<i>Tip: recent users can be managed with one tap from 🆕 Recent Users.</i>"),
		&gotgbot.EditMessageTextOpts{ParseMode: "HTML", ReplyMarkup: admConvBackBtn("users")})
	return handlers.NextConversationState(admStateFindUser)
}

func adminFindUserMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	if msg.From == nil || !isAdmin(msg.From.Id) {
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
	_, _ = msg.Reply(b, premiumize(text), &gotgbot.SendMessageOpts{
		ParseMode:   "HTML",
		ReplyMarkup: *decorateButtons(&kb), // same icons + role colors as the panel
	})
	return handlers.EndConversation()
}

// adminAddCardsStart is the entry point for the add-cards conversation.
func adminAddCardsStart(b *gotgbot.Bot, ctx *ext.Context) error {
	query := ctx.CallbackQuery
	if !isAdmin(query.From.Id) {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "❌ Owner only.",
			ShowAlert: true,
		})
		return handlers.EndConversation()
	}

	avail, _ := countAvailableCards()
	_, _ = query.Answer(b, nil)
	_, _, _ = ctx.EffectiveMessage.EditText(b, premiumize(fmt.Sprintf(
		"➕ <b>Add Codes</b>\n\n"+
			"Send the cards now — <b>one per line</b>. Duplicates are skipped automatically.\n\n"+
			"📦 Current stock: <b>%d</b>",
		avail)),
		&gotgbot.EditMessageTextOpts{ParseMode: "HTML", ReplyMarkup: admConvBackBtn("codes")})
	return handlers.NextConversationState(admStateAddCards)
}

func adminAddCardsMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	if msg.From == nil || !isAdmin(msg.From.Id) {
		return handlers.EndConversation()
	}

	added, skipped, err := addCards(strings.Split(msg.GetText(), "\n"))
	if err != nil {
		_, _ = msg.Reply(b, "❌ Failed to add cards: "+CustomError(err).Error(), nil)
		return handlers.EndConversation()
	}

	log.Printf("admin added %d cards (%d skipped)", added, skipped)
	total, _ := countAvailableCards()
	_, _ = msg.Reply(b, premiumize(fmt.Sprintf(
		"✅ <b>Added %d card(s).</b>\n⏭️ Skipped (duplicates/empty): %d\n📦 <b>Stock available:</b> %d",
		added, skipped, total)),
		&gotgbot.SendMessageOpts{
			ParseMode: "HTML",
			ReplyMarkup: *decorateButtons(&gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					{admBtn("➕ Add More", "admc.addcodes")},
					{admBtn("⚙️ Panel", "admp.home")},
				},
			}),
		})
	if added > 0 {
		go broadcastStockUpdate(b, added, total, msg.From.FirstName)
	}
	return handlers.EndConversation()
}

func adminCancel(b *gotgbot.Bot, ctx *ext.Context) error {
	_, _ = ctx.EffectiveMessage.Reply(b, "❌ Cancelled. Open the panel anytime with /admin", nil)
	return handlers.EndConversation()
}

// ---------- Conversations: bot settings ----------

// admConvBackBtn is the single 🔙 Back button attached to every
// conversation prompt — it replaces the retired /cancel command flow.
// Tapping it ends the conversation and takes the panel straight back to
// the section the conversation was opened from.
func admConvBackBtn(target string) gotgbot.InlineKeyboardMarkup {
	kb := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{admBtn("🔙 Back", "admcback."+target)},
		},
	}
	return *decorateButtons(&kb)
}

// adminConversationBack powers that button (registered inside every
// conversation state, so it only fires while a conversation is active).
func adminConversationBack(b *gotgbot.Bot, ctx *ext.Context) error {
	query := ctx.CallbackQuery
	msg := ctx.EffectiveMessage

	if !isAdmin(query.From.Id) {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "❌ Owner only.",
			ShowAlert: true,
		})
		return handlers.EndConversation()
	}

	var (
		text string
		kb   gotgbot.InlineKeyboardMarkup
	)
	switch strings.TrimPrefix(query.Data, "admcback.") {
	case "users":
		text, kb = admUsersView()
	case "codes":
		text, kb = admCodesView()
	case "fsub":
		text, kb = admFsubView()
	case "admins":
		text, kb = admAdminsView()
	default: // "settings" and anything unknown land on the settings hub
		text, kb = admSettingsView()
	}
	_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "🔙 Back"})
	admEdit(b, msg, text, kb)
	return handlers.EndConversation()
}

func admSettingsBackBtn() gotgbot.InlineKeyboardMarkup {
	m := gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{admBtn("🛠 Settings", "admp.settings")},
		},
	}
	return *decorateButtons(&m)
}

// adminLogSetStart asks the owner for the log chat ID.
func adminLogSetStart(b *gotgbot.Bot, ctx *ext.Context) error {
	query := ctx.CallbackQuery
	if !isAdmin(query.From.Id) {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: "❌ Owner only.", ShowAlert: true})
		return handlers.EndConversation()
	}

	_, _ = query.Answer(b, nil)
	_, _, _ = ctx.EffectiveMessage.EditText(b, premiumize(
		"🪵 <b>Set Log Chat</b>\n\n"+
			"Send the chat ID where claim notifications should go — a channel/group (<code>-100...</code>) or your own user ID.\n\n"+
			"<i>The bot must be able to message that chat (member/admin).</i>"),
		&gotgbot.EditMessageTextOpts{ParseMode: "HTML", ReplyMarkup: admConvBackBtn("settings")})
	return handlers.NextConversationState(admStateLogSet)
}

func adminLogSetMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	if msg.From == nil || !isAdmin(msg.From.Id) {
		return handlers.EndConversation()
	}

	chatID := stringToInt64(strings.TrimSpace(msg.GetText()))
	if chatID == 0 {
		_, _ = msg.Reply(b, "❌ Invalid chat ID. Send a numeric chat ID, or /cancel.", nil)
		return nil
	}

	// Verify the bot can reach the chat before saving
	if _, err := b.GetChat(chatID, nil); err != nil {
		_, _ = msg.Reply(b,
			"❌ I can't access that chat. Add me there first (member or admin), then send the ID again — or /cancel.",
			nil)
		return nil
	}

	if err := setLogChat(chatID); err != nil {
		_, _ = msg.Reply(b, "❌ Failed to save: "+CustomError(err).Error(), nil)
		return handlers.EndConversation()
	}

	log.Printf("admin set log chat to %d", chatID)
	_, _ = msg.Reply(b, fmt.Sprintf(
		"✅ <b>Log chat set!</b>\n\nClaim notifications will now go to <code>%d</code>.", chatID),
		&gotgbot.SendMessageOpts{ParseMode: "HTML", ReplyMarkup: admSettingsBackBtn()})
	return handlers.EndConversation()
}

// adminFsubAddStart asks the owner for a force-join channel ID.
func adminFsubAddStart(b *gotgbot.Bot, ctx *ext.Context) error {
	query := ctx.CallbackQuery
	if !isAdmin(query.From.Id) {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: "❌ Owner only.", ShowAlert: true})
		return handlers.EndConversation()
	}

	_, _ = query.Answer(b, nil)
	_, _, _ = ctx.EffectiveMessage.EditText(b, premiumize(
		"📢 <b>Add Force-Join Channel</b>\n\n"+
			"Send the channel ID (e.g. <code>-1001234567890</code>).\n\n"+
			"<i>The bot must be an <b>admin</b> in the channel with invite permission, otherwise users can't join it.</i>"),
		&gotgbot.EditMessageTextOpts{ParseMode: "HTML", ReplyMarkup: admConvBackBtn("fsub")})
	return handlers.NextConversationState(admStateFsubAdd)
}

func adminFsubAddMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	if msg.From == nil || !isAdmin(msg.From.Id) {
		return handlers.EndConversation()
	}

	chatID := stringToInt64(strings.TrimSpace(msg.GetText()))
	if chatID >= 0 {
		_, _ = msg.Reply(b, "❌ Invalid channel ID. Channels/groups have negative IDs like <code>-1001234567890</code>.\n\nTry again, or /cancel.",
			&gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return nil
	}

	chat, err := b.GetChat(chatID, nil)
	if err != nil {
		_, _ = msg.Reply(b,
			"❌ I can't access that chat. Make me an <b>admin</b> in the channel first, then send the ID again — or /cancel.",
			&gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return nil
	}

	added, err := addFsubChannel(chatID)
	if err != nil {
		_, _ = msg.Reply(b, "❌ Failed to save: "+CustomError(err).Error(), nil)
		return handlers.EndConversation()
	}
	if !added {
		_, _ = msg.Reply(b, "⚠️ That channel is already in the force-join list.",
			&gotgbot.SendMessageOpts{ReplyMarkup: admSettingsBackBtn()})
		return handlers.EndConversation()
	}

	// Warm the invite-link cache; create one if the channel has none
	linkStatus := "✅ Invite link ready."
	link, lerr := fetchInviteLink(b, chatID)
	if lerr != nil || link == "" {
		if inv, cerr := b.CreateChatInviteLink(chatID, nil); cerr == nil && inv != nil && inv.InviteLink != "" {
			cacheInviteLink(chatID, inv.InviteLink)
		} else {
			linkStatus = "⚠️ Couldn't get/create an invite link — check my admin invite permission, or users won't be able to join!"
		}
	}

	log.Printf("admin added force-join channel %d (%s)", chatID, chat.Title)
	_, _ = msg.Reply(b, fmt.Sprintf(
		"✅ <b>Channel added to force-join!</b>\n\n📢 %s\n🆔 <code>%d</code>\n%s",
		esc(chat.Title), chatID, linkStatus),
		&gotgbot.SendMessageOpts{ParseMode: "HTML", ReplyMarkup: admSettingsBackBtn()})
	return handlers.EndConversation()
}

// adminTargetStart asks the owner for the referral target.
func adminTargetStart(b *gotgbot.Bot, ctx *ext.Context) error {
	query := ctx.CallbackQuery
	if !isAdmin(query.From.Id) {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: "❌ Owner only.", ShowAlert: true})
		return handlers.EndConversation()
	}

	_, _ = query.Answer(b, nil)
	_, _, _ = ctx.EffectiveMessage.EditText(b, premiumize(fmt.Sprintf(
		"🎯 <b>Referral Target</b>\n\nSend the number of friends a user must refer to unlock a reward.\n\nCurrent: <b>%d</b>",
		ReferralTarget)),
		&gotgbot.EditMessageTextOpts{ParseMode: "HTML", ReplyMarkup: admConvBackBtn("settings")})
	return handlers.NextConversationState(admStateTarget)
}

func adminTargetMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	if msg.From == nil || !isAdmin(msg.From.Id) {
		return handlers.EndConversation()
	}

	n := int(stringToInt64(strings.TrimSpace(msg.GetText())))
	if n < 1 || n > 10000 {
		_, _ = msg.Reply(b, "❌ Send a number between 1 and 10000, or /cancel.", nil)
		return nil
	}

	if err := setReferralTarget(n); err != nil {
		_, _ = msg.Reply(b, "❌ Failed to save: "+CustomError(err).Error(), nil)
		return handlers.EndConversation()
	}

	log.Printf("admin set referral target to %d", n)
	_, _ = msg.Reply(b, fmt.Sprintf(
		"✅ <b>Referral target updated!</b>\n\nUsers now need <b>%d</b> referrals to claim a reward.", n),
		&gotgbot.SendMessageOpts{ParseMode: "HTML", ReplyMarkup: admSettingsBackBtn()})
	return handlers.EndConversation()
}

// adminAdminAddStart asks the owner for a user ID to grant admin access.
func adminAdminAddStart(b *gotgbot.Bot, ctx *ext.Context) error {
	query := ctx.CallbackQuery
	if !isOwner(query.From.Id) {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "🔐 Only the bot owner can manage admins.",
			ShowAlert: true,
		})
		return handlers.EndConversation()
	}

	_, _ = query.Answer(b, nil)
	_, _, _ = ctx.EffectiveMessage.EditText(b, premiumize(
		"👑 <b>Add Admin</b>\n\n"+
			"Send the Telegram user ID you want to grant full admin panel access.\n\n"+
			"<i>They will be able to manage users, cards, settings and broadcast. Choose carefully.</i>"),
		&gotgbot.EditMessageTextOpts{ParseMode: "HTML", ReplyMarkup: admConvBackBtn("admins")})
	return handlers.NextConversationState(admStateAdminAdd)
}

func adminAdminAddMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	if msg.From == nil || !isOwner(msg.From.Id) {
		return handlers.EndConversation()
	}

	adminID := stringToInt64(strings.TrimSpace(msg.GetText()))
	if adminID <= 0 {
		_, _ = msg.Reply(b, "❌ Invalid ID. Send a numeric Telegram user ID, or /cancel.", nil)
		return nil
	}

	added, err := addAdminID(adminID)
	if err != nil {
		_, _ = msg.Reply(b, "❌ Failed to save: "+CustomError(err).Error(), nil)
		return handlers.EndConversation()
	}
	if !added {
		_, _ = msg.Reply(b, "⚠️ That user already has admin access.",
			&gotgbot.SendMessageOpts{ReplyMarkup: admSettingsBackBtn()})
		return handlers.EndConversation()
	}

	log.Printf("owner added admin %d", adminID)
	_, _ = msg.Reply(b, fmt.Sprintf(
		"✅ <b>Admin added!</b>\n\n<code>%d</code> can now open /admin and manage the bot.",
		adminID),
		&gotgbot.SendMessageOpts{ParseMode: "HTML", ReplyMarkup: admSettingsBackBtn()})

	return handlers.EndConversation()
}

// adminSupportStart asks for the URL behind the 🆘 Support button.
func adminSupportStart(b *gotgbot.Bot, ctx *ext.Context) error {
	query := ctx.CallbackQuery
	if !isAdmin(query.From.Id) {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: "❌ Admins only.", ShowAlert: true})
		return handlers.EndConversation()
	}

	current := getSupportURL()
	if current == "" {
		current = "<i>not set — button hidden</i>"
	} else {
		current = "<code>" + esc(current) + "</code>"
	}

	_, _ = query.Answer(b, nil)
	_, _, _ = ctx.EffectiveMessage.EditText(b, premiumize(fmt.Sprintf(
		"🆘 <b>Support Link</b>\n\n"+
			"Send the link for the Support button shown with delivered cards — e.g. <code>https://t.me/YourSupport</code>.\n\n"+
			"Current: %s\n\n"+
			"Send <code>off</code> to hide the button.",
		current)),
		&gotgbot.EditMessageTextOpts{ParseMode: "HTML", ReplyMarkup: admConvBackBtn("settings")})
	return handlers.NextConversationState(admStateSupport)
}

func adminSupportMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	if msg.From == nil || !isAdmin(msg.From.Id) {
		return handlers.EndConversation()
	}

	raw := strings.TrimSpace(msg.GetText())

	if strings.EqualFold(raw, "off") || strings.EqualFold(raw, "none") {
		if err := setSupportURL(""); err != nil {
			_, _ = msg.Reply(b, "❌ Failed to save: "+CustomError(err).Error(), nil)
			return handlers.EndConversation()
		}
		log.Printf("admin hid the support button")
		_, _ = msg.Reply(b, "✅ <b>Support button hidden.</b>",
			&gotgbot.SendMessageOpts{ParseMode: "HTML", ReplyMarkup: admSettingsBackBtn()})
		return handlers.EndConversation()
	}

	// Normalise shorthand: t.me/foo → https://t.me/foo ; @foo → https://t.me/foo
	url := raw
	if strings.HasPrefix(url, "@") {
		url = "https://t.me/" + strings.TrimPrefix(url, "@")
	} else if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}
	if !(strings.HasPrefix(url, "https://t.me/") || strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://")) ||
		len(url) > 200 || strings.ContainsAny(url, " \n\t\"'<>") {
		_, _ = msg.Reply(b, "❌ That doesn't look like a valid link. Send an https or t.me link, <code>off</code>, or /cancel.",
			&gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return nil
	}

	if err := setSupportURL(url); err != nil {
		_, _ = msg.Reply(b, "❌ Failed to save: "+CustomError(err).Error(), nil)
		return handlers.EndConversation()
	}

	log.Printf("admin set support link to %s", url)
	_, _ = msg.Reply(b, fmt.Sprintf(
		"✅ <b>Support link updated!</b>\n\n🆘 Button now points to: <code>%s</code>", url),
		&gotgbot.SendMessageOpts{ParseMode: "HTML", ReplyMarkup: admSettingsBackBtn()})
	return handlers.EndConversation()
}

// adminHowtoStart asks for the how-to-use text shown under delivered cards.
func adminHowtoStart(b *gotgbot.Bot, ctx *ext.Context) error {
	query := ctx.CallbackQuery
	if !isAdmin(query.From.Id) {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: "❌ Admins only.", ShowAlert: true})
		return handlers.EndConversation()
	}

	_, _ = query.Answer(b, nil)
	_, _, _ = ctx.EffectiveMessage.EditText(b, premiumize(fmt.Sprintf(
		"📖 <b>How-to-Use Text</b>\n\n"+
			"Send the instructions shown under every delivered card (plain text, up to 700 characters).\n\n"+
			"<b>Current:</b>\n<i>%s</i>\n\n"+
			"Send <code>default</code> to restore the built-in text.",
		esc(truncate(getHowtoText(), 300)))),
		&gotgbot.EditMessageTextOpts{ParseMode: "HTML", ReplyMarkup: admConvBackBtn("settings")})
	return handlers.NextConversationState(admStateHowto)
}

func adminHowtoMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	if msg.From == nil || !isAdmin(msg.From.Id) {
		return handlers.EndConversation()
	}

	raw := strings.TrimSpace(msg.GetText())

	if strings.EqualFold(raw, "default") {
		if err := setHowtoText(""); err != nil {
			_, _ = msg.Reply(b, "❌ Failed to save: "+CustomError(err).Error(), nil)
			return handlers.EndConversation()
		}
		log.Printf("admin restored default how-to text")
		_, _ = msg.Reply(b, "✅ <b>Default how-to text restored.</b>",
			&gotgbot.SendMessageOpts{ParseMode: "HTML", ReplyMarkup: admSettingsBackBtn()})
		return handlers.EndConversation()
	}

	runes := []rune(raw)
	if len(runes) < 5 || len(runes) > 700 {
		_, _ = msg.Reply(b, "❌ Keep it between 5 and 700 characters. Try again, send <code>default</code>, or /cancel.",
			&gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return nil
	}

	if err := setHowtoText(raw); err != nil {
		_, _ = msg.Reply(b, "❌ Failed to save: "+CustomError(err).Error(), nil)
		return handlers.EndConversation()
	}

	log.Printf("admin updated how-to text (%d chars)", len(runes))
	_, _ = msg.Reply(b, "✅ <b>How-to text updated!</b>\n\nNew cards will show it under the card details.",
		&gotgbot.SendMessageOpts{ParseMode: "HTML", ReplyMarkup: admSettingsBackBtn()})
	return handlers.EndConversation()
}

// ---------- Custom emoji icons (admc.emojis) ----------

func adminEmojisStart(b *gotgbot.Bot, ctx *ext.Context) error {
	query := ctx.CallbackQuery
	if !isAdmin(query.From.Id) {
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: "❌ Admins only.", ShowAlert: true})
		return handlers.EndConversation()
	}

	cur := getEmojiIDs()
	curStr := "<i>none — standard emojis everywhere</i>"
	if len(cur) > 0 {
		names := make([]string, 0, len(cur))
		for s := range cur {
			names = append(names, s)
		}
		sort.Strings(names)
		var sb strings.Builder
		for _, s := range names {
			fmt.Fprintf(&sb, "▫️ <code>%s</code> = <code>%s</code>\n", s, cur[s])
		}
		curStr = strings.TrimRight(sb.String(), "\n")
	}

	_, _ = query.Answer(b, nil)
	_, _, _ = ctx.EffectiveMessage.EditText(b, premiumize(fmt.Sprintf(
		"🎨 <b>Custom Emojis</b>\n\n"+
			"Replace the bot's standard icons — they change in all message texts, the reward-delivery caption AND on buttons (icon shown before the label on Bot API 9.4+ clients; standard text fallback elsewhere).\n\n"+
			"<b>Two ways to map a slot:</b>\n"+
			"1️⃣ <b>Any public emoji</b> (works instantly, no restrictions):\n"+
			"<code>card=🔥\nparty=💎</code>\n"+
			"2️⃣ <b>Premium custom emoji ID</b>:\n"+
			"<code>trophy=5402038549988123456</code>\n"+
			"⚠️ Custom IDs work only from packs <b>owned by this bot</b> (@BotFather → /newemojipack) — "+
			"or from <b>any public pack</b> if the bot owns an extra username bought on <b>Fragment</b>. "+
			"Get an ID by sending the emoji to @JsonDumpBot (<code>custom_emoji_id</code>).\n\n"+
			"<b>Available slots:</b>\n%s\n\n"+
			"<b>Currently set:</b>\n%s\n\n"+
			"Send <code>off</code> to clear all.",
		emojiSlotList(), curStr)),
		&gotgbot.EditMessageTextOpts{ParseMode: "HTML", ReplyMarkup: admConvBackBtn("settings")})
	return handlers.NextConversationState(admStateEmojis)
}

func adminEmojisMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	if msg.From == nil || !isAdmin(msg.From.Id) {
		return handlers.EndConversation()
	}
	raw := strings.TrimSpace(msg.GetText())

	if strings.EqualFold(raw, "off") || strings.EqualFold(raw, "clear") {
		if err := clearEmojiIDs(); err != nil {
			_, _ = msg.Reply(b, "❌ Failed to save: "+CustomError(err).Error(), nil)
			return handlers.EndConversation()
		}
		log.Printf("admin cleared custom emojis")
		_, _ = msg.Reply(b, "✅ <b>Custom emojis cleared</b> — standard icons are back everywhere.",
			&gotgbot.SendMessageOpts{ParseMode: "HTML", ReplyMarkup: admSettingsBackBtn()})
		return handlers.EndConversation()
	}

	// Parse "slot=ID" lines (also tolerates "slot: ID" / "slot ID")
	candidate := map[string]string{}
	var unknown, malformed []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.FieldsFunc(line, func(r rune) bool {
			return r == '=' || r == ':' || r == ' ' || r == '\t'
		})
		if len(parts) != 2 {
			malformed = append(malformed, truncate(line, 30))
			continue
		}
		slot := strings.ToLower(strings.TrimSpace(parts[0]))
		id := strings.TrimSpace(parts[1])
		if _, ok := iconDefaults[slot]; !ok {
			unknown = append(unknown, slot)
			continue
		}
		if !isEmojiID(id) && !isPlainEmoji(id) {
			malformed = append(malformed, truncate(line, 30))
			continue
		}
		candidate[slot] = id
	}

	if len(candidate) == 0 {
		_, _ = msg.Reply(b,
			"❌ <b>No valid mappings found.</b>\n\nUse one <code>slot=ID</code> pair per line, e.g.:\n<code>card=5402038549988123456</code>\n\nCheck the slot list above, send <code>off</code>, or /cancel.",
			&gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return nil // stay in the conversation for another try
	}

	// Merge over existing slots so partial updates are additive
	merged := getEmojiIDs()
	for s, id := range candidate {
		merged[s] = id
	}

	// Live-validate every new ID with a test-send to this chat
	_, _ = msg.Reply(b, fmt.Sprintf("⏳ Validating <b>%d</b> emoji ID(s) with Telegram…", len(candidate)),
		&gotgbot.SendMessageOpts{ParseMode: "HTML"})
	badIDs := validateEmojiIDs(b, msg.Chat.Id, candidate)
	var rejected []string
	for s, id := range badIDs {
		delete(merged, s)
		rejected = append(rejected, fmt.Sprintf("%s = %s", s, id))
	}

	if err := setEmojiIDs(merged); err != nil {
		_, _ = msg.Reply(b, "❌ Failed to save: "+CustomError(err).Error(), nil)
		return handlers.EndConversation()
	}

	saved := len(candidate) - len(rejected)
	log.Printf("admin saved %d custom emojis (%d rejected)", saved, len(rejected))

	var sb strings.Builder
	fmt.Fprintf(&sb, "✅ <b>Custom emojis updated!</b>\n\n🎨 Saved: <b>%d</b>  ·  📦 Total active: <b>%d</b>", saved, len(merged))
	if len(rejected) > 0 {
		sort.Strings(rejected)
		fmt.Fprintf(&sb, "\n\n❌ <b>Rejected by Telegram</b> (use a bot-owned pack, buy the bot a Fragment username to unlock <b>all</b> public custom emojis, or use plain emojis):\n<code>%s</code>", strings.Join(rejected, "\n"))
	}
	if len(unknown) > 0 {
		fmt.Fprintf(&sb, "\n\n⚠️ Unknown slots skipped: <code>%s</code>", strings.Join(unknown, ", "))
	}
	if len(malformed) > 0 {
		fmt.Fprintf(&sb, "\n\n⚠️ Malformed lines skipped: <code>%s</code>", strings.Join(malformed, ", "))
	}
	sb.WriteString("\n\n<i>Open /start to preview the new look.</i>")

	_, _ = msg.Reply(b, sb.String(),
		&gotgbot.SendMessageOpts{ParseMode: "HTML", ReplyMarkup: admSettingsBackBtn()})
	return handlers.EndConversation()
}
