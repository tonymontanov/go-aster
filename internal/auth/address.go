/*
FILE: internal/auth/address.go

DESCRIPTION:
Ethereum address helpers: derivation from a secp256k1 public key and EIP-55
checksum formatting. Used by the Signer to derive/validate the API wallet
(signer) address from the private key, so that a mistyped address fails fast
at client construction instead of producing opaque signature errors.

MAIN FUNCTIONS:
  - addressFromPubKey: keccak256(uncompressed pubkey)[12:] → 20-byte address.
  - checksumAddress:   EIP-55 mixed-case formatting ("0xAbC...").
  - equalAddress:      case-insensitive address comparison.

DEPENDENCIES:
- github.com/decred/dcrd/dcrec/secp256k1/v4: public key serialization.
*/

package auth

import (
	"encoding/hex"
	"strings"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// addressFromPubKey derives the Ethereum address from a secp256k1 public key:
// the last 20 bytes of keccak256 of the uncompressed public key without the
// 0x04 prefix byte.
func addressFromPubKey(pub *secp256k1.PublicKey) [20]byte {
	var raw []byte = pub.SerializeUncompressed() // 65 bytes, leading 0x04
	var digest [32]byte = keccak256(raw[1:])
	var addr [20]byte
	copy(addr[:], digest[12:])
	return addr
}

// checksumAddress formats a 20-byte address as an EIP-55 mixed-case hex string
// with the 0x prefix. Wallets and the Aster UI display addresses in this form.
func checksumAddress(addr [20]byte) string {
	var lower string = hex.EncodeToString(addr[:])
	var digest [32]byte = keccak256([]byte(lower))

	var out []byte = make([]byte, 2+40)
	out[0] = '0'
	out[1] = 'x'
	var i int
	for i = 0; i < 40; i++ {
		var c byte = lower[i]
		// Uppercase a letter when the corresponding keccak nibble is >= 8.
		if c >= 'a' && c <= 'f' {
			var nibble byte
			if i%2 == 0 {
				nibble = digest[i/2] >> 4
			} else {
				nibble = digest[i/2] & 0x0f
			}
			if nibble >= 8 {
				c = c - 'a' + 'A'
			}
		}
		out[2+i] = c
	}
	return string(out)
}

// equalAddress compares two hex addresses case-insensitively, tolerating a
// missing 0x prefix on either side.
func equalAddress(a, b string) bool {
	return strings.EqualFold(strings.TrimPrefix(a, "0x"), strings.TrimPrefix(b, "0x"))
}
