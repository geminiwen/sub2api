//go:build unit

// Package tlsfingerprint provides TLS fingerprint simulation for HTTP clients.
//
// Unit tests for TLS fingerprint dialer.
// Integration tests that require external network are in dialer_integration_test.go
// and require the 'integration' build tag.
//
// Run unit tests: go test -v ./internal/pkg/tlsfingerprint/...
// Run integration tests: go test -v -tags=integration ./internal/pkg/tlsfingerprint/...
package tlsfingerprint

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	utls "github.com/refraction-networking/utls"
)

// TestDialerBasicConnection tests that the dialer can establish TLS connections.
func TestDialerBasicConnection(t *testing.T) {
	skipNetworkTest(t)

	// Create a dialer with default profile
	profile := &Profile{
		Name:         "Test Profile",
		EnableGREASE: false,
	}
	dialer := NewDialer(profile, nil)

	// Create HTTP client with custom TLS dialer
	client := &http.Client{
		Transport: &http.Transport{
			DialTLSContext: dialer.DialTLSContext,
		},
		Timeout: 30 * time.Second,
	}

	// Make a request to a known HTTPS endpoint
	resp, err := client.Get("https://www.google.com")
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

// TestJA3Fingerprint verifies the JA3 fingerprint matches the Bun-based default profile.
// This test uses tls.peet.ws to verify the fingerprint.
// Expected JA3 hash: 44f88fca027f27bab4bb08d4af15f23e (Claude Code native 2.1.80 / Bun)
func TestJA3Fingerprint(t *testing.T) {
	skipNetworkTest(t)

	profile := &Profile{
		Name:         "Claude Code Native Test",
		EnableGREASE: false,
	}
	dialer := NewDialer(profile, nil)

	client := &http.Client{
		Transport: &http.Transport{
			DialTLSContext: dialer.DialTLSContext,
		},
		Timeout: 30 * time.Second,
	}

	// Use tls.peet.ws fingerprint detection API
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", "https://tls.peet.ws/api/all", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("User-Agent", "Claude Code/2.0.0 Node.js/20.0.0")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to get fingerprint: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	var fpResp FingerprintResponse
	if err := json.Unmarshal(body, &fpResp); err != nil {
		t.Logf("Response body: %s", string(body))
		t.Fatalf("failed to parse fingerprint response: %v", err)
	}

	// Log all fingerprint information
	t.Logf("JA3: %s", fpResp.TLS.JA3)
	t.Logf("JA3 Hash: %s", fpResp.TLS.JA3Hash)
	t.Logf("JA4: %s", fpResp.TLS.JA4)
	t.Logf("PeetPrint: %s", fpResp.TLS.PeetPrint)
	t.Logf("PeetPrint Hash: %s", fpResp.TLS.PeetPrintHash)

	// Verify JA3 hash matches expected value
	expectedJA3Hash := "44f88fca027f27bab4bb08d4af15f23e"
	if fpResp.TLS.JA3Hash == expectedJA3Hash {
		t.Logf("✓ JA3 hash matches expected value: %s", expectedJA3Hash)
	} else {
		t.Errorf("✗ JA3 hash mismatch: got %s, expected %s", fpResp.TLS.JA3Hash, expectedJA3Hash)
	}

	expectedJA3 := "771,4865-4866-4867-49195-49199-49196-49200-52393-52392-49161-49171-49162-49172-156-157-47-53,0-65037-23-65281-10-11-35-16-5-13-18-51-45-43,29-23-24,0"
	if fpResp.TLS.JA3 == expectedJA3 {
		t.Logf("✓ JA3 string matches expected value")
	} else {
		t.Errorf("✗ JA3 string mismatch: got %s, expected %s", fpResp.TLS.JA3, expectedJA3)
	}

	expectedExtensions := "0-65037-23-65281-10-11-35-16-5-13-18-51-45-43"
	if strings.Contains(fpResp.TLS.JA3, expectedExtensions) {
		t.Logf("✓ JA3 contains expected extension list: %s", expectedExtensions)
	} else {
		t.Errorf("✗ JA3 extension list mismatch: got %s", fpResp.TLS.JA3)
	}
}

func skipNetworkTest(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过网络测试（short 模式）")
	}
	if os.Getenv("TLSFINGERPRINT_NETWORK_TESTS") != "1" {
		t.Skip("跳过网络测试（需要设置 TLSFINGERPRINT_NETWORK_TESTS=1）")
	}
}

// TestDialerWithProfile tests that different profiles produce different fingerprints.
func TestDialerWithProfile(t *testing.T) {
	// Create two dialers with different profiles
	profile1 := &Profile{
		Name:         "Profile 1 - No GREASE",
		EnableGREASE: false,
	}
	profile2 := &Profile{
		Name:         "Profile 2 - With GREASE",
		EnableGREASE: true,
	}

	dialer1 := NewDialer(profile1, nil)
	dialer2 := NewDialer(profile2, nil)

	// Build specs and compare
	// Note: We can't directly compare JA3 without making network requests
	// but we can verify the specs are different
	spec1 := dialer1.buildClientHelloSpec()
	spec2 := dialer2.buildClientHelloSpec()

	// Profile with GREASE should have more extensions
	if len(spec2.Extensions) <= len(spec1.Extensions) {
		t.Error("expected GREASE profile to have more extensions")
	}
}

// TestHTTPProxyDialerBasic tests HTTP proxy dialer creation.
// Note: This is a unit test - actual proxy testing requires a proxy server.
func TestHTTPProxyDialerBasic(t *testing.T) {
	profile := &Profile{
		Name:         "Test Profile",
		EnableGREASE: false,
	}

	// Test that dialer is created without panic
	proxyURL := mustParseURL("http://proxy.example.com:8080")
	dialer := NewHTTPProxyDialer(profile, proxyURL)

	if dialer == nil {
		t.Fatal("expected dialer to be created")
	}
	if dialer.profile != profile {
		t.Error("expected profile to be set")
	}
	if dialer.proxyURL != proxyURL {
		t.Error("expected proxyURL to be set")
	}
}

// TestSOCKS5ProxyDialerBasic tests SOCKS5 proxy dialer creation.
// Note: This is a unit test - actual proxy testing requires a proxy server.
func TestSOCKS5ProxyDialerBasic(t *testing.T) {
	profile := &Profile{
		Name:         "Test Profile",
		EnableGREASE: false,
	}

	// Test that dialer is created without panic
	proxyURL := mustParseURL("socks5://proxy.example.com:1080")
	dialer := NewSOCKS5ProxyDialer(profile, proxyURL)

	if dialer == nil {
		t.Fatal("expected dialer to be created")
	}
	if dialer.profile != profile {
		t.Error("expected profile to be set")
	}
	if dialer.proxyURL != proxyURL {
		t.Error("expected proxyURL to be set")
	}
}

// TestBuildClientHelloSpec tests ClientHello spec construction.
func TestBuildClientHelloSpec(t *testing.T) {
	// Test with nil profile (should use defaults)
	spec := buildClientHelloSpecFromProfile(nil)

	if len(spec.CipherSuites) == 0 {
		t.Error("expected cipher suites to be set")
	}
	if len(spec.Extensions) == 0 {
		t.Error("expected extensions to be set")
	}

	// Verify default cipher suites are used
	if len(spec.CipherSuites) != len(defaultCipherSuites) {
		t.Errorf("expected %d cipher suites, got %d", len(defaultCipherSuites), len(spec.CipherSuites))
	}

	if len(spec.CompressionMethods) != 1 || spec.CompressionMethods[0] != 0 {
		t.Fatalf("expected null compression only, got %v", spec.CompressionMethods)
	}

	// Test with custom profile
	customProfile := &Profile{
		Name:         "Custom",
		EnableGREASE: false,
		CipherSuites: []uint16{0x1301, 0x1302},
	}
	spec = buildClientHelloSpecFromProfile(customProfile)

	if len(spec.CipherSuites) != 2 {
		t.Errorf("expected 2 cipher suites, got %d", len(spec.CipherSuites))
	}
}

func TestApplyPresetWithCapturedSessionID(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()
	defer func() { _ = serverConn.Close() }()

	tlsConn := utls.UClient(clientConn, &utls.Config{ServerName: "example.com"}, utls.HelloCustom)
	spec := buildClientHelloSpecFromProfile(nil)

	if err := applyPresetWithCapturedSessionID(tlsConn, spec); err != nil {
		t.Fatalf("apply preset failed: %v", err)
	}
	if tlsConn.HandshakeState.Hello == nil {
		t.Fatal("expected public ClientHello to be populated")
	}
	if got := len(tlsConn.HandshakeState.Hello.SessionId); got != 32 {
		t.Fatalf("session ID length = %d, want 32", got)
	}
}

func TestBuildClientHelloSpec_DefaultShapeMatchesCapturedFingerprint(t *testing.T) {
	spec := buildClientHelloSpecFromProfile(nil)

	expectedCipherSuites := []uint16{
		0x1301, 0x1302, 0x1303,
		0xc02b, 0xc02f, 0xc02c, 0xc030,
		0xcca9, 0xcca8,
		0xc009, 0xc013, 0xc00a, 0xc014,
		0x009c, 0x009d, 0x002f, 0x0035,
	}
	if !reflect.DeepEqual(spec.CipherSuites, expectedCipherSuites) {
		t.Fatalf("cipher suites mismatch: got %v, want %v", spec.CipherSuites, expectedCipherSuites)
	}

	expectedCurves := []utls.CurveID{utls.X25519, utls.CurveP256, utls.CurveP384}
	curvesExt, ok := spec.Extensions[4].(*utls.SupportedCurvesExtension)
	if !ok {
		t.Fatalf("extension 4 = %T, want *utls.SupportedCurvesExtension", spec.Extensions[4])
	}
	if !reflect.DeepEqual(curvesExt.Curves, expectedCurves) {
		t.Fatalf("supported curves mismatch: got %v, want %v", curvesExt.Curves, expectedCurves)
	}

	pointsExt, ok := spec.Extensions[5].(*utls.SupportedPointsExtension)
	if !ok {
		t.Fatalf("extension 5 = %T, want *utls.SupportedPointsExtension", spec.Extensions[5])
	}
	if !reflect.DeepEqual(pointsExt.SupportedPoints, []uint8{0}) {
		t.Fatalf("point formats mismatch: got %v, want [0]", pointsExt.SupportedPoints)
	}

	sigExt, ok := spec.Extensions[9].(*utls.SignatureAlgorithmsExtension)
	if !ok {
		t.Fatalf("extension 9 = %T, want *utls.SignatureAlgorithmsExtension", spec.Extensions[9])
	}
	expectedSigAlgs := []utls.SignatureScheme{
		0x0403, 0x0804, 0x0401,
		0x0503, 0x0805, 0x0501,
		0x0806, 0x0601, 0x0201,
	}
	if !reflect.DeepEqual(sigExt.SupportedSignatureAlgorithms, expectedSigAlgs) {
		t.Fatalf("signature algorithms mismatch: got %v, want %v", sigExt.SupportedSignatureAlgorithms, expectedSigAlgs)
	}
}

func TestBuildClientHelloSpec_DefaultExtensionOrderMatchesCapturedFingerprint(t *testing.T) {
	spec := buildClientHelloSpecFromProfile(nil)

	got := make([]string, 0, len(spec.Extensions))
	for _, ext := range spec.Extensions {
		switch e := ext.(type) {
		case *utls.SNIExtension:
			got = append(got, "server_name")
		case *utls.GREASEEncryptedClientHelloExtension:
			got = append(got, "ech_grease")
			buf := make([]byte, e.Len())
			n, err := e.Read(buf)
			if err != io.EOF {
				t.Fatalf("ECH GREASE read error: %v", err)
			}
			if n != len(buf) {
				t.Fatalf("ECH GREASE length mismatch: got %d, want %d", n, len(buf))
			}
			if buf[0] != 0xfe || buf[1] != 0x0d {
				t.Fatalf("ECH GREASE extension id bytes = %02x%02x, want fe0d", buf[0], buf[1])
			}
		case *utls.ExtendedMasterSecretExtension:
			got = append(got, "extended_master_secret")
		case *utls.RenegotiationInfoExtension:
			got = append(got, "renegotiation_info")
		case *utls.SupportedCurvesExtension:
			got = append(got, "supported_groups")
		case *utls.SupportedPointsExtension:
			got = append(got, "ec_point_formats")
		case *utls.SessionTicketExtension:
			got = append(got, "session_ticket")
		case *utls.ALPNExtension:
			got = append(got, "alpn")
			if !reflect.DeepEqual(e.AlpnProtocols, []string{"http/1.1"}) {
				t.Fatalf("ALPN mismatch: got %v, want [http/1.1]", e.AlpnProtocols)
			}
		case *utls.StatusRequestExtension:
			got = append(got, "status_request")
		case *utls.SignatureAlgorithmsExtension:
			got = append(got, "signature_algorithms")
		case *utls.SCTExtension:
			got = append(got, "signed_certificate_timestamp")
		case *utls.KeyShareExtension:
			got = append(got, "key_share")
			if len(e.KeyShares) != 1 || e.KeyShares[0].Group != utls.X25519 {
				t.Fatalf("key shares mismatch: got %+v", e.KeyShares)
			}
		case *utls.PSKKeyExchangeModesExtension:
			got = append(got, "psk_key_exchange_modes")
			if !reflect.DeepEqual(e.Modes, []uint8{utls.PskModeDHE}) {
				t.Fatalf("psk modes mismatch: got %v, want [%d]", e.Modes, utls.PskModeDHE)
			}
		case *utls.SupportedVersionsExtension:
			got = append(got, "supported_versions")
			if !reflect.DeepEqual(e.Versions, []uint16{utls.VersionTLS13, utls.VersionTLS12}) {
				t.Fatalf("supported versions mismatch: got %v", e.Versions)
			}
		default:
			t.Fatalf("unexpected extension type %T", ext)
		}
	}

	expected := []string{
		"server_name",
		"ech_grease",
		"extended_master_secret",
		"renegotiation_info",
		"supported_groups",
		"ec_point_formats",
		"session_ticket",
		"alpn",
		"status_request",
		"signature_algorithms",
		"signed_certificate_timestamp",
		"key_share",
		"psk_key_exchange_modes",
		"supported_versions",
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("extension order mismatch: got %v, want %v", got, expected)
	}
}

// TestToUTLSCurves tests curve ID conversion.
func TestToUTLSCurves(t *testing.T) {
	input := []uint16{0x001d, 0x0017, 0x0018}
	result := toUTLSCurves(input)

	if len(result) != len(input) {
		t.Errorf("expected %d curves, got %d", len(input), len(result))
	}

	for i, curve := range result {
		if uint16(curve) != input[i] {
			t.Errorf("curve %d: expected 0x%04x, got 0x%04x", i, input[i], uint16(curve))
		}
	}
}

// Helper function to parse URL without error handling.
func mustParseURL(rawURL string) *url.URL {
	u, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}
	return u
}

// TestProfileExpectation defines expected fingerprint values for a profile.
type TestProfileExpectation struct {
	Profile       *Profile
	ExpectedJA3   string // Expected JA3 hash (empty = don't check)
	ExpectedJA4   string // Expected full JA4 (empty = don't check)
	JA4CipherHash string // Expected JA4 cipher hash - the stable middle part (empty = don't check)
}

// TestAllProfiles tests multiple TLS fingerprint profiles against tls.peet.ws.
// Run with: go test -v -run TestAllProfiles ./internal/pkg/tlsfingerprint/...
func TestAllProfiles(t *testing.T) {
	skipNetworkTest(t)

	// Define all profiles to test with their expected fingerprints
	// These profiles are from config.yaml gateway.tls_fingerprint.profiles
	profiles := []TestProfileExpectation{
		{
			// Claude Code native 2.1.80 / Bun
			Profile: &Profile{
				Name:         "claude_code_bun_2180",
				EnableGREASE: false,
				CipherSuites: []uint16{4865, 4866, 4867, 49195, 49199, 49196, 49200, 52393, 52392, 49161, 49171, 49162, 49172, 156, 157, 47, 53},
				Curves:       []uint16{29, 23, 24},
				PointFormats: []uint8{0},
			},
			ExpectedJA3: "44f88fca027f27bab4bb08d4af15f23e",
		},
	}

	for _, tc := range profiles {
		tc := tc // capture range variable
		t.Run(tc.Profile.Name, func(t *testing.T) {
			fp := fetchFingerprint(t, tc.Profile)
			if fp == nil {
				return // fetchFingerprint already called t.Fatal
			}

			t.Logf("Profile: %s", tc.Profile.Name)
			t.Logf("  JA3:           %s", fp.JA3)
			t.Logf("  JA3 Hash:      %s", fp.JA3Hash)
			t.Logf("  JA4:           %s", fp.JA4)
			t.Logf("  PeetPrint:     %s", fp.PeetPrint)
			t.Logf("  PeetPrintHash: %s", fp.PeetPrintHash)

			// Verify expectations
			if tc.ExpectedJA3 != "" {
				if fp.JA3Hash == tc.ExpectedJA3 {
					t.Logf("  ✓ JA3 hash matches: %s", tc.ExpectedJA3)
				} else {
					t.Errorf("  ✗ JA3 hash mismatch: got %s, expected %s", fp.JA3Hash, tc.ExpectedJA3)
				}
			}

			if tc.ExpectedJA4 != "" {
				if fp.JA4 == tc.ExpectedJA4 {
					t.Logf("  ✓ JA4 matches: %s", tc.ExpectedJA4)
				} else {
					t.Errorf("  ✗ JA4 mismatch: got %s, expected %s", fp.JA4, tc.ExpectedJA4)
				}
			}

			// Check JA4 cipher hash (stable middle part)
			// JA4 format: prefix_cipherHash_extHash
			if tc.JA4CipherHash != "" {
				if strings.Contains(fp.JA4, "_"+tc.JA4CipherHash+"_") {
					t.Logf("  ✓ JA4 cipher hash matches: %s", tc.JA4CipherHash)
				} else {
					t.Errorf("  ✗ JA4 cipher hash mismatch: got %s, expected cipher hash %s", fp.JA4, tc.JA4CipherHash)
				}
			}
		})
	}
}

// fetchFingerprint makes a request to tls.peet.ws and returns the TLS fingerprint info.
func fetchFingerprint(t *testing.T, profile *Profile) *TLSInfo {
	t.Helper()

	dialer := NewDialer(profile, nil)
	client := &http.Client{
		Transport: &http.Transport{
			DialTLSContext: dialer.DialTLSContext,
		},
		Timeout: 30 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", "https://tls.peet.ws/api/all", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
		return nil
	}
	req.Header.Set("User-Agent", "Claude Code/2.0.0 Node.js/20.0.0")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to get fingerprint: %v", err)
		return nil
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
		return nil
	}

	var fpResp FingerprintResponse
	if err := json.Unmarshal(body, &fpResp); err != nil {
		t.Logf("Response body: %s", string(body))
		t.Fatalf("failed to parse fingerprint response: %v", err)
		return nil
	}

	return &fpResp.TLS
}
