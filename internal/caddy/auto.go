package caddy

import (
	"fmt"
	"strconv"

	"caddyman/internal/config"
	"caddyman/internal/serviceinfo"
	"caddyman/internal/services"
)

// infoCache keeps services' caddyman-info answers between saves, so
// resolving auto sites runs each binary again only when it or its config
// file changed.
var infoCache serviceinfo.Cache

// autoPort is the port the service behind auto site s is configured with,
// from its caddyman-info. A variable so tests can stand in for systemd.
var autoPort = func(s config.Site) (int, error) {
	u := services.Unit{Name: s.Service, Scope: s.ServiceScope}
	bin := services.Binary(u)
	if bin == "" {
		return 0, fmt.Errorf("can't find the program behind %s (%s)", s.Service, s.ServiceScope)
	}
	info, err := infoCache.Query(bin)
	if err != nil {
		return 0, fmt.Errorf("%s doesn't answer %s (%v)", s.Service, serviceinfo.Command, err)
	}
	if info.Port == nil || *info.Port <= 0 {
		return 0, fmt.Errorf("%s has no port yet: start it once (ServiceMan)", s.Service)
	}
	return *info.Port, nil
}

// Resolve is c with each enabled auto site's Upstream replaced by
// localhost:<its service's port>. c itself is left as it is.
func Resolve(c *config.Config) (*config.Config, error) {
	out := *c
	out.Sites = append([]config.Site(nil), c.Sites...)
	for i, s := range out.Sites {
		if !s.Enabled || !s.IsAuto() {
			continue
		}
		port, err := autoPort(s)
		if err != nil {
			return nil, fmt.Errorf("%s: Upstream is auto, but %w", c.Address(s), err)
		}
		out.Sites[i].Upstream = "localhost:" + strconv.Itoa(port)
	}
	return &out, nil
}
