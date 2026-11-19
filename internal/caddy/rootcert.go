package caddy

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"time"
)

// RootCert describes the root certificate of Caddy's internal CA.
type RootCert struct {
	Subject  string
	SHA256   string // colon-separated, as browsers show it
	NotAfter time.Time
}

// ReadRootCert reads the PEM root certificate at path.
func ReadRootCert(path string) (RootCert, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return RootCert{}, err
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return RootCert{}, fmt.Errorf("%s isn't a PEM certificate", path)
	}
	cert, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		return RootCert{}, err
	}
	sum := sha256.Sum256(cert.Raw)
	hexes := make([]string, len(sum))
	for i, x := range sum {
		hexes[i] = fmt.Sprintf("%02X", x)
	}
	return RootCert{Subject: cert.Subject.CommonName, SHA256: strings.Join(hexes, ":"), NotAfter: cert.NotAfter}, nil
}
