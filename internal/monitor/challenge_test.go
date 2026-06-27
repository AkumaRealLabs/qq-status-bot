package monitor

import (
	"strings"
	"testing"
)

func TestGenerateChallengeExpectedAnswerIsInPrompt(t *testing.T) {
	for range 50 {
		ch := generateChallenge()
		if ch.ExpectedAnswer == "" || !strings.Contains(strings.ToLower(ch.Prompt), ch.ExpectedAnswer) {
			t.Fatalf("challenge = %+v", ch)
		}
	}
}

func TestValidateResponse(t *testing.T) {
	if got := validateResponse("Banana.", "banana"); !got.Valid {
		t.Fatalf("correct answer rejected: %+v", got)
	}
	for _, response := range []string{"", "blue", "the answer is banana because the category is fruit"} {
		if got := validateResponse(response, "banana"); got.Valid {
			t.Fatalf("response %q accepted: %+v", response, got)
		}
	}
}
