package services

import "testing"

func TestExecPath(t *testing.T) {
	for show, want := range map[string]string{
		"{ path=/home/me/.local/bin/app ; argv[]=/home/me/.local/bin/app serve ; ignore_errors=no ; pid=3 }\n": "/home/me/.local/bin/app",
		"": "",
		"{ path=app ; argv[]=app }": "",
	} {
		if got := execPath(show); got != want {
			t.Errorf("execPath(%q) = %q, want %q", show, got, want)
		}
	}
}
