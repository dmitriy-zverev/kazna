package security

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// ErrInvalidURL marks a URL that must not be stored or called.
var ErrInvalidURL = errors.New("security: invalid url")

// OutboundURLOptions controls how strict URL validation is. Development and
// staging relax the rules so a local webhook sink can be registered; production
// must not (docs/VERIFICATION_HARNESS.md, docs/SECURITY.md §4).
type OutboundURLOptions struct {
	// AllowHTTP permits the http scheme. Production requires https.
	AllowHTTP bool
	// AllowPrivate permits loopback, private and single-label hosts. Production
	// forbids them to prevent SSRF against internal services.
	AllowPrivate bool
}

// ValidateOutboundURL rejects URLs the service must never call or store: wrong
// scheme, embedded credentials, or hosts that resolve to internal addresses.
//
// Validation at write time is necessary but not sufficient on its own — the
// delivery worker must re-validate the resolved address before each request to
// close the DNS-rebinding window (docs/SECURITY.md §4).
func ValidateOutboundURL(raw string, opts OutboundURLOptions) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("%w: url is empty", ErrInvalidURL)
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}

	switch parsed.Scheme {
	case "https":
	case "http":
		if !opts.AllowHTTP {
			return fmt.Errorf("%w: https is required", ErrInvalidURL)
		}
	default:
		return fmt.Errorf("%w: scheme must be http or https", ErrInvalidURL)
	}

	if parsed.User != nil {
		return fmt.Errorf("%w: credentials in the url are not allowed", ErrInvalidURL)
	}

	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("%w: host is required", ErrInvalidURL)
	}

	if opts.AllowPrivate {
		return nil
	}

	if isInternalHostname(host) {
		return fmt.Errorf("%w: %q is an internal host", ErrInvalidURL, host)
	}

	ips, err := resolveHost(host)
	if err != nil {
		return fmt.Errorf("%w: cannot resolve %q", ErrInvalidURL, host)
	}
	for _, ip := range ips {
		if isInternalIP(ip) {
			return fmt.Errorf("%w: %q resolves to the internal address %s", ErrInvalidURL, host, ip)
		}
	}
	return nil
}

func isInternalHostname(host string) bool {
	lower := strings.ToLower(strings.TrimSuffix(host, "."))
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return true
	}
	if strings.HasSuffix(lower, ".local") || strings.HasSuffix(lower, ".internal") {
		return true
	}
	// A single-label name such as "webhook-sink" is an internal service name,
	// not a public host.
	return !strings.Contains(lower, ".")
}

func isInternalIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsMulticast() ||
		ip.IsInterfaceLocalMulticast()
}

func resolveHost(host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		ips = append(ips, addr.IP)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no addresses for %q", host)
	}
	return ips, nil
}
