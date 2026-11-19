package services

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTracked(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte(`{"version":1,"services":[
		{"unit":"jellyfin.service","scope":"user","url":"http://localhost:8096/health"},
		{"unit":"nginx.service"}]}`), 0o644)
	got, err := Tracked(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []Unit{
		{Name: "jellyfin.service", Scope: "user", URL: "http://localhost:8096/health"},
		{Name: "nginx.service", Scope: "system"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tracked = %+v", got)
	}
	if got[0].Key() != "user/jellyfin.service" || got[0].Label() != "jellyfin.service (user)" {
		t.Errorf("Key/Label = %q %q", got[0].Key(), got[0].Label())
	}
	if u, err := Tracked(filepath.Join(t.TempDir(), "none.json")); err != nil || u != nil {
		t.Errorf("missing ServiceMan config: %v, %v", u, err)
	}
}

func TestUpstream(t *testing.T) {
	for in, want := range map[string]string{
		"http://localhost:8096/health": "localhost:8096",
		"http://127.0.0.1/":            "127.0.0.1:80",
		"https://nas:5001/x":           "https://nas:5001",
		"":                             "",
		"localhost:80":                 "",
	} {
		if got := Upstream(in); got != want {
			t.Errorf("Upstream(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseShow(t *testing.T) {
	got := parseShow("LoadState=loaded\nActiveState=active\nSubState=running\n\nLoadState=not-found\nActiveState=inactive\nSubState=dead\n")
	want := []State{{Load: "loaded", Active: "active", Sub: "running"}, {Load: "not-found", Active: "inactive", Sub: "dead"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseShow = %+v", got)
	}
}
