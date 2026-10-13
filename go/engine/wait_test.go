package engine

import "testing"

func TestLiveWaiting(t *testing.T) {
	cases := map[string]bool{"idle": false, "busy": false, "waiting": true, "": true}
	for st, want := range cases {
		if got := (LiveInfo{PID: 1, Status: st}).Waiting(); got != want {
			t.Errorf("status %q: got %v want %v", st, got, want)
		}
	}
	if (LiveInfo{}).Waiting() {
		t.Error("not running must not count as waiting")
	}
}
