/*
FILE: internal/auth/sign.go

DESCRIPTION:
sign.go implements request signing for the Aster V3 REST API (API Wallet /
Agent model). Algorithm per the official documentation:

 1. Collect all business parameters as strings.
 2. Add `nonce` (current time in microseconds) and `signer` (API wallet
    address). Regular TRADE/USER_DATA/USER_STREAM endpoints do NOT include
    `user`; only sub-account management endpoints do (out of v1 scope).
 3. URL-encode the parameter set sorted by key (ASCII order) — this exact
    string is both the signing payload and the wire payload.
 4. Wrap the string into the fixed EIP-712 typed data (see eip712.go) and
    sign the digest with ECDSA/secp256k1 using the API wallet private key.
 5. Append `&signature=0x<r||s||v hex>` to the encoded string. The signature
    is appended LAST and never re-sorted into the middle of the payload, so
    the server can strip it and verify the remaining string as received.

MAIN FUNCTIONS:
  - NewSigner(user, signerAddr, privateKeyHex, chainID): Signer factory; an
    empty private key creates a disabled signer (public endpoints only).
  - (Signer).SignParams(params): adds nonce+signer, encodes, signs; returns
    the final wire string "k=v&...&signature=0x...".
  - (Signer).SignMessage(msg): low-level EIP-712 signature over an arbitrary
    payload string (exposed for tests and future guarded/sub-account flows).

SECURITY NOTES:
  - The private key is stored inside Signer and is never serialized. String()
    returns a redacted representation.
  - Do not log signing payloads together with signatures at debug level in
    user applications; the payload itself contains no secrets, but noise
    invites accidental credential pasting.

DEPENDENCIES:
- github.com/decred/dcrd/dcrec/secp256k1/v4: deterministic (RFC 6979) ECDSA —
  byte-compatible with the reference eth_account implementation.
*/

package auth

import (
	"encoding/hex"
	"errors"
	"net/url"
	"strconv"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
	secpecdsa "github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

// ErrSignerDisabled is returned when a signing operation is requested but the
// signer was constructed without a private key.
var ErrSignerDisabled = errors.New("auth: signer is disabled (private key not configured)")

// Signer — Aster V3 request signer. Safe for concurrent use: all fields are
// read-only after construction except the internal nonce counter, which is
// lock-free.
type Signer struct {
	user    string
	signer  string
	privKey *secp256k1.PrivateKey
	domain  eip712Domain
	nonce   NonceGenerator
	enabled bool
}

/*
NewSigner creates a Signer.

Parameters:
  - user:          master account wallet address ("0x..."). Stored for
    endpoints that require it (sub-account management); regular trading
    endpoints identify the account via signer alone. May be empty.
  - signerAddr:    API wallet (agent) address. Optional: when empty it is
    derived from the private key. When set it is validated against the
    derived address — a mismatch returns an error immediately instead of
    producing opaque -1022 INVALID_SIGNATURE responses later.
  - privateKeyHex: API wallet private key, hex with optional 0x prefix.
    Empty → the signer is disabled and only public endpoints work.
  - chainID:       EIP-712 domain chainId. 0 → DefaultChainID (1666).
*/
func NewSigner(user, signerAddr, privateKeyHex string, chainID int64) (*Signer, error) {
	if chainID == 0 {
		chainID = DefaultChainID
	}
	var s *Signer = &Signer{
		user:   user,
		domain: newEIP712Domain(chainID),
	}
	if privateKeyHex == "" {
		return s, nil
	}

	var keyBytes []byte
	var err error
	keyBytes, err = hex.DecodeString(strip0x(privateKeyHex))
	if err != nil {
		return nil, errors.New("auth: private key is not valid hex")
	}
	if len(keyBytes) != 32 {
		return nil, errors.New("auth: private key must be 32 bytes")
	}
	s.privKey = secp256k1.PrivKeyFromBytes(keyBytes)

	var derived string = checksumAddress(addressFromPubKey(s.privKey.PubKey()))
	if signerAddr != "" && !equalAddress(signerAddr, derived) {
		return nil, errors.New("auth: signer address does not match the private key (derived " + derived + ")")
	}
	s.signer = derived
	s.enabled = true
	return s, nil
}

// Enabled returns true if the signer is ready to sign requests.
func (s *Signer) Enabled() bool { return s != nil && s.enabled }

// User returns the master account wallet address (may be empty).
func (s *Signer) User() string {
	if s == nil {
		return ""
	}
	return s.user
}

// SignerAddress returns the API wallet address in EIP-55 form.
func (s *Signer) SignerAddress() string {
	if s == nil {
		return ""
	}
	return s.signer
}

// NextNonce returns the next strictly increasing microsecond nonce.
func (s *Signer) NextNonce() int64 { return s.nonce.Next() }

/*
SignParams signs a request parameter set.

The params map is MUTATED: `nonce` and `signer` are set before encoding.
Returns the final wire payload:

	"<sorted urlencoded params>&signature=0x<130 hex chars>"

The same string must be sent verbatim — as the query string for GET/DELETE or
as the x-www-form-urlencoded body for POST/PUT — because the signature covers
exactly these bytes.
*/
func (s *Signer) SignParams(params url.Values) (string, error) {
	if !s.Enabled() {
		return "", ErrSignerDisabled
	}
	params.Set("nonce", strconv.FormatInt(s.nonce.Next(), 10))
	params.Set("signer", s.signer)

	var payload string = params.Encode() // sorted by key per url.Values contract
	var signature string
	var err error
	signature, err = s.SignMessage(payload)
	if err != nil {
		return "", err
	}
	return payload + "&signature=" + signature, nil
}

/*
SignMessage computes the Aster EIP-712 signature over an arbitrary message
string and returns it as 0x-prefixed hex of r||s||v (65 bytes, v = 27+recid),
matching the reference eth_account output format.
*/
func (s *Signer) SignMessage(msg string) (string, error) {
	if !s.Enabled() {
		return "", ErrSignerDisabled
	}
	var digest [32]byte = s.domain.digest(msg)

	// SignCompact returns [v, r(32), s(32)] with v = 27 + recovery id for an
	// uncompressed key; Ethereum expects r||s||v.
	var compact []byte = secpecdsa.SignCompact(s.privKey, digest[:], false)
	var sig [65]byte
	copy(sig[:64], compact[1:])
	sig[64] = compact[0]

	var out []byte = make([]byte, 2+130)
	out[0] = '0'
	out[1] = 'x'
	hex.Encode(out[2:], sig[:])
	return string(out), nil
}

// String returns a log-safe representation of the Signer — without secrets.
func (s *Signer) String() string {
	if s == nil || !s.enabled {
		return "auth.Signer{disabled}"
	}
	return "auth.Signer{enabled, signer=" + s.signer + "}"
}

// strip0x removes an optional 0x/0X prefix from a hex string.
func strip0x(s string) string {
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		return s[2:]
	}
	return s
}
