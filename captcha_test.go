package main

import (
	"fmt"
	"testing"
)

// The math captcha must always include exactly one correct option and no
// duplicates/negatives — otherwise users could get stuck at verification.
func TestCaptchaQuestionShape(t *testing.T) {
	ops := []struct {
		s string
		f func(a, b int) int
	}{
		{"+", func(a, b int) int { return a + b }},
		{"−", func(a, b int) int { return a - b }},
		{"×", func(a, b int) int { return a * b }},
	}

	for i := 0; i < 500; i++ {
		q, answer, options := newCaptchaQuestion()

		if len(options) != 5 {
			t.Fatalf("%q: expected 5 options, got %d", q, len(options))
		}
		seen := map[int]bool{}
		correct := 0
		for _, o := range options {
			if o < 0 {
				t.Fatalf("%q: negative option %d", q, o)
			}
			if seen[o] {
				t.Fatalf("%q: duplicate option %d", q, o)
			}
			seen[o] = true
			if o == answer {
				correct++
			}
		}
		if correct != 1 {
			t.Fatalf("%q: answer %d must appear exactly once in %v", q, answer, options)
		}

		// The printed question must actually evaluate to `answer`.
		found := false
		for _, op := range ops {
			var a, b int
			if n, err := fmt.Sscanf(q, "%d "+op.s+" %d", &a, &b); err == nil && n == 2 {
				if got := op.f(a, b); got != answer {
					t.Fatalf("question %q = %d, but answer stored is %d", q, got, answer)
				}
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("unrecognised question format: %q", q)
		}
	}
}
