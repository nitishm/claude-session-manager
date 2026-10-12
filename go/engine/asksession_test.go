package engine

import (
	"fmt"
	"strings"
	"testing"
)

func TestAskDigestKeepsHeadAndTail(t *testing.T) {
	var ex []Exchange
	for i := 1; i <= 200; i++ {
		ex = append(ex, Exchange{N: i, When: "Oct 01", User: strings.Repeat("u", 250), Reply: strings.Repeat("r", 250)})
	}
	d := askDigest(ex)
	if len(d) > 19000 {
		t.Fatalf("digest too long: %d", len(d))
	}
	for _, want := range []string{"#1 ", "#8 ", "#200 ", "omitted"} {
		if !strings.Contains(d, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(d, fmt.Sprintf("#%d ", 50)) {
		t.Error("middle should be dropped")
	}
	if small := askDigest(ex[:3]); strings.Contains(small, "omitted") {
		t.Error("short sessions are kept whole")
	}
}
