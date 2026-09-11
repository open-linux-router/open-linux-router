package ingress

import (
	"crypto/x509"
	"encoding/pem"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Certificate observation — the one thing this module owns that decays on a
// clock (docs/ingress.md §8).
//
// Everything else in olr is applied or not. A certificate is applied and then,
// ninety days later, stops being valid, and the failure is distributed in time
// in the nastiest possible way: renewal happens well before expiry, so the
// first failed renewal costs nothing and the tenth costs nothing. By the time a
// browser complains, the cause is a month old. That is why this is an active
// check in `status` and not a line in Caddy's log.
//
// **Read by parsing certificates, not by walking a known path.** Caddy's
// storage layout is certmagic's and is not a contract we are party to —
// `certificates/<issuer>/<subject>/<subject>.crt` today, something else after
// an upgrade. Globbing for `*.crt` and asking x509 what is in it depends on
// none of that, and the answer comes from the certificate itself rather than
// from metadata about it.

// IssuedCert is one certificate found in the proxy's storage.
type IssuedCert struct {
	// Path is where it was found, for an operator who needs to look.
	Path string `json:"path"`

	// Names are the identities it covers, wildcards included.
	Names []string `json:"names"`

	// NotBefore doubles as "when this was last issued", which is the closest
	// thing to a last-successful-renewal timestamp that exists without trusting
	// a metadata format. A renewal replaces the file, so this moves.
	NotBefore time.Time `json:"issued_at"`

	// NotAfter is when it stops being accepted.
	NotAfter time.Time `json:"expires_at"`
}

// ReadCertificates returns every certificate under the proxy's data directory.
//
// A missing directory is not an error: it is what a box looks like before the
// first issuance, and reporting it as a failure would make a healthy
// just-enabled module look broken.
func ReadCertificates(dataDir string) ([]IssuedCert, error) {
	var out []IssuedCert

	err := filepath.WalkDir(dataDir, func(path string, e fs.DirEntry, err error) error {
		switch {
		case os.IsNotExist(err):
			return nil
		case err != nil:
			// One unreadable subtree must not hide the certificates in the
			// others — a permissions problem somewhere in Caddy's storage is
			// not a reason to report "no certificate".
			return nil
		case e.IsDir() || !strings.HasSuffix(e.Name(), ".crt"):
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if cert := parseFirstCert(data); cert != nil {
			out = append(out, IssuedCert{
				Path:      path,
				Names:     certNames(cert),
				NotBefore: cert.NotBefore,
				NotAfter:  cert.NotAfter,
			})
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return out, nil
}

// parseFirstCert reads the leaf out of a PEM chain.
//
// The leaf is the first block; the rest is the issuing chain, whose expiry is
// the CA's problem and not ours. Returning nil rather than an error for
// anything unparseable is deliberate — a stray file in a storage directory we
// do not own is not a fault to report.
func parseFirstCert(data []byte) *x509.Certificate {
	for len(data) > 0 {
		block, rest := pem.Decode(data)
		if block == nil {
			return nil
		}
		if block.Type == "CERTIFICATE" {
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil
			}
			return cert
		}
		data = rest
	}
	return nil
}

func certNames(cert *x509.Certificate) []string {
	names := append([]string{}, cert.DNSNames...)
	if len(names) == 0 && cert.Subject.CommonName != "" {
		names = append(names, cert.Subject.CommonName)
	}
	return names
}

// Covers reports whether this certificate would be accepted for name.
//
// Wildcard matching is the whole reason this exists: the certificate we obtain
// is for `*.home.example.com` and the names we publish are
// `grafana.home.example.com`, so a plain string compare would report that
// nothing is covered while everything works.
func (c IssuedCert) Covers(name string) bool {
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	for _, n := range c.Names {
		n = strings.ToLower(strings.TrimSuffix(n, "."))
		if n == name {
			return true
		}
		// One label, exactly — the same rule §1 enforces at write time.
		if suffix, ok := strings.CutPrefix(n, "*."); ok {
			if rest, found := strings.CutSuffix(name, "."+suffix); found &&
				rest != "" && !strings.Contains(rest, ".") {
				return true
			}
		}
	}
	return false
}

// CertState is the answer `status` gives about the certificate.
type CertState struct {
	// Found reports whether any certificate covering the published names
	// exists yet. False on a box where issuance has not completed, which is a
	// normal transient state and not a fault.
	Found bool `json:"found"`

	// Cert is the covering certificate, when there is one.
	Cert *IssuedCert `json:"certificate,omitempty"`

	// ExpiresIn is how long it has left. Negative means it has expired, which
	// is worth being able to say rather than clamping to zero.
	ExpiresIn time.Duration `json:"-"`

	// ExpiresInDays is the same number in the unit an operator thinks in.
	ExpiresInDays int `json:"expires_in_days,omitempty"`

	// RenewalOverdue reports that the proxy should already have renewed this
	// and has not.
	//
	// This is the check the whole file exists for. Caddy renews once roughly
	// two thirds of the lifetime has elapsed, so a certificate still inside its
	// final third is one whose renewal has been failing — silently, for as long
	// as it has been in that window. Saying so turns a problem that surfaces as
	// "the internet is broken" in three weeks into one an operator can fix
	// today.
	RenewalOverdue bool `json:"renewal_overdue"`
}

// renewalThreshold is the fraction of a certificate's lifetime that may remain
// before renewal should have happened. Caddy's own trigger point.
const renewalThreshold = 1.0 / 3.0

// CertificateState picks the certificate covering the published names and
// reports on it.
//
// It answers about the *wildcard* rather than about every name, because there
// is only ever one certificate: that is what §1's claim — adding a service
// involves no certificate step — actually rests on.
func CertificateState(certs []IssuedCert, domain string, now time.Time) CertState {
	probe := qualify("olr-probe", domain)

	var best *IssuedCert
	for i := range certs {
		if !certs[i].Covers(probe) {
			continue
		}
		// The longest-lived match. A storage directory can hold a superseded
		// certificate next to its replacement, and reporting the old one would
		// invent an expiry scare that the running proxy does not have.
		if best == nil || certs[i].NotAfter.After(best.NotAfter) {
			best = &certs[i]
		}
	}
	if best == nil {
		return CertState{}
	}

	remaining := best.NotAfter.Sub(now)
	lifetime := best.NotAfter.Sub(best.NotBefore)

	state := CertState{
		Found:         true,
		Cert:          best,
		ExpiresIn:     remaining,
		ExpiresInDays: int(remaining.Hours() / 24),
	}
	if lifetime > 0 && float64(remaining) < float64(lifetime)*renewalThreshold {
		state.RenewalOverdue = true
	}
	return state
}
