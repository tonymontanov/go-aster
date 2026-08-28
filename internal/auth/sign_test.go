/*
FILE: internal/auth/sign_test.go

DESCRIPTION:
Golden-vector tests for the Aster EIP-712 signer. The expected signatures were
generated with the reference implementation (Python eth_account 0.13.7,
Account.sign_typed_data) using the PUBLIC demonstration credentials from the
official Aster V3 API documentation — they are documentation samples, not real
secrets. Byte-for-byte equality holds because both implementations use
deterministic RFC 6979 ECDSA.
*/

package auth

import (
	"net/url"
	"strings"
	"testing"
)

// Demonstration credentials from the Aster V3 docs (public sample values).
const (
	testPrivateKey = "0x4fd0a42218f3eae43a6ce26d22544e986139a01e5b34a62db53757ffca81bae1"
	testSigner     = "0x21cF8Ae13Bb72632562c6Fff438652Ba1a151bb0"
	testUser       = "0x63DD5aCC6b1aa0f563956C0e534DD30B6dcF7C4e"
)

func newTestSigner(t *testing.T, chainID int64) *Signer {
	t.Helper()
	var s *Signer
	var err error
	s, err = NewSigner(testUser, testSigner, testPrivateKey, chainID)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	return s
}

func TestSignerDerivesAddress(t *testing.T) {
	var s *Signer
	var err error
	// Address omitted — must be derived from the private key in EIP-55 form.
	s, err = NewSigner(testUser, "", testPrivateKey, 0)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	if s.SignerAddress() != testSigner {
		t.Fatalf("derived signer = %s, want %s", s.SignerAddress(), testSigner)
	}
	if !s.Enabled() {
		t.Fatal("signer must be enabled")
	}
}

func TestSignerRejectsMismatchedAddress(t *testing.T) {
	var err error
	_, err = NewSigner(testUser, "0x0000000000000000000000000000000000000001", testPrivateKey, 0)
	if err == nil {
		t.Fatal("expected error for mismatched signer address")
	}
}

func TestSignerDisabledWithoutKey(t *testing.T) {
	var s *Signer
	var err error
	s, err = NewSigner("", "", "", 0)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	if s.Enabled() {
		t.Fatal("signer must be disabled without a private key")
	}
	if _, err = s.SignMessage("x"); err != ErrSignerDisabled {
		t.Fatalf("SignMessage error = %v, want ErrSignerDisabled", err)
	}
}

// TestSignMessageGoldenVectors — reference vectors from eth_account.
func TestSignMessageGoldenVectors(t *testing.T) {
	var vectors = []struct {
		name    string
		chainID int64
		payload string
		want    string
	}{
		{
			name:    "simple order",
			chainID: 1666,
			payload: "nonce=1748310859508867&price=0.5&quantity=20&side=BUY&signer=0x21cF8Ae13Bb72632562c6Fff438652Ba1a151bb0&symbol=ASTERUSDT&timeInForce=GTC&type=LIMIT",
			want:    "0xcb797524bc07185300e55a416ad25d2dcb443d96571c5ca0200cb65811442a635871064ba611451f0623e99df7573ce6cd204ffcc5fb21ec20e693245cbe74c31c",
		},
		{
			name:    "empty string",
			chainID: 1666,
			payload: "",
			want:    "0xb7fe1961a997fb3fab846eadbfb0cbf0297029df88d0b5f5f1f738f10f8bbd337a5baeda44397cb37248146bec810048988a5597bb841de18d298dbc1b8994201b",
		},
		{
			name:    "listen key",
			chainID: 1666,
			payload: "nonce=1700000000000000&signer=0x21cF8Ae13Bb72632632c6Fff438652Ba1a151bb0",
			want:    "0x56b9b35d45fe3e907cd9124b0a7ce5f72a065861dc6beace16398a37e923fa4548c7c2ab33ffdde7d82f8dcb1f5e5e4e492356d71b6accfcce0380210cad6c481c",
		},
		{
			name:    "urlencoded specials",
			chainID: 1666,
			payload: "a=%2B%26%3D&b=hello+world&nonce=1&signer=0xAb",
			want:    "0x041736d12da8ea395589cd92dd31f8483e53fbcc89f553cfdbd905478bc2eed74118d5b5646af7a77585285893cea6cb4fa00482903e299363cb1ff235ad041c1b",
		},
		{
			name:    "chainId 56",
			chainID: 56,
			payload: "nonce=2&signer=0xCd",
			want:    "0x9b3cf1879d80efdb81cd92669959acf78d51b7c4bb7917f4e34eea7f6cc8f2e755e73b7cf9b9f44e20ff5efc7ba71e441f81f1c26167e479ab5728a111b31cd21c",
		},
	}

	var v struct {
		name    string
		chainID int64
		payload string
		want    string
	}
	for _, v = range vectors {
		t.Run(v.name, func(t *testing.T) {
			var s *Signer = newTestSigner(t, v.chainID)
			var got string
			var err error
			got, err = s.SignMessage(v.payload)
			if err != nil {
				t.Fatalf("SignMessage: %v", err)
			}
			if got != v.want {
				t.Fatalf("signature mismatch:\n got %s\nwant %s", got, v.want)
			}
		})
	}
}

func TestSignParamsShape(t *testing.T) {
	var s *Signer = newTestSigner(t, 0)
	var params url.Values = url.Values{}
	params.Set("symbol", "BTCUSDT")
	params.Set("side", "BUY")

	var payload string
	var err error
	payload, err = s.SignParams(params)
	if err != nil {
		t.Fatalf("SignParams: %v", err)
	}
	// The signature must be the LAST parameter, appended after the sorted
	// encoded payload, so the server can strip it and verify the rest as is.
	var idx int = strings.LastIndex(payload, "&signature=0x")
	if idx < 0 {
		t.Fatalf("payload has no trailing signature: %s", payload)
	}
	var signedPart string = payload[:idx]
	if signedPart != params.Encode() {
		t.Fatalf("signed part mismatch:\n got %s\nwant %s", signedPart, params.Encode())
	}
	if params.Get("nonce") == "" || params.Get("signer") != testSigner {
		t.Fatal("SignParams must inject nonce and signer")
	}
	// 0x + 130 hex chars (r||s||v).
	if len(payload)-idx != len("&signature=")+2+130 {
		t.Fatalf("unexpected signature length in %q", payload[idx:])
	}
}

func TestNonceMonotonic(t *testing.T) {
	var g NonceGenerator
	var prev int64
	var i int
	for i = 0; i < 10000; i++ {
		var n int64 = g.Next()
		if n <= prev {
			t.Fatalf("nonce not strictly increasing: %d after %d", n, prev)
		}
		prev = n
	}
}
