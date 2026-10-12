package engine

import (
	"fmt"
	"strings"
	"testing"
)

func TestConvoDigestFitsWhole(t *testing.T) {
	var turns []ConvoTurn
	for i := 1; i <= 50; i++ {
		turns = append(turns, ConvoTurn{N: i, When: "Oct 01", User: fmt.Sprintf("prompt %d", i), Reply: "reply"})
	}
	d := convoDigest(turns, "anything", askBudget)
	if !strings.Contains(d, "all 50 turns") || strings.Contains(d, "left out") || !strings.Contains(d, "#25 ") {
		t.Fatalf("a session that fits must be sent whole: %.200s", d)
	}
}

func TestConvoDigestPicksRelevantMiddle(t *testing.T) {
	var turns []ConvoTurn
	for i := 1; i <= 100; i++ {
		turns = append(turns, ConvoTurn{N: i, When: "Oct 01", User: strings.Repeat("x", 200), Reply: strings.Repeat("y", 200)})
	}
	turns[49].User = "we chose the kubernetes-retry backoff here"
	d := convoDigest(turns, "why did we pick the kubernetes-retry backoff?", 12000)
	if len(d) > 13500 {
		t.Fatalf("over budget: %d", len(d))
	}
	for _, want := range []string{"#1 ", "#5 ", "#100 ", "#50 ", "left out"} {
		if !strings.Contains(d, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(d, "#30 ") {
		t.Error("an unrelated middle turn should be left out")
	}
}

func TestQuestionWords(t *testing.T) {
	got := strings.Join(questionWords("What did we decide about the Retry logic in client.go?"), ",")
	if got != "decide,retry,logic,client.go" {
		t.Errorf("got %s", got)
	}
}
