# handoff.md — go-aster SDK

Context document for continuing work across sessions. Update after every
significant task (architecture change, new module, refactoring).

## Role & stack

Go SDK for Aster DEX V3 API (Binance-style perp DEX), consumed by the
sleipnir-trading-core HFT desk. Go 1.24. Dependencies (deliberately minimal):
`gorilla/websocket`, `json-iterator/go`, `shopspring/decimal`,
`golang.org/x/crypto` (Keccak-256), `decred/dcrd/dcrec/secp256k1/v4` (ECDSA).
Module: `github.com/tonymontanov/go-aster` (repo renamed from `aster-go`).

## Architecture

Two-layer design copied from sibling SDKs go-okx / go-bybit (same file
headers, explicit `var` declarations, domain methods instead of chain-style):

```
aster (root)        client.go (init-factory for sections), config.go,
                    errors.go/logger.go/metrics.go (aliases), rate-limit-event.go
internal/auth       EIP-712 signer (eip712.go, sign.go, address.go, nonce.go)
internal/rest       unified Do(ctx, Options) — form-encoded params, signature
                    appended LAST, X-MBX-* header collection, error mapping
internal/ws         supervised Conn: combined mode (/stream + SUBSCRIBE ops)
                    and raw mode (/ws/<listenKey>); reconnect+resubscribe+Reset
internal/codec      jsoniter + ParseDecimal/Int64
internal/asterr     Error{Kind, HTTPStatus, AsterCode, Message, Cause}
types/              neutral enums + market structs (BUY/SELL, GTC/IOC/FOK/GTX/HIDDEN...)
orderbook/          engine: U/u/pu sequencing, pre-snapshot buffering, gap→resync
futures/            client.go + trading.go + account.go + market.go +
                    stream.go + stream-user.go + mapping.go + types/
spot/               NOT YET (v2) — RegisterSpotFactory is already wired in root
```

Key protocol facts (versus Binance Futures, which Aster V3 copies):

- Base URLs: REST `https://fapi.asterdex.com` (`/fapi/v3/*`), WS
  `wss://fstream.asterdex.com`; testnet `*.asterdex-testnet.com`. The
  `fapi3.asterdex.com` host from the docs' Python sample answers 403 (WAF)
  — verified 2026-09-02, `fapi` is the right one. Testnet docs name BOTH
  `fstream.asterdex-testnet.com` and `fstream5.asterdex-testnet.com` for
  market streams — unverified (no testnet wallet yet).
- Auth: NO HMAC. EIP-712 typed data (domain `AsterSignTransaction`, version
  "1", chainId 1666 on MAINNET / 714 on TESTNET, zero verifyingContract),
  message = the sorted urlencoded param string. Params added per signed
  request: `nonce` (microseconds, strictly increasing, ±60s window),
  `signer` (API wallet address), `signature` (0x + 130 hex, r||s||v — the
  0x prefix is accepted by the backend, verified live). `user` is NOT sent
  on regular endpoints — only sub-account endpoints need it (v2.5).
- The signed string must be sent VERBATIM (query for GET, form body for
  POST/PUT/DELETE) with `&signature=` appended last — never re-sorted.
  Verified live on mainnet 2026-09-02 (leverage, listenKey, order
  place/modify/cancel, positionRisk, openOrders, allOpenOrders).
- Since 2026-09-01 every TRADE/USER_DATA endpoint requires the master
  wallet to have completed a deposit, otherwise `-5050 DEPOSIT_REQUIRED`
  (mapped to ErrorKindAuth, non-retryable).
- JSON wire format is CASE-SENSITIVE: `e`/`E`, `U`/`u`, `b`/`B`, `s`/`S`,
  `x`/`X`, `l`/`L`, `n`/`N`, `t`/`T` are different fields. internal/codec
  therefore uses a jsoniter Config with `CaseSensitive: true` — the default
  ConfigCompatibleWithStandardLibrary collapses each pair onto one struct
  field and every WS stream dies with "readUint64: unexpected character"
  (this is what broke the desk's first live run). Pinned by
  internal/codec/json_test.go and futures/stream_parse_test.go.
- clientOrderId rule `^[.A-Z:/a-z0-9_-]{1,36}$` (`-4015` otherwise); a
  bare UUID (36 chars) fits, a prefixed one does not.
- exchangeInfo `pricePrecision`/`quantityPrecision` are DISPLAY precisions
  and disagree with the filters for 526 of 573 symbols (BSBUSDT:
  pricePrecision=7, tickSize=0.00001). A price rounded to pricePrecision
  is rejected with `-4014 Price not increased by tick size`. Use
  `SymbolInfo.PriceDecimals()` / `QuantityDecimals()` (derived from
  TickSize/StepSize, futures/types/symbol-info.go) for rounding. All
  tick/step values on the venue are powers of ten.
- Signer verified byte-for-byte against eth_account 0.13.7 golden vectors
  (internal/auth/sign_test.go; generator: scratchpad genvectors.py, demo
  credentials from the official docs).
- Batch limits: create/modify 5 per request, cancel 10 ids per request.
- Account info endpoint is `GET /fapi/v3/accountWithJoinMargin` (NOT
  /account); balances `GET /fapi/v3/balance`; positions `/fapi/v3/positionRisk`.
- Diff-depth: `U`/`u`/`pu` algorithm identical to Binance USD-M; snapshot
  limits 5/10/20/50/100/500/1000; streams @depth@100ms|250(default)|500ms.
- WS: server pings every 5 min (answer with pong; ReadTimeout default 6m);
  ≤10 client msgs/sec (SubscribeInterval paces control frames); ≤200
  streams/conn; 24h connection lifetime (supervisor reconnects).
- listenKey: POST/PUT/DELETE /fapi/v3/listenKey, 60m validity, SDK keeps it
  alive every 30m and rotates on listenKeyExpired (Conn.Kick → redial with
  fresh key from urlFn).
- TIF includes Aster-specific `HIDDEN`; stpMode EXPIRE_TAKER/MAKER/BOTH.

## Roadmap

Done ✅
- Repo renamed aster-go → go-aster (GitHub + local symlink `aster-go`).
- Root package (Client/Config/aliases/RateLimitEvent).
- internal/auth (EIP-712 + nonce + address derivation, golden-vector tests).
- internal/rest (+ tests: wire format, error mapping incl. 2xx `{"code":-`
  safety net, X-MBX header collection, observer).
- internal/ws (+ tests: subscribe frames, dispatch, reconnect+Reset, raw
  mode, Kick, ping→pong).
- orderbook engine (+ tests: buffering, straddle, gap, stale, trim, reset).
- futures: Trading/Account/MarketData/Stream + user data stream + contract
  tests on doc fixtures. `go test ./... -race` clean.
- doc.go, README.md, examples/quickstart.
- v1.0.0 (2026-09-02, PR #1) — post-live fixes on top of v0.1.0:
  internal/codec CaseSensitive (root cause of all WS parse failures),
  Config.ChainID defaults 1666/714 by Testnet (+ exported DefaultChainID /
  TestnetChainID), `-5050` → ErrorKindAuth, IsUnknownOrder predicate
  (-2011/-2013, idempotent cancels on the desk), tests: config_test.go,
  internal/codec/json_test.go, internal/asterr/errors_test.go,
  futures/stream_parse_test.go (official WS fixtures for all 8 event types),
  SymbolInfo.PriceDecimals()/QuantityDecimals() + symbol-info_test.go
  (second live bug: desk rounded prices to display precision → -4014 on
  every BSBUSDT order).
- LIVE VALIDATION ON MAINNET 2026-09-02 via sleipnir `cmd/aster-live
  -private -trade -ws`: 26/26 checks — public REST, market WS (orderbook
  U/u/pu consistent, mark/index/spread/aggTrade), private REST, SetLeverage,
  CreateOrder (GTX far from market) → GetOpenOrders → ModifyOrder (PUT) →
  CancelOrder → 0 open, user-data WS delivered 3 ORDER_TRADE_UPDATE pushes.

Done (desk side) ✅
- sleipnir-trading-core connector shipped on branch `aster-connector`
  (from `qa`): section ids `aster_futures` / `aster_futures_testnet`,
  packages `internal/connectors/aster/{common,futures}`, rate-limiter
  strategy, wiring, `cmd/aster-live` harness. Uses this SDK at v1.0.0.
- Desk credentials convention CHANGED 2026-09-02 (branch aster-connector,
  merged to qa): `api_key` = API wallet address (→ Config.Signer,
  validated against the key), `secret_key` = API wallet private key,
  `passphrase` = master wallet address (→ Config.User, optional). The
  previous mapping (api_key = master wallet) was the root cause of the
  "signer address does not match the private key" startup loop.
- SDK v1.1 candidate: expose a user-data reconnect hook so the desk
  connector can re-seed WatchOpenOrders state after SDK-internal
  reconnects (currently covered by OBM periodic REST sync).

Planned 📋
- (done 2026-09-02) v1.0.0 tagged after the desk's full strategy run;
  sleipnir go.mod depends on the tag, no local replace.
- Testnet run (chainId 714, fstream vs fstream5 host) — needs a dedicated
  testnet API wallet (www.asterdex-testnet.com → Pro API); the current
  ASTER_FUTURES_TESTNET_* env values are mainnet placeholders.
- v2.0: spot/ section (sapi.asterdex.com, /api/v3, same signer).
- v2.5: chase, strategy orders (OTO/OCO/OTOCO), guarded cancel, Noop,
  sub-accounts (needs `user` param + master-key signing), prediction API.

## Rules & code style

- English comments/docs everywhere (public repo). File header blocks
  (FILE/DESCRIPTION/...), `// Name — description.` doc comments.
- Explicit variable declarations (`var x T = ...`), including loop counters;
  `:=` only in `if err := ...` guards and tests.
- Domain methods `(ctx, req) (res, error)`; validation BEFORE network with
  ErrorKindInvalidRequest; messages namespaced `"trading.CreateOrder: ..."`.
- One error type `*aster.Error`; predicates aster.IsRateLimit/IsAuth/...
- decimal end-to-end; wire numerics decoded from strings via codec.ParseDecimal
  (errors ignored for optional fields only).
- No panics in library code. Hot paths: no per-message allocations beyond
  parsing; jsoniter behind internal/codec chokepoint.
- Sections never import each other; shared code moves DOWN (types/, internal/).
- Tests: httptest contract fixtures from official docs; mock WS server;
  golden vectors for crypto. No network in tests.
- internal/codec MUST stay case-sensitive; every new wire struct with
  single-letter keys gets a fixture test in futures/stream_parse_test.go.

## Integration secrets

- No secrets in the repo. Examples read env: `ASTER_USER`,
  `ASTER_PRIVATE_KEY`, `ASTER_TESTNET=1`.
- The private key in tests is the PUBLIC demo key from the official Aster
  docs — safe by construction.
- sleipnir credential mapping (planned, connector side): `api_key` → user
  address, `passphrase` → signer address, `secret_key` → private key; env
  fallbacks `ASTER_FUTURES_API_KEY` / `_PASSPHRASE` / `_SECRET_KEY`.
