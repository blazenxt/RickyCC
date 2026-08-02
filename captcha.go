package main

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

// Captcha verification: after a NEW user passes the force-join check, they
// solve a one-tap challenge before being registered. Challenges rotate
// between four types so scripts can't rely on a single fixed format:
//
//	🧮 math (add / subtract / multiply)
//	🔢 number sequence ("3, 8, 13, __ ?")
//	👀 emoji counting (targets mixed with fillers)
//	🕵️ odd-one-out (emoji or word categories)
//
// Every wrong tap regenerates the challenge (possibly a different type).
// 3 tries per challenge; 3 failed challenges in a row = 15-minute lockout.
//
// State lives in memory: pending questions and the referral payload are kept
// server-side (callback data only carries the tapped index), entries expire
// after captchaTTL — /start issues a fresh challenge.

type captchaKind int

const (
	kindMath captchaKind = iota
	kindSequence
	kindEmojiCount
	kindOddOneOut
	kindCount // number of kinds, for random selection
)

type pendingCaptcha struct {
	kind      captchaKind
	prompt    string   // question body (HTML)
	options   []string // button labels, shuffled
	answerIdx int      // index of the correct option

	payload   string // original /start payload (referrer id), preserved
	tries     int
	createdAt time.Time
}

const (
	captchaMaxTries = 3
	captchaTTL      = 30 * time.Minute
	captchaMaxFails = 3
	captchaLockout  = 15 * time.Minute
)

var captchaStore = struct {
	sync.Mutex
	m map[int64]*pendingCaptcha
}{m: make(map[int64]*pendingCaptcha)}

// ---------- shuffle helper ----------

func shuffleOptions(options []string, answerIdx int) ([]string, int) {
	rand.Shuffle(len(options), func(i, j int) {
		options[i], options[j] = options[j], options[i]
		if answerIdx == i {
			answerIdx = j
		} else if answerIdx == j {
			answerIdx = i
		}
	})
	return options, answerIdx
}

// ---------- challenge generators ----------

// genMath: "7 + 5 = ?"
func genMath() (string, []string, int) {
	var a, b, ans int
	var op string
	switch rand.IntN(3) {
	case 0:
		a, b = 2+rand.IntN(8), 2+rand.IntN(8)
		ans, op = a+b, "+"
	case 1:
		a = 3 + rand.IntN(7)
		b = 1 + rand.IntN(a-1)
		ans, op = a-b, "−"
	default:
		a, b = 2+rand.IntN(8), 2+rand.IntN(4)
		ans, op = a*b, "×"
	}
	prompt := fmt.Sprintf(icon("math")+" Solve this:\n\n<b>%d %s %d = ?</b>", a, op, b)

	seen := map[int]bool{ans: true}
	options := []string{strconv.Itoa(ans)}
	for len(options) < 5 {
		cand := ans + rand.IntN(11) - 5
		if cand < 0 || seen[cand] {
			continue
		}
		seen[cand] = true
		options = append(options, strconv.Itoa(cand))
	}
	options, idx := shuffleOptions(options, 0)
	return prompt, options, idx
}

// genSequence: "3, 8, 13, __ ?"
func genSequence() (string, []string, int) {
	start, step := 1+rand.IntN(6), 2+rand.IntN(5)
	t1, t2, t3 := start, start+step, start+2*step
	ans := start + 3*step
	prompt := fmt.Sprintf(icon("num")+" What comes next?\n\n<b>%d, %d, %d, __ ?</b>", t1, t2, t3)

	options := []string{strconv.Itoa(ans)}
	for _, cand := range []int{ans + step, ans - step, ans + 1, ans + 2, ans - 1} {
		if len(options) == 5 {
			break
		}
		if cand < 0 {
			continue
		}
		dup := false
		for _, o := range options {
			if o == strconv.Itoa(cand) {
				dup = true
				break
			}
		}
		if !dup {
			options = append(options, strconv.Itoa(cand))
		}
	}
	options, idx := shuffleOptions(options, 0)
	return prompt, options, idx
}

// genEmojiCount: targets hidden among fillers — count only the target emoji.
func genEmojiCount() (string, []string, int) {
	targets := []string{"🍎", "⭐", "🔥", "💎", "🎯", "🚀", "🍇", "🐸", "🌸", "🎈"}
	fillers := []string{"🌿", "☁️", "🌊", "🍂", "🪨", "🫧", "🌵", "🧊"}
	target := targets[rand.IntN(len(targets))]

	n := 2 + rand.IntN(5) // 2..6 targets
	m := 2 + rand.IntN(3) // 2..4 fillers
	items := make([]string, 0, n+m)
	for i := 0; i < n; i++ {
		items = append(items, target)
	}
	for i := 0; i < m; i++ {
		items = append(items, fillers[rand.IntN(len(fillers))])
	}
	rand.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })

	prompt := fmt.Sprintf(icon("eyes")+" Count carefully!\n\n%s\n\nHow many <b>%s</b> can you see?", strings.Join(items, " "), target)

	seen := map[int]bool{n: true}
	options := []string{strconv.Itoa(n)}
	for _, base := range []int{n - 1, n + 1, n - 2, n + 2, n + 3} {
		if len(options) == 5 {
			break
		}
		if base > 0 && !seen[base] {
			seen[base] = true
			options = append(options, strconv.Itoa(base))
		}
	}
	options, idx := shuffleOptions(options, 0)
	return prompt, options, idx
}

// Odd-one-out families: 4 items from one family + 1 intruder from another.
var captchaEmojiFamilies = [][]string{
	{"🍎", "🍌", "🍇", "🍉", "🍓", "🥝", "🍑"},
	{"🐶", "🐱", "🦊", "🦁", "🐼", "🐸"},
	{"🚗", "🚲", "✈️", "🚀", "🚁", "🚌"},
	{"⚽", "🏀", "🎾", "🏐", "🎱", "🏓"},
}

// NOTE: families must stay disjoint (no shared words) so the intruder is
// always unambiguous — "orange" deliberately appears in NONE of them.
var captchaWordFamilies = [][]string{
	{"MANGO", "APPLE", "BANANA", "GRAPES", "CHERRY", "PAPAYA"},
	{"DOG", "CAT", "LION", "TIGER", "HORSE", "PANDA"},
	{"CAR", "BUS", "TRAIN", "BOAT", "PLANE", "CYCLE"},
	{"RED", "BLUE", "GREEN", "YELLOW", "PURPLE", "PINK"},
}

// genOddOneOut: 4 items from one family + 1 intruder from another.
func genOddOneOut() (string, []string, int) {
	var fams [][]string
	label := ""
	if rand.IntN(2) == 0 {
		fams, label = captchaEmojiFamilies, "emojis"
	} else {
		fams, label = captchaWordFamilies, "words"
	}

	fi := rand.IntN(len(fams))
	fj := (fi + 1 + rand.IntN(len(fams)-1)) % len(fams) // guaranteed different

	picks := rand.Perm(len(fams[fi]))[:4]
	options := make([]string, 0, 5)
	for _, p := range picks {
		options = append(options, fams[fi][p])
	}
	// Defensive: never allow the intruder to collide with a pick
	inSame := func(s string) bool {
		for _, o := range options {
			if o == s {
				return true
			}
		}
		return false
	}
	intruder := fams[fj][rand.IntN(len(fams[fj]))]
	for tries := 0; tries < 8 && inSame(intruder); tries++ {
		intruder = fams[fj][rand.IntN(len(fams[fj]))]
	}
	options = append(options, intruder)

	prompt := fmt.Sprintf(icon("spy")+" Four of these %s belong together, one doesn't.\nTap the <b>odd one out</b>! 👇", label)
	options, idx := shuffleOptions(options, 4)
	return prompt, options, idx
}

// generateLocked builds a fresh challenge into the pending entry.
// Caller must hold captchaStore (entry not yet shared otherwise).
func (c *pendingCaptcha) generateLocked() {
	c.kind = captchaKind(rand.IntN(int(kindCount)))
	switch c.kind {
	case kindSequence:
		c.prompt, c.options, c.answerIdx = genSequence()
	case kindEmojiCount:
		c.prompt, c.options, c.answerIdx = genEmojiCount()
	case kindOddOneOut:
		c.prompt, c.options, c.answerIdx = genOddOneOut()
	default:
		c.kind = kindMath
		c.prompt, c.options, c.answerIdx = genMath()
	}
}

// ---------- fail / lockout tracking ----------

type captchaFailState struct {
	fails       int
	firstFail   time.Time
	lockedUntil time.Time
}

const captchaFailWindow = time.Hour

var captchaFails = struct {
	sync.Mutex
	m map[int64]*captchaFailState
}{m: make(map[int64]*captchaFailState)}

// captchaLockRemaining is a pure read — it must NOT mutate state, otherwise
// the fail counter would reset every time /start checks the lock.
func captchaLockRemaining(userID int64) time.Duration {
	captchaFails.Lock()
	defer captchaFails.Unlock()
	st, ok := captchaFails.m[userID]
	if !ok {
		return 0
	}
	if d := time.Until(st.lockedUntil); d > 0 {
		return d
	}
	return 0
}

// captchaRecordFail counts a fully-failed challenge; captchaMaxFails failures
// inside captchaFailWindow locks the user out for captchaLockout.
func captchaRecordFail(userID int64) {
	captchaFails.Lock()
	defer captchaFails.Unlock()
	st, ok := captchaFails.m[userID]
	if !ok {
		st = &captchaFailState{firstFail: time.Now()}
		captchaFails.m[userID] = st
	}
	if time.Since(st.firstFail) > captchaFailWindow {
		st.fails = 0 // old failures decay
		st.firstFail = time.Now()
	}
	st.fails++
	if st.fails >= captchaMaxFails {
		st.fails = 0
		st.lockedUntil = time.Now().Add(captchaLockout)
	}
}

func captchaClearFails(userID int64) {
	captchaFails.Lock()
	delete(captchaFails.m, userID)
	captchaFails.Unlock()
}

// ---------- rendering ----------

func renderCaptcha(c *pendingCaptcha) (string, gotgbot.InlineKeyboardMarkup) {
	row := make([]gotgbot.InlineKeyboardButton, 0, len(c.options))
	for i, opt := range c.options {
		row = append(row, gotgbot.InlineKeyboardButton{
			Text:         opt,
			CallbackData: fmt.Sprintf("cap.%d", i),
		})
	}

	text := fmt.Sprintf(
		icon("robot")+" <b>Human Verification</b>\n\n"+
			"%s\n\n"+
			"Tap the correct answer below. <i>(Attempt %d of %d)</i>",
		c.prompt, c.tries+1, captchaMaxTries)

	// Sweep every remaining raw Unicode emoji (counting-grid targets like
	// ⭐🔥💎🎯, pointers, any unmapped header glyph) through the custom-emoji
	// layer so the whole verification page matches the rest of the UI.
	// Button labels never go through premiumize — Telegram cannot render
	// custom emoji in buttons, so options stay standard by design.
	return premiumize(text), *decorateButtons(&gotgbot.InlineKeyboardMarkup{InlineKeyboard: [][]gotgbot.InlineKeyboardButton{row}})
}

// ---------- flow ----------

// issueCaptcha registers a fresh challenge for a new user and shows it.
// The referral payload is kept server-side and restored on success.
func issueCaptcha(b *gotgbot.Bot, msg *gotgbot.Message, userID int64, payload string) error {
	if rem := captchaLockRemaining(userID); rem > 0 {
		_, err := msg.Reply(b, fmt.Sprintf(
			icon("validity")+" <b>Too many failed verifications.</b>\n\nPlease wait about <b>%d minute(s)</b> and send /start again.",
			int(rem.Minutes())+1), &gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	c := &pendingCaptcha{payload: payload, createdAt: time.Now()}
	c.generateLocked()

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

// captchaCallback handles "cap.<index>" taps.
func captchaCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	query := ctx.CallbackQuery
	msg := ctx.EffectiveMessage
	user := ctx.EffectiveUser

	idx, _ := strconv.Atoi(strings.TrimPrefix(query.Data, "cap."))

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
			icon("validity")+" <b>Verification expired.</b>\n\nSend /start to begin again.",
			&gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
		return nil

	case idx >= 0 && idx < len(c.options) && idx == c.answerIdx:
		payload := c.payload
		delete(captchaStore.m, user.Id)
		captchaStore.Unlock()
		captchaClearFails(user.Id)

		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "✅ Verified — welcome!"})
		_, _, _ = msg.EditText(b,
			icon("ok")+" <b>Verified!</b> Setting up your account...",
			&gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
		return completeRegistration(b, ctx, payload)

	default:
		c.tries++
		if c.tries >= captchaMaxTries {
			delete(captchaStore.m, user.Id)
			captchaStore.Unlock()
			captchaRecordFail(user.Id)

			_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text:      fmt.Sprintf("❌ Verification failed (%d/%d)!", captchaMaxFails, captchaMaxFails),
				ShowAlert: true,
			})
			_, _, _ = msg.EditText(b,
				icon("err")+" <b>Verification failed.</b>\n\nToo many wrong attempts — send /start to begin a new verification.",
				&gotgbot.EditMessageTextOpts{ParseMode: "HTML"})
			return nil
		}

		c.generateLocked() // brand-new challenge (possibly another type)
		text, kb := renderCaptcha(c)
		captchaStore.Unlock()

		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "❌ Wrong — new challenge, try again!",
			ShowAlert: true,
		})
		_, _, _ = msg.EditText(b, text, &gotgbot.EditMessageTextOpts{
			ParseMode:   "HTML",
			ReplyMarkup: kb,
		})
		return nil
	}
}
