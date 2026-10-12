package engine

import (
	"fmt"
	"os"
	"path/filepath"
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

func TestReadConversationKeepsToolOutput(t *testing.T) {
	lines := []string{
		`{"type":"user","timestamp":"2026-10-01T10:00:00Z","message":{"content":"check the pods"}}`,
		`{"type":"assistant","timestamp":"2026-10-01T10:00:01Z","message":{"content":[{"type":"text","text":"Looking."},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"kubectl get pods"}},{"type":"tool_use","id":"t2","name":"Read","input":{"file_path":"/x/a.go"}},{"type":"tool_use","id":"t3","name":"Read","input":{"file_path":"/x/missing.go"}}]}}`,
		`{"type":"user","timestamp":"2026-10-01T10:00:02Z","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"payments-7f9   0/1   CrashLoopBackOff"},{"type":"tool_result","tool_use_id":"t2","content":"package a // lots of file text"},{"type":"tool_result","tool_use_id":"t3","is_error":true,"content":[{"type":"text","text":"no such file"}]}]}}`,
		`{"type":"user","timestamp":"2026-10-01T10:01:00Z","message":{"content":"<bash-input>git status</bash-input>"}}`,
		`{"type":"user","timestamp":"2026-10-01T10:01:01Z","message":{"content":"<bash-stdout>nothing to commit</bash-stdout><bash-stderr></bash-stderr>"}}`,
		`{"type":"user","timestamp":"2026-10-01T10:02:00Z","message":{"content":"why did it crash?"}}`,
	}
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	turns := readConversation(p)
	if len(turns) != 2 {
		t.Fatalf("want 2 turns, got %d", len(turns))
	}
	d := convoDigest(turns, "which pod crashed?", askBudget)
	for _, want := range []string{"ran: kubectl get pods", "payments-7f9", "CrashLoopBackOff", "error in: Read /x/missing.go", "no such file", "user ran: git status", "nothing to commit"} {
		if !strings.Contains(d, want) {
			t.Errorf("digest missing %q:\n%s", want, d)
		}
	}
	if strings.Contains(d, "lots of file text") {
		t.Error("successful non-Bash results must not be included")
	}
}

func TestTrimOutputKeepsEnds(t *testing.T) {
	got := trimOutput("START "+strings.Repeat("mid ", 500)+"ERROR: boom", 200)
	if !strings.HasPrefix(got, "START") || !strings.HasSuffix(got, "ERROR: boom") || !strings.Contains(got, "[…]") {
		t.Errorf("got %q", got)
	}
}
