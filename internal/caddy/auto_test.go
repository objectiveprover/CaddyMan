package caddy

import (
	"errors"
	"strings"
	"testing"

	"caddyman/internal/config"
)

// stubAutoPort stands in for systemd and the services' caddyman-info.
func stubAutoPort(t *testing.T, ports map[string]int) *int {
	t.Helper()
	asked := new(int)
	old := autoPort
	autoPort = func(s config.Site) (int, error) {
		*asked++
		if p, ok := ports[s.ServiceKey()]; ok && p > 0 {
			return p, nil
		}
		return 0, errors.New(s.Service + " has no port yet: start it once (ServiceMan)")
	}
	t.Cleanup(func() { autoPort = old })
	return asked
}

func TestAutoUpstream(t *testing.T) {
	c := testConfig(t)
	c.Put("", config.Site{Name: "a", Enabled: true, Kind: config.Proxy, Upstream: config.AutoUpstream, Service: "a.service", ServiceScope: "user"})
	c.Put("", config.Site{Name: "b", Enabled: true, Kind: config.Proxy, Upstream: "81"})
	c.Put("", config.Site{Name: "off", Enabled: false, Kind: config.Proxy, Upstream: config.AutoUpstream, Service: "off.service", ServiceScope: "user"})
	asked := stubAutoPort(t, map[string]int{"user/a.service": 20001})

	want, err := Desired(c)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(want["a.home.arpa.caddy"]); !strings.Contains(got, "reverse_proxy localhost:20001") {
		t.Errorf("auto site's file:\n%s", got)
	}
	if *asked != 1 {
		t.Errorf("asked %d services, want 1 (the disabled one isn't asked)", *asked)
	}
	if c.Find("a.home.arpa").Upstream != config.AutoUpstream {
		t.Error("Resolve changed the config itself")
	}

	// Saved: the config keeps auto, the file has the port.
	if _, err := SaveLocal(c, nil); err != nil {
		t.Fatal(err)
	}

	// No port: nothing is written, and the error says which site and why.
	stubAutoPort(t, nil)
	_, err = SaveLocal(c, nil)
	if err == nil || !strings.Contains(err.Error(), "a.home.arpa: Upstream is auto, but a.service has no port yet") {
		t.Errorf("SaveLocal without a port = %v", err)
	}
	if _, err := Pending(c); err == nil {
		t.Error("Pending without a port didn't fail")
	}
}
