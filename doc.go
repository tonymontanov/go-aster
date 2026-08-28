/*
FILE: doc.go

DESCRIPTION:
Package aster is the root of the go-aster SDK — a high-performance Go client
for the Aster DEX V3 API (https://github.com/asterdex/api-docs), built for
HFT/algorithmic trading.

ARCHITECTURE (two layers, mirroring the sibling go-okx / go-bybit SDKs):

	aster (root)            Client, Config, Error, Logger, Metrics — shared resources
	├── internal/auth       EIP-712 request signing (API Wallet / Agent model)
	├── internal/rest       unified REST transport (one HTTP pool, error mapping)
	├── internal/ws         supervised WS connection (reconnect, resubscribe)
	├── internal/codec      JSON parsing (json-iterator) + decimal helpers
	├── types               neutral protocol types shared by all sections
	├── orderbook           local L2 book engine (diff-depth synchronization)
	├── futures             Futures section: USD-M perpetuals (/fapi/v3)   [v1]
	└── spot                Spot section (/api/v3)                — reserved [v2]

Section packages follow the exchange's own naming (Aster docs: "Futures",
"Spot"). Each section composes four domain sub-clients over the shared
transport: Trading, Account, MarketData, Stream.

AUTHENTICATION:
Aster V3 uses the API Wallet / Agent model instead of API key + HMAC. Every
private request carries `signer` (API wallet address), `nonce` (microseconds)
and an EIP-712 signature produced by the API wallet private key (domain
"AsterSignTransaction", chainId 1666). Create an API wallet at
https://www.asterdex.com/en/api-wallet (Pro API).

QUICK START:

	import (
		aster "github.com/tonymontanov/go-aster"
		"github.com/tonymontanov/go-aster/futures"
		futurestypes "github.com/tonymontanov/go-aster/futures/types"
	)

	var client *aster.Client
	var err error
	client, err = aster.NewClient(aster.Config{
		User:       os.Getenv("ASTER_USER"),        // master wallet address
		PrivateKey: os.Getenv("ASTER_PRIVATE_KEY"), // API wallet private key
	})
	if err != nil { ... }
	var f *futures.Client = client.Futures().(*futures.Client)

	// REST
	var info futurestypes.OrderInfo
	info, err = f.Trading().CreateOrder(ctx, futurestypes.CreateOrderRequest{...})

	// WS
	err = f.Stream().WatchOrderbook(ctx, "BTCUSDT", 20, onBook, onErr)

PERFORMANCE NOTES:
  - one shared HTTP connection pool, one market WS connection per section;
  - json-iterator on all hot paths, two-stage decoding via RawMessage;
  - prices/quantities are shopspring/decimal end to end;
  - the EIP-712 domain separator is precomputed once; a request signature
    costs three keccak hashes + one deterministic ECDSA sign.
*/

package aster
