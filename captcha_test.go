package main

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Every challenge type must produce 5 distinct options with exactly one
// correct index.
func checkShape(t *testing.T, prompt string, options []string, answerIdx int) {
	t.Helper()
	if len(options) != 5 {
		t.Fatalf("%q: expected 5 options, got %d", prompt, len(options))
	}
	if answerIdx < 0 || answerIdx >= len(options) {
		t.Fatalf("%q: answerIdx %d out of range", prompt, answerIdx)
	}
	seen := map[string]bool{}
	for _, o := range options {
		if o == "" {
			t.Fatalf("%q: empty option", prompt)
		}
		if seen[o] {
			t.Fatalf("%q: duplicate option %q", prompt, o)
		}
		seen[o] = true
	}
}

var mathRe = regexp.MustCompile(`<b>(\d+) ([+−×]) (\d+) = \?</b>`)

func TestMathChallenge(t *testing.T) {
	for i := 0; i < 400; i++ {
		prompt, options, answerIdx := genMath()
		checkShape(t, prompt, options, answerIdx)

		m := mathRe.FindStringSubmatch(prompt)
		if m == nil {
			t.Fatalf("unparseable math prompt: %q", prompt)
		}
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[3])
		var want int
		switch m[2] {
		case "+":
			want = a + b
		case "−":
			want = a - b
			if want < 0 {
				t.Fatalf("negative subtraction question: %q", prompt)
			}
		case "×":
			want = a * b
		}
		if got, _ := strconv.Atoi(options[answerIdx]); got != want {
			t.Fatalf("%q: marked answer %d, want %d", prompt, got, want)
		}
	}
}

var seqRe = regexp.MustCompile(`<b>(\d+), (\d+), (\d+), __ \?</b>`)

func TestSequenceChallenge(t *testing.T) {
	for i := 0; i < 400; i++ {
		prompt, options, answerIdx := genSequence()
		checkShape(t, prompt, options, answerIdx)

		m := seqRe.FindStringSubmatch(prompt)
		if m == nil {
			t.Fatalf("unparseable sequence prompt: %q", prompt)
		}
		t1, _ := strconv.Atoi(m[1])
		t2, _ := strconv.Atoi(m[2])
		t3, _ := strconv.Atoi(m[3])
		step := t2 - t1
		if step <= 0 || t3-t2 != step {
			t.Fatalf("shown terms are not an arithmetic sequence: %q", prompt)
		}
		want := t3 + step
		if got, _ := strconv.Atoi(options[answerIdx]); got != want {
			t.Fatalf("%q: marked answer %d, want %d", prompt, got, want)
		}
	}
}

func TestEmojiCountChallenge(t *testing.T) {
	boldRe := regexp.MustCompile(`<b>([^<]+)</b>`)
	targets := map[string]bool{}
	fillers := map[string]bool{}
	for _, e := range []string{"🍎", "⭐", "🔥", "💎", "🎯", "🚀", "🍇", "🐸", "🌸", "🎈"} {
		targets[e] = true
	}
	for _, e := range []string{"🌿", "☁️", "🌊", "🍂", "🪨", "🫧", "🌵", "🧊"} {
		fillers[e] = true
	}

	for i := 0; i < 400; i++ {
		prompt, options, answerIdx := genEmojiCount()
		checkShape(t, prompt, options, answerIdx)

		parts := strings.Split(prompt, "\n\n")
		if len(parts) != 3 {
			t.Fatalf("unexpected emoji-count prompt shape: %q", prompt)
		}
		bm := boldRe.FindStringSubmatch(parts[2])
		if bm == nil {
			t.Fatalf("no target emoji marked in prompt: %q", prompt)
		}
		target := bm[1]
		if !targets[target] {
			t.Fatalf("%q: %q is not a target emoji", prompt, target)
		}

		// Count target occurrences in the emoji line
		line := parts[1]
		count := strings.Count(line, target)
		if count < 2 || count > 6 {
			t.Fatalf("%q: target count %d outside 2..6", prompt, count)
		}
		// Every other emoji in the line must be a filler
		for _, e := range strings.Fields(line) {
			if e != target && !fillers[e] {
				t.Fatalf("%q: unexpected emoji %q in line", prompt, e)
			}
		}
		if got, _ := strconv.Atoi(options[answerIdx]); got != count {
			t.Fatalf("%q: marked answer %d, actual count %d", prompt, got, count)
		}
	}
}

func TestOddOneOutChallenge(t *testing.T) {
	allFams := append(append([][]string{}, captchaEmojiFamilies...), captchaWordFamilies...)
	familyOf := func(item string) int {
		for i, fam := range allFams {
			for _, w := range fam {
				if w == item {
					return i
				}
			}
		}
		return -1
	}

	for i := 0; i < 400; i++ {
		prompt, options, answerIdx := genOddOneOut()
		checkShape(t, prompt, options, answerIdx)

		// The four non-answer options must share one family,
		// and the marked answer must NOT belong to it.
		famSet := map[int]bool{}
		for j, o := range options {
			if j == answerIdx {
				continue
			}
			f := familyOf(o)
			if f < 0 {
				t.Fatalf("%q: option %q belongs to no known family", prompt, o)
			}
			famSet[f] = true
		}
		if len(famSet) != 1 {
			t.Fatalf("%q: non-answer options span %d families", prompt, len(famSet))
		}
		var group int
		for f := range famSet {
			group = f
		}
		if familyOf(options[answerIdx]) == group {
			t.Fatalf("%q: marked answer %q is NOT the odd one out", prompt, options[answerIdx])
		}
	}
}

func TestCaptchaAllKindsReachable(t *testing.T) {
	seen := map[captchaKind]bool{}
	c := &pendingCaptcha{}
	for i := 0; i < 300; i++ {
		c.generateLocked()
		checkShape(t, c.prompt, c.options, c.answerIdx)
		seen[c.kind] = true
	}
	for k := captchaKind(0); k < kindCount; k++ {
		if !seen[k] {
			t.Fatalf("kind %d never generated in 300 tries", k)
		}
	}
}

// The verification page must render through the custom-emoji layer exactly
// like the rest of the UI — including the emojis that are part of the
// challenge itself (their appearance is one-to-one per glyph, so counting
// stays unambiguous) — while button labels stay standard, because Telegram
// cannot render custom emoji in inline buttons.
func TestCaptchaRenderPremiumized(t *testing.T) {
	setupTestDB(t)
	loadConfig(0, nil)

	star := iconDefaults["star"] // use the exact registry glyph
	apple := "🍎"                 // not a registry default → must never be touched
	c := &pendingCaptcha{
		prompt:    iconDefaults["eyes"] + " Count carefully!\n\n" + star + " " + apple + " " + star + "\n\nHow many <b>" + star + "</b> can you see?",
		options:   []string{"2", "1", "3", "4", "5"},
		answerIdx: 0,
	}

	// Standard mode (no mapping): zero custom-emoji tags anywhere.
	stdText, _ := renderCaptcha(c)
	if strings.Contains(stdText, "<tg-emoji") {
		t.Fatalf("standard mode rendered custom tags: %q", stdText)
	}
	if !strings.Contains(stdText, iconDefaults["robot"]) || !strings.Contains(stdText, "How many <b>"+star+"</b>") {
		t.Fatalf("standard fallbacks missing: %q", stdText)
	}

	if err := setEmojiIDs(map[string]string{
		"robot": "5500000000000000001",
		"eyes":  "5500000000000000002",
		"star":  "5500000000000000003",
	}); err != nil {
		t.Fatalf("setEmojiIDs: %v", err)
	}

	text, kb := renderCaptcha(c)
	robotTag := `<tg-emoji emoji-id="5500000000000000001">` + iconDefaults["robot"] + `</tg-emoji>`
	eyesTag := `<tg-emoji emoji-id="5500000000000000002">` + iconDefaults["eyes"] + `</tg-emoji>`
	starTag := `<tg-emoji emoji-id="5500000000000000003">` + star + `</tg-emoji>`

	if !strings.Contains(text, robotTag) {
		t.Fatalf("header robot icon not premiumized: %q", text)
	}
	if got := strings.Count(text, eyesTag); got != 1 {
		t.Fatalf("prompt icon should render custom exactly once, got %d: %q", got, text)
	}
	if got := strings.Count(text, starTag); got != 3 {
		t.Fatalf("all 3 target glyphs (grid + question) must render custom, got %d: %q", got, text)
	}
	if strings.Count(text, apple) != 1 || strings.Contains(text, ">"+apple+"<") {
		t.Fatalf("non-registry challenge emoji must stay untouched: %q", text)
	}
	if got := strings.Count(text, "<tg-emoji"); got != 5 {
		t.Fatalf("expected exactly 5 tags (robot + eyes + 3 stars), got %d: %q", got, text)
	}

	// Idempotent: re-sweeping must never nest tags.
	if again := premiumize(text); again != text {
		t.Fatalf("premiumize not idempotent on captcha text:\n%s\n---\n%s", text, again)
	}

	// Button labels stay standard — no tags ever.
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if strings.Contains(btn.Text, "<tg-emoji") {
				t.Fatalf("custom emoji tag leaked into button %q", btn.Text)
			}
		}
	}

	// Stripping the tags reproduces exactly the standard-rendered page, so
	// the delivery fallback path can never wedge on unsupported clients.
	if stripped := stripTGEmoji(text); stripped != stdText {
		t.Fatalf("stripTGEmoji mismatch:\n%q\nwant\n%q", stripped, stdText)
	}
}

func TestCaptchaLockout(t *testing.T) {
	// Unique per run so repeated -count runs don't share state
	uid := int64(500000 + time.Now().UnixNano()%400000)

	if d := captchaLockRemaining(uid); d != 0 {
		t.Fatalf("fresh user should not be locked, got %s", d)
	}
	for i := 0; i < captchaMaxFails-1; i++ {
		captchaRecordFail(uid)
		if d := captchaLockRemaining(uid); d != 0 {
			t.Fatalf("lock came early after %d fails: %s", i+1, d)
		}
	}
	captchaRecordFail(uid) // hits the threshold
	if d := captchaLockRemaining(uid); d <= 0 {
		t.Fatal("expected lockout after max consecutive fails")
	}

	captchaClearFails(uid)
	if d := captchaLockRemaining(uid); d != 0 {
		t.Fatalf("clear should end the lockout, got %s", d)
	}
}
