package hosts

import (
	"reflect"
	"testing"

	"caddyman/internal/config"
)

func TestLines(t *testing.T) {
	c := &config.Config{DomainSuffix: "home.arpa", Sites: []config.Site{
		{Name: "b", Enabled: true}, {Name: "off", Enabled: false}, {Name: "a.lan", Enabled: true},
	}}
	got := Lines(c, "192.168.1.10")
	want := []string{"192.168.1.10  a.lan", "192.168.1.10  b.home.arpa"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Lines = %q, want %q", got, want)
	}
	if ip, _ := LanIP("10.0.0.2"); ip != "10.0.0.2" {
		t.Errorf("LanIP override = %q", ip)
	}
}
