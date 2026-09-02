/*
FILE: config_test.go

DESCRIPTION:
Tests for Config defaults: the EIP-712 chainId must follow the environment
(production 1666, testnet 714) unless pinned explicitly, and endpoints must
switch with Config.Testnet without overriding explicit URLs.
*/

package aster

import "testing"

func TestWithDefaultsChainID(t *testing.T) {
	var cases = []struct {
		name    string
		cfg     Config
		wantCID int64
	}{
		{name: "production default", cfg: Config{}, wantCID: DefaultChainID},
		{name: "testnet default", cfg: Config{Testnet: true}, wantCID: TestnetChainID},
		{name: "explicit wins on testnet", cfg: Config{Testnet: true, ChainID: 56}, wantCID: 56},
		{name: "explicit wins on production", cfg: Config{ChainID: 56}, wantCID: 56},
	}
	var tc struct {
		name    string
		cfg     Config
		wantCID int64
	}
	for _, tc = range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got Config = tc.cfg.withDefaults()
			if got.ChainID != tc.wantCID {
				t.Fatalf("ChainID = %d, want %d", got.ChainID, tc.wantCID)
			}
		})
	}
	if DefaultChainID != 1666 || TestnetChainID != 714 {
		t.Fatalf("chainId constants drifted: %d / %d", DefaultChainID, TestnetChainID)
	}
}

func TestWithDefaultsEndpoints(t *testing.T) {
	var prod Config = Config{}.withDefaults()
	if prod.REST.FuturesBaseURL != DefaultFuturesRestURL || prod.WS.FuturesURL != DefaultFuturesWsURL {
		t.Fatalf("production endpoints: %s / %s", prod.REST.FuturesBaseURL, prod.WS.FuturesURL)
	}
	var test Config = Config{Testnet: true}.withDefaults()
	if test.REST.FuturesBaseURL != TestnetFuturesRestURL || test.WS.FuturesURL != TestnetFuturesWsURL {
		t.Fatalf("testnet endpoints: %s / %s", test.REST.FuturesBaseURL, test.WS.FuturesURL)
	}
	var pinned Config = Config{Testnet: true, REST: RestConfig{FuturesBaseURL: "http://127.0.0.1:1"}}.withDefaults()
	if pinned.REST.FuturesBaseURL != "http://127.0.0.1:1" {
		t.Fatalf("explicit REST URL overridden: %s", pinned.REST.FuturesBaseURL)
	}
}

func TestNewClientRejectsMismatchedSigner(t *testing.T) {
	// Public demo credentials from the official docs; the signer address
	// deliberately does not match the key.
	var _, err = NewClient(Config{
		Signer:     "0x63DD5aCC6b1aa0f563956C0e534DD30B6dcF7C4e",
		PrivateKey: "0x4fd0a42218f3eae43a6ce26d22544e986139a01e5b34a62db53757ffca81bae1",
	})
	if err == nil {
		t.Fatal("NewClient must reject a signer that does not match the private key")
	}
	if !IsAuth(err) {
		t.Fatalf("error kind = %v, want auth", err)
	}
}
