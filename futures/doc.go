/*
FILE: futures/doc.go

DESCRIPTION:
Package futures implements the Futures profile of the go-aster SDK: Aster
USD-M perpetual futures (REST /fapi/v3 + WS fstream). The package name follows
the exchange's own section naming ("Futures" in the Aster documentation).

ARCHITECTURE:
Two-layer design shared by the sibling SDKs (go-okx, go-bybit):
  - layer 1 (root aster package + internal/*): unified transport — REST client
    with EIP-712 signing, WS supervisor, codec, errors, logging, metrics;
  - layer 2 (this package): futures-specific domain clients that pin the
    /fapi/v3 endpoints and futures payload shapes onto the unified transport.

Domain sub-clients:
  - TradingClient    — orders: create/modify/cancel, batch, cancel-all,
                       cancel-forgotten, open orders, id mapping.
  - AccountClient    — balances, positions, leverage, margin, position mode.
  - MarketDataClient — exchange info, order book snapshot, klines, mark price.
  - StreamClient     — WS subscriptions: orderbook, spread, prices, trades,
                       user data (orders/positions/balances).

USAGE:

	import (
		aster "github.com/tonymontanov/go-aster"
		"github.com/tonymontanov/go-aster/futures"
	)

	var client *aster.Client
	client, _ = aster.NewClient(aster.Config{
		User:       "0x...", // master wallet
		PrivateKey: "0x...", // API wallet private key
	})
	var f *futures.Client = client.Futures().(*futures.Client)
	var info futurestypes.OrderInfo
	info, _ = f.Trading().CreateOrder(ctx, req)

The import of this package registers the futures factory in the root client
(init), so `import _ ".../futures"` is enough to enable client.Futures().
*/

package futures
