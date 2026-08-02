package main

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

// Captcha verification: after a NEW user passes the force-join check, they
// must solve a quick math question (inline buttons) before being registered.
// This blocks scripted join-farms that would otherwise farm referrals.
//
// State lives in memory: a fresh question is asked per attempt, the pending
// referral payload is kept server-side (never exposed in callback data), and
// entries expire after captchaTTL — /start simply issues a new challenge.

type pendingCaptcha struct {
	answer    int
	payload   string // original /start payload (referrer id), preserved
	tries     int
	createdAt time.Time

	question string
	options  []int
}

const (
	captchaMaxTries = 3
	captchaTTL      = 30 * time.Minute
)

var captchaStore = struct {
	sync.Mutex
	m map[int64]*pendingCaptcha
}{m: make(map[int64]*pendingCaptcha)}

// newCaptchaQuestion builds a fresh math question with shuffled options and
// returns it together with the correct answer.
func newCaptchaQuestion() (question string, answer int, options []int) {
	var a, b int
	switch rand.IntN(3) {
	case 0: // addition
		a, b = 2+rand.IntN(8), 2+rand.IntN(8)
		answer = a + b
		question = fmt.Sprintf("%d + %d", a, b)
	case 1: // subtraction (always non-negative)
		a = 3 + rand.IntN(7)
		b = 1 + rand.IntN(a-1)
		answer = a - b
		question = fmt.Sprintf("%d − %d", a, b)
	default: // small multiplication
		a, b = 2+rand.IntN(8), 2+rand.IntN(4)
		answer = a * b
		question = fmt.Sprintf("%d × %d", a, b)
	}

	// 4 distractors within ±5 of the answer, always distinct and non-negative
	seen := map[int]bool{answer: true}
	options = []int{answer}
	for len(options) < 5 {
		cand := answer + rand.IntN(11) - 5
		if cand < 0 || seen[cand] {
			continue
		}
		seen[cand] = true
		options = append(options, cand)
	}
	rand.Shuffle(len(options), func(i, j int) { options[i], options[j] = options[j], options[i] })
	return question, answer, options
}

// refreshLocked replaces the question (and answer) on an existing pending entry.
// Caller must hold captchaStore.
func (c *pendingCaptcha) refreshLocked() {
	c.question, c.answer, c.options = newCaptchaQuestion()
}

// renderCaptcha builds the verification prompt and its answer keyboard.
func renderCaptcha(c *pendingCaptcha) (string, gotgbot.InlineKeyboardMarkup) {
	row := make([]gotgbot.InlineKeyboardButton, 0, len(c.options))
	for _, opt := range c.options {
		row = append(row, gotgbot.InlineKeyboardButton{
			Text:         fmt.Sprintf("%d", opt),
			CallbackData: fmt.Sprintf("cap.%d", opt),
		})
	}

	text := fmt.Sprintf(
		"🤖 <b>Human Verification</b>\n\n"+
			"One quick step before we continue — prove you're human:\n\n"+
			"🧮 <b>%s = ?</b>\n\n"+
			"Tap the correct answer below. <i>(Attempt %d of %d)</i>",
		c.question, c.tries+1, captchaMaxTries)

	return text, gotgbot.InlineKeyboardMarkup{InlineKeyboard: [][]gotgbot.InlineKeyboardButton{row}}
}

// issueCaptcha registers a fresh challenge for a new user and shows it.
// The referral payload is kept server-side and restored on success.
func issueCaptcha(b *gotgbot.Bot, msg *gotgbot.Message, userID int64, payload string) error {
	c := &pendingCaptcha{payload: payload, createdAt: time.Now()}
	c.refreshLocked()

	captchaStore.Lock()
	// Lazy cleanup of abandoned challenges
	for id, p := range captchaStore.m {
		if time.Since(p.createdAt) > captchaTTL {
			delete(captchaStore.m, id)
		}
	}
	captchaStore.m[userID] = c
	captchaStore.Unlock()

	text, kb := renderCaptcha(c)
	_, err := msg.Reply(b, text, &gotgbot.SendMessageOpts{
		ParseMode:   "HTML",
		ReplyMarkup: kb,
	})
	return err
}

// captchaCallback handles "cap.<choice>" taps.
func captchaCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	query := ctx.CallbackQuery
	msg := ctx.EffectiveMessage
	user := ctx.EffectiveUser

	choice := int(stringToInt64(strings.TrimPrefix(query.Data, "cap.")))

	captchaStore.Lock()
	c, ok := captchaStore.m[user.Id]
	if ok && time.Since(c.createdAt) > captchaTTL {
		delete(captchaStore.m, user.Id)
		ok = false
	}

	switch {
	case !ok:
		captchaStore.Unlock()
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "⌛ Verification expired — send /start to get a new one.",
			ShowAlert: true,
		})
		_, _, _ = msg.EditText(b,
			"⌛ <b>Verification expired.</b>\n\nSend /start to begin again.",
			&gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
		return nil

	case choice == c.answer:
		payload := c.payload
		delete(captchaStore.m, user.Id)
		captchaStore.Unlock()

		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "✅ Verified — welcome!"})
		_, _, _ = msg.EditText(b,
			"✅ <b>Verified!</b> Setting up your account...",
			&gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
		return completeRegistration(b, ctx, payload)

	default:
		c.tries++
		if c.tries >= captchaMaxTries {
			delete(captchaStore.m, user.Id)
			captchaStore.Unlock()

			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text:      "❌ Too many wrong attempts!",
				ShowAlert: true,
			})
			_, _, _ = msg.EditText(b,
				"❌ <b>Verification failed.</b>\n\nToo many wrong attempts — send /start to try again.",
				&gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
			return nil
		}

		c.refreshLocked() // brand-new question for every wrong try
		text, kb := renderCaptcha(c)
		captchaStore.Unlock()

		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "❌ Wrong answer — try again!",
			ShowAlert: true,
		})
		_, _, _ = msg.EditText(b, text, &gotgbot.EditMessageTextOpts{
			ParseMode:   "HTML",
			ReplyMarkup: kb,
		})
		return nil
	}
}
