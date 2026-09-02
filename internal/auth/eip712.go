/*
FILE: internal/auth/eip712.go

DESCRIPTION:
Minimal EIP-712 typed-data hashing specialized for the fixed Aster signing
schema. Aster V3 signs every private REST request with:

	typed data = {
	  domain:      { name: "AsterSignTransaction", version: "1",
	                 chainId: <cfg, default 1666>, verifyingContract: 0x0 },
	  primaryType: "Message",
	  types:       { Message: [{ name: "msg", type: "string" }] },
	  message:     { msg: "<urlencoded request params>" },
	}

Because the schema never changes at runtime, the domain separator and the
Message type hash are precomputed once in newEIP712Domain; per request only
three Keccak-256 hashes remain:

	structHash = keccak256(msgTypeHash || keccak256(msg))
	digest     = keccak256(0x19 || 0x01 || domainSeparator || structHash)

MAIN FUNCTIONS:
  - newEIP712Domain(chainID): precomputes the domain separator.
  - (eip712Domain).digest(msg): returns the 32-byte digest to sign.
  - keccak256: raw Keccak-256 (NOT the NIST SHA3 variant).

PERFORMANCE:
No allocations beyond the sha3 state objects; hot path is ~3 keccak
permutations per request. Signing itself (ECDSA) dominates the cost.

DEPENDENCIES:
- golang.org/x/crypto/sha3: keccak256 (NewLegacyKeccak256).
*/

package auth

import (
	"encoding/binary"

	"golang.org/x/crypto/sha3"
)

// Aster EIP-712 domain constants. Fixed by the exchange protocol.
const (
	eip712DomainType = "EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"
	eip712DomainName = "AsterSignTransaction"
	eip712DomainVer  = "1"
	eip712MsgType    = "Message(string msg)"

	// DefaultChainID — chainId of the PRODUCTION Aster EIP-712 signing domain
	// (official V3 docs, mainnet reference). NewSigner uses it when chainID
	// is 0.
	DefaultChainID int64 = 1666
	// TestnetChainID — chainId of the TESTNET signing domain (official V3
	// testnet docs and the reference aster-code.py sample). Selected by the
	// root Config when Testnet is set and ChainID is left 0.
	TestnetChainID int64 = 714
)

// eip712Domain holds the precomputed hashes of the fixed Aster signing schema.
type eip712Domain struct {
	separator   [32]byte // keccak256 of the ABI-encoded EIP712Domain struct
	msgTypeHash [32]byte // keccak256("Message(string msg)")
}

// newEIP712Domain precomputes the domain separator for the given chainId.
// verifyingContract is always the zero address per the Aster specification.
func newEIP712Domain(chainID int64) eip712Domain {
	var typeHash [32]byte = keccak256([]byte(eip712DomainType))
	var nameHash [32]byte = keccak256([]byte(eip712DomainName))
	var verHash [32]byte = keccak256([]byte(eip712DomainVer))

	// uint256 chainId — 32-byte big-endian. chainID is small, so the top 24
	// bytes stay zero.
	var chainBytes [32]byte
	binary.BigEndian.PutUint64(chainBytes[24:], uint64(chainID))

	// address verifyingContract — 0x0, ABI-encoded as 32 zero bytes.
	var contractBytes [32]byte

	var buf []byte = make([]byte, 0, 5*32)
	buf = append(buf, typeHash[:]...)
	buf = append(buf, nameHash[:]...)
	buf = append(buf, verHash[:]...)
	buf = append(buf, chainBytes[:]...)
	buf = append(buf, contractBytes[:]...)

	return eip712Domain{
		separator:   keccak256(buf),
		msgTypeHash: keccak256([]byte(eip712MsgType)),
	}
}

// digest returns the EIP-712 digest to sign for the given message string.
func (d eip712Domain) digest(msg string) [32]byte {
	// structHash = keccak256(msgTypeHash || keccak256(bytes(msg)))
	var msgHash [32]byte = keccak256([]byte(msg))
	var structBuf [64]byte
	copy(structBuf[:32], d.msgTypeHash[:])
	copy(structBuf[32:], msgHash[:])
	var structHash [32]byte = keccak256(structBuf[:])

	// digest = keccak256(0x19 || 0x01 || domainSeparator || structHash)
	var digestBuf [66]byte
	digestBuf[0] = 0x19
	digestBuf[1] = 0x01
	copy(digestBuf[2:34], d.separator[:])
	copy(digestBuf[34:66], structHash[:])
	return keccak256(digestBuf[:])
}

// keccak256 computes the legacy Keccak-256 hash (Ethereum variant, pre-NIST
// padding). NOT interchangeable with sha3.Sum256.
func keccak256(data []byte) [32]byte {
	var h = sha3.NewLegacyKeccak256()
	// sha3 state Write never returns an error.
	_, _ = h.Write(data)
	var out [32]byte
	h.Sum(out[:0])
	return out
}
