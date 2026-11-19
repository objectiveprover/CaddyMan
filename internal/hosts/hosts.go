// Package hosts makes /etc/hosts lines for the sites, for devices to find
// them until a local DNS server does.
package hosts

import (
	"fmt"
	"net"

	"caddyman/internal/config"
)

// LanIP is this machine's address on the network: override when set,
// otherwise the source address of the default route. The UDP "dial" sends
// nothing; it only asks the kernel which address it would use.
func LanIP(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	conn, err := net.Dial("udp4", "1.1.1.1:80")
	if err != nil {
		return "", fmt.Errorf("find this machine's LAN address (set lan_ip in the config): %w", err)
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String(), nil
}

// Lines are "<ip>  <address>" for every enabled site, sorted by address.
func Lines(c *config.Config, ip string) []string {
	var out []string
	for _, s := range c.Sorted() {
		if s.Enabled {
			out = append(out, ip+"  "+c.Address(s))
		}
	}
	return out
}
