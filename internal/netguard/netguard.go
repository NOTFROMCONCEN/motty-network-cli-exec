// Package netguard is the intranet mode: one process-wide switch the workbench
// owns, and the checks the internet-facing features run before they contact a
// host. When the mode is off nothing here changes a request. When it is on, a
// feature that calls Check with a host that is neither this computer nor a
// private-network address gets ErrBlocked. The session features — SSH, TELNET,
// RDP, VNC and the forwards that carry them — do not call it at all: a host
// the user configured keeps working wherever it lives, so the mode can never
// cut a session off.
package netguard

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
)

// ErrBlocked is what Check reports when the mode is on and the host is not a
// local or private one. Callers surface it as their own failure, so a blocked
// update check or sync reads as a refusal, not as a network fault.
var ErrBlocked = errors.New("blocked by intranet mode")

var enabled atomic.Bool

// SetEnabled turns the intranet mode on or off for the whole process. It is
// safe to call from any goroutine.
func SetEnabled(on bool) { enabled.Store(on) }

// Enabled reports whether the intranet mode is on.
func Enabled() bool { return enabled.Load() }

// Check reports ErrBlocked when the mode is on and host is not a local or
// private address. host is a host name or an IP literal without a port; an
// empty host allows, since there is nothing to judge. A public name fails
// without a lookup, so the mode never sends the resolver a question about it.
func Check(host string) error {
	if !enabled.Load() {
		return nil
	}
	if Allowed(host) {
		return nil
	}
	return fmt.Errorf("%w: %s is not a local or private address", ErrBlocked, host)
}

// CheckAddress is Check for a host:port pair, as a dialer sees it.
func CheckAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	return Check(host)
}

// CheckURL is Check for the host of a web address. A URL that does not parse
// allows only when the mode is off: a feature that cannot say where it is
// going has no business going anywhere while the mode is on.
func CheckURL(raw string) error {
	if !enabled.Load() {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return fmt.Errorf("%w: %q does not name a host", ErrBlocked, raw)
	}
	return Check(parsed.Hostname())
}

// Allowed reports whether host can be reached without leaving this computer
// or the local network. A literal loopback, link-local, private or unspecified
// address passes, and so does a name that only a local network would answer:
// a single label such as "nas", which the system's search domains resolve,
// and the reserved local suffixes .local, .lan, .internal and .home.arpa.
// Anything else — a public name or a public address — fails.
func Allowed(host string) bool {
	host = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(host), ".")))
	if host == "" {
		return true
	}
	// An IPv6 zone belongs to this machine's interface, never to the internet.
	if index := strings.IndexByte(host, '%'); index >= 0 {
		host = host[:index]
	}
	// A name with no dot is this machine or a name the local resolver's search
	// domains complete; it cannot be a public host.
	if host == "localhost" || !strings.Contains(host, ".") {
		return true
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return allowedAddr(addr.Unmap())
	}
	for _, suffix := range []string{".local", ".lan", ".internal", ".home.arpa"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// allowedAddr judges an IP literal: the machine itself, its link-local
// neighborhoods, and the ranges set aside for private networks pass.
func allowedAddr(addr netip.Addr) bool {
	if !addr.IsValid() {
		return false
	}
	return addr.IsLoopback() || addr.IsUnspecified() ||
		addr.IsLinkLocalUnicast() || addr.IsPrivate()
}
