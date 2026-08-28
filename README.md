# go-aster

High-performance Go SDK for the [Aster DEX](https://www.asterdex.com) V3 API, built for HFT/algorithmic trading. Sibling of [go-okx](https://github.com/tonymontanov/go-okx) and [go-bybit](https://github.com/tonymontanov/go-bybit) — same architecture, same conventions.

## Status

| Version | Scope | State |
| ------- | ----- | ----- |
| v1.x | Futures section (USD-M perpetuals, `/fapi/v3`): REST + WS + orderbook engine | in development |
| v2.x | Spot section (`/api/v3`) | planned |
| v2.5 | Advanced endpoints (chase, strategy orders, guarded cancel, Noop, sub-accounts) | planned |

## Installation

```bash
go get github.com/tonymontanov/go-aster
```

Requires Go 1.24+.

## Authentication (V3 API Wallet / Agent model)

Aster V3 does not use API key + HMAC. Every private request is signed with an
**EIP-712** typed-data signature (domain `AsterSignTransaction`, chainId 1666)
produced by an **API wallet** (agent) private key:

- `User` — master account wallet address;
- `Signer` — API wallet address (optional in config: derived from the key);
- `PrivateKey` — API wallet private key (hex).

Create an API wallet at <https://www.asterdex.com/en/api-wallet> (switch to
*Pro API*). Testnet wallets: <https://www.asterdex-testnet.com/en/api-wallet>.

The signing implementation is pure Go (`x/crypto` Keccak-256 +
`dcrd/secp256k1` deterministic ECDSA) and is verified byte-for-byte against
the reference `eth_account` implementation by golden-vector tests.

## Quick start

```go
import (
    aster "github.com/tonymontanov/go-aster"
    "github.com/tonymontanov/go-aster/futures"
    futurestypes "github.com/tonymontanov/go-aster/futures/types"
)

client, err := aster.NewClient(aster.Config{
    User:       os.Getenv("ASTER_USER"),
    PrivateKey: os.Getenv("ASTER_PRIVATE_KEY"),
})
if err != nil { panic(err) }
f := client.Futures().(*futures.Client)

// REST: place a limit order
info, err := f.Trading().CreateOrder(ctx, futurestypes.CreateOrderRequest{
    Symbol:      "BTCUSDT",
    Side:        futurestypes.SideTypeBuy,
    Type:        futurestypes.OrderTypeLimit,
    TimeInForce: futurestypes.TimeInForceTypeGTC,
    Quantity:    decimal.RequireFromString("0.001"),
    Price:       decimal.RequireFromString("50000"),
})

// WS: consistent local order book (snapshot + diff + auto-resync)
err = f.Stream().WatchOrderbook(ctx, "BTCUSDT", 20,
    func(book futurestypes.OrderBookSnapshot) { /* ... */ },
    func(err error) { /* ... */ },
)
```

See [examples/quickstart](examples/quickstart/main.go) for a runnable demo.

## Architecture

Two layers (see `doc.go` for details):

```
aster (root)          Client, Config, Error, Logger, Metrics
├── internal/auth     EIP-712 signing, monotonic microsecond nonces
├── internal/rest     unified REST transport, error mapping, X-MBX-* headers
├── internal/ws       supervised WS (reconnect + backoff + resubscribe)
├── internal/codec    json-iterator + decimal helpers
├── types             neutral protocol types
├── orderbook         L2 book engine (U/u/pu sequencing, gap detection)
├── futures           Futures section: Trading / Account / MarketData / Stream
└── spot              reserved (v2)
```

Domain methods are plain calls — `(ctx, request) (response, error)` — no
chain-style builders. All prices/quantities are `shopspring/decimal`.

### Futures surface (v1)

- **Trading**: `CreateOrder`, `ModifyOrder`, `CancelOrder`, `CreateBatchOrders`,
  `ModifyBatchOrders`, `CancelBatchOrders`, `CancelAllOrders`,
  `AutoCancelAllOpenOrders` (dead-man switch), `CancelForgottenOrders` (TTL),
  `GetOpenOrders`, `GetOrder`, `GetAllOrders`, ClientOrderID↔OrderID mapping.
- **Account**: `GetBalances`, `GetPositions`, `ClosePosition`, `SetLeverage`,
  `SetMarginType`, `Set/GetPositionMode`, `Set/GetStpMode`, `GetUserTrades`,
  `GetCommissionRate`.
- **MarketData**: `GetExchangeInfo`, `GetSymbolInfo`, `GetOrderBook`,
  `GetKlines`/`GetHistoricalCandles`, `GetMarkPrice`, `GetFundingRateHistory`,
  `GetBookTicker`, `GetPrice`, `Ping`, `GetServerTime`.
- **Stream**: `WatchOrderbook` (engine-backed, auto-resync), `WatchSpread`,
  `WatchMarkPrice`, `WatchAggTrades`, `WatchMiniTicker`, `WatchKline`,
  `WatchOrderUpdates`, `WatchAccountUpdates` (listenKey lifecycle is fully
  automatic), `UnwatchSymbol`.

## Reliability contract

- WS reconnect with exponential backoff + jitter, automatic resubscribe;
- the orderbook engine buffers deltas, validates `U`/`u`/`pu` sequencing and
  resyncs from REST snapshots on any gap;
- the user-data listenKey is created, kept alive (PUT every 30m) and rotated
  on expiry automatically;
- rate-limit usage (`X-MBX-USED-WEIGHT-*`, `X-MBX-ORDER-COUNT-*`) is exposed
  per call and via `Config.RateLimitEventObserver`;
- errors carry a category (`Network`/`RateLimit`/`Auth`/`InvalidRequest`/
  `Exchange`) plus the raw exchange code for `errors.As` inspection.

## Testing

```bash
go test ./...        # unit + contract tests (no network)
go test ./... -race
```

Contract tests run against fixtures from the official API documentation;
the EIP-712 signer is pinned by golden vectors generated with `eth_account`.

## License

Apache 2.0 — see [LICENSE](LICENSE).
