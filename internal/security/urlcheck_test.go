package security

import (
	"errors"
	"strings"
	"testing"
)

var productionOpts = OutboundURLOptions{AllowHTTP: false, AllowPrivate: false}
var developmentOpts = OutboundURLOptions{AllowHTTP: true, AllowPrivate: true}

func TestValidateOutboundURLProductionRejects(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"empty", ""},
		{"whitespace", "   "},
		{"no scheme", "example.com/hook"},
		{"ftp scheme", "ftp://example.com/hook"},
		{"plain http", "http://93.184.216.34/hook"},
		{"loopback v4", "https://127.0.0.1/hook"},
		{"loopback v6", "https://[::1]/hook"},
		{"unspecified", "https://0.0.0.0/hook"},
		{"private 10/8", "https://10.1.2.3/hook"},
		{"private 192.168", "https://192.168.1.5/hook"},
		{"private 172.16", "https://172.16.0.9/hook"},
		{"link local", "https://169.254.169.254/latest/meta-data"},
		{"localhost", "https://localhost/hook"},
		{"localhost subdomain", "https://api.localhost/hook"},
		{"dot local", "https://printer.local/hook"},
		{"single label", "https://webhook-sink:8080/hook"},
		{"credentials", "https://user:pass@example.com/hook"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateOutboundURL(tc.url, productionOpts); !errors.Is(err, ErrInvalidURL) {
				t.Fatalf("ValidateOutboundURL(%q) = %v, want ErrInvalidURL", tc.url, err)
			}
		})
	}
}

func TestValidateOutboundURLProductionAccepts(t *testing.T) {
	// Public IP literals need no DNS lookup, which keeps this test hermetic.
	for _, raw := range []string{
		"https://93.184.216.34/hook",
		"https://93.184.216.34:8443/path?query=1",
	} {
		if err := ValidateOutboundURL(raw, productionOpts); err != nil {
			t.Fatalf("ValidateOutboundURL(%q) = %v, want nil", raw, err)
		}
	}
}

func TestValidateOutboundURLDevelopmentRelaxes(t *testing.T) {
	for _, raw := range []string{
		"http://webhook-sink:8080/payment-events",
		"http://localhost:3000/hook",
		"http://127.0.0.1:9000/hook",
	} {
		if err := ValidateOutboundURL(raw, developmentOpts); err != nil {
			t.Fatalf("ValidateOutboundURL(%q) = %v, want nil", raw, err)
		}
	}

	// Even in development the scheme must still be sane.
	if err := ValidateOutboundURL("ftp://localhost/hook", developmentOpts); !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("ftp url accepted in development: %v", err)
	}
}

func TestValidateOutboundURLCannotResolveIsRejected(t *testing.T) {
	// A syntactically public hostname that does not exist must not be stored:
	// we cannot prove where it points.
	err := ValidateOutboundURL("https://this-host-does-not-exist-"+strings.Repeat("x", 40)+".invalid/hook", productionOpts)
	if !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("unresolvable host: err = %v, want ErrInvalidURL", err)
	}
}
