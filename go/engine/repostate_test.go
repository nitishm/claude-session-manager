package engine

import "testing"

func TestParseGitStatus(t *testing.T) {
	var st RepoState
	parseGitStatus(&st, "## main...origin/main [ahead 2, behind 1]\n M a.go\n?? b.go\n")
	if st.Branch != "main" || st.Ahead != 2 || st.Behind != 1 || st.Dirty != 2 {
		t.Fatalf("got %+v", st)
	}
	st = RepoState{}
	parseGitStatus(&st, "## feat/x\n")
	if st.Branch != "feat/x" || st.Dirty != 0 {
		t.Fatalf("got %+v", st)
	}
}

func TestParsePR(t *testing.T) {
	p, err := parsePR([]byte(`{"state":"OPEN","isDraft":false,"reviewDecision":"APPROVED","title":"t","statusCheckRollup":[{"conclusion":"SUCCESS"},{"status":"IN_PROGRESS","conclusion":""},{"state":"SUCCESS"}]}`))
	if err != nil || p.Passed != 2 || p.Pending != 1 || p.Checks != "pending" || p.Review != "APPROVED" {
		t.Fatalf("got %+v %v", p, err)
	}
	p, _ = parsePR([]byte(`{"state":"MERGED","statusCheckRollup":[{"conclusion":"FAILURE"}]}`))
	if p.Checks != "failing" || p.State != "MERGED" {
		t.Fatalf("got %+v", p)
	}
}
