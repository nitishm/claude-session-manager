package engine

import "testing"

func TestBundleOfIgnoresArguments(t *testing.T) {
	login := "/usr/bin/login -flp me /bin/bash --noprofile --norc -c exec -l '/x/CCS.app/Contents/MacOS/ccs-go' --select a"
	if p, n := bundleOf(login); p != "" || n != "" {
		t.Fatalf("login line matched %q %q", p, n)
	}
	if _, n := bundleOf("/Applications/Visual Studio Code - Insiders.app/Contents/MacOS/Electron"); n != "Visual Studio Code - Insiders" {
		t.Fatalf("name = %q", n)
	}
	if _, n := bundleOf("-/x/CCS.app/Contents/MacOS/ccs-go --select a"); n != "CCS" {
		t.Fatalf("name = %q", n)
	}
}
