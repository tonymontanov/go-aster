/*
FILE: config.go

DESCRIPTION:
config.go defines the SDK configuration structs (see spec §5.5).
Also contains the production / testnet Aster endpoints and default values.

MAIN FUNCTIONS:
  - DefaultConfig(): returns Config with production endpoints and default values
    for timeouts/reconnect/orderbook.
  - (Config).withDefaults(): fills empty Config fields with defaults. Inside
    the SDK the config is ALWAYS passed through withDefaults() first.

MAIN TYPES:
  - Config:                    public Client configuration.
  - RestConfig / WsConfig:     transport parameters (timeouts, reconnect).
  - OrderbookConfig:           orderbook engine parameters (depth, snapshot).

ENDPOINTS:
Production Aster hosts are used by default:
  - REST futures: https://fapi.asterdex.com
  - WS futures:   wss://fstream.asterdex.com
Testnet (Config.Testnet = true):
  - REST futures: https://fapi.asterdex-testnet.com
  - WS futures:   wss://fstream.asterdex-testnet.com
  - EIP-712 chainId 714 instead of the production 1666 (different signing
    domains per the official docs; an explicit Config.ChainID always wins).

AUTHENTICATION (V3 API Wallet / Agent model):
Aster V3 does not use API key + HMAC. Credentials are:
  - User:       master account wallet address (0x...);
  - Signer:     API wallet (agent) address — optional, derived from the key;
  - PrivateKey: API wallet private key (hex) used for EIP-712 signing.
Create an API wallet at https://www.asterdex.com/en/api-wallet (Pro API).

DEPENDENCIES:
Standard library:
  - time: timeouts and reconnect/keepalive intervals.
*/

package aster

import (
	"time"

	"github.com/tonymontanov/go-aster/internal/auth"
)

// EIP-712 signing-domain chainIds (single source of truth: internal/auth).
// Re-exported so applications can pin them explicitly in Config.ChainID.
const (
	// DefaultChainID — chainId of the production signing domain.
	DefaultChainID int64 = auth.DefaultChainID
	// TestnetChainID — chainId of the testnet signing domain.
	TestnetChainID int64 = auth.TestnetChainID
)

// Aster transport URLs. Declared as vars rather than const so tests can
// override them (e.g. to point at a mock server).
var (
	// DefaultFuturesRestURL — production REST endpoint for USD-M perpetual
	// futures (/fapi/v3/*).
	DefaultFuturesRestURL string = "https://fapi.asterdex.com"
	// DefaultFuturesWsURL — production WS endpoint for futures market and
	// user-data streams.
	DefaultFuturesWsURL string = "wss://fstream.asterdex.com"

	// TestnetFuturesRestURL — testnet REST endpoint for futures.
	TestnetFuturesRestURL string = "https://fapi.asterdex-testnet.com"
	// TestnetFuturesWsURL — testnet WS endpoint for futures.
	TestnetFuturesWsURL string = "wss://fstream.asterdex-testnet.com"
)

// Config — public SDK configuration. Passed to NewClient.
type Config struct {
	// User — master account wallet address ("0x..."). Not sent with regular
	// trading requests (the signer identifies the account), but required by
	// sub-account management endpoints; store it from the start.
	User string
	// Signer — API wallet (agent) address. Optional: when empty it is derived
	// from PrivateKey. When set it is validated against the derived address at
	// NewClient time — a mismatch fails fast instead of producing opaque
	// -1022 INVALID_SIGNATURE responses.
	Signer string
	// PrivateKey — API wallet private key, hex with optional 0x prefix.
	// Empty → the client can only access public endpoints.
	PrivateKey string
	// ChainID — EIP-712 signing domain chainId. Default: DefaultChainID
	// (1666, production) or TestnetChainID (714) when Testnet is set and
	// ChainID is left 0. The two environments use DIFFERENT signing domains
	// (official V3 docs): a 1666 signature is rejected by the testnet with
	// -1022 INVALID_SIGNATURE.
	ChainID int64

	// REST — REST transport settings. If empty, DefaultConfig().REST is used.
	REST RestConfig
	// WS — WebSocket transport settings. If empty, DefaultConfig().WS is used.
	WS WsConfig
	// Orderbook — orderbook engine settings. If empty, DefaultConfig().Orderbook is used.
	Orderbook OrderbookConfig

	// Logger — optional logger. If nil, NoopLogger() is used.
	Logger Logger

	// Metrics — optional counter factory. If nil, NoopMetrics() is used.
	// The SDK creates the following counters through it (see docs):
	//   aster_ws_messages_received_total
	//   aster_ws_messages_dropped_total
	//   aster_ws_reconnects_total
	//   aster_ws_subscriptions_total
	//   aster_ws_listen_key_renewals_total
	Metrics CounterFactory

	// UserAgent — User-Agent value for REST requests. Default: "go-aster/v1".
	UserAgent string

	// RateLimitEventObserver — optional hook called SYNCHRONOUSLY by the SDK
	// after every REST response (successful or exchange-level error) with a
	// structured RateLimitEvent: the X-MBX-USED-WEIGHT-* / X-MBX-ORDER-COUNT-*
	// headers plus request metadata (OrderCount / Symbols / Category).
	//
	// The observer is NOT called on transport errors (timeout / network reset,
	// before an HTTP response arrives) because there is no new rate-limit
	// information in those cases. It is called on any received response,
	// including 4xx/5xx.
	//
	// Speed contract: the observer is called in the goroutine that executed
	// the REST call and blocks the return from the call until it completes.
	// Implementations must be O(1) — typically a non-blocking send to a
	// buffered channel. Any blocking or panic stalls the caller's REST pipeline.
	//
	// If nil — no-op, zero overhead.
	RateLimitEventObserver func(RateLimitEvent)

	// Testnet — switches default endpoints to the Aster testnet
	// (fapi.asterdex-testnet.com / fstream.asterdex-testnet.com). Explicitly
	// set URLs are never overridden. Testnet requires its own API wallet
	// created at https://www.asterdex-testnet.com/en/api-wallet.
	Testnet bool
}

// RestConfig — HTTP transport settings.
type RestConfig struct {
	// FuturesBaseURL — base URL for the futures REST API (/fapi/v3/*).
	// Default: DefaultFuturesRestURL (or testnet when Config.Testnet).
	FuturesBaseURL string
	// RequestTimeout — timeout for a single REST request. Default: 10s.
	// For latency-critical calls (place/cancel) pass a ctx with its own
	// deadline — it overrides RequestTimeout.
	RequestTimeout time.Duration
	// MaxIdleConns — idle connection pool size for http.Transport. Default: 100.
	MaxIdleConns int
	// MaxIdleConnsPerHost — pool size per host. Default: 100.
	MaxIdleConnsPerHost int
	// IdleConnTimeout — keep-alive idle timeout. Default: 90s.
	IdleConnTimeout time.Duration
}

// WsConfig — WebSocket transport settings.
type WsConfig struct {
	// FuturesURL — futures WS base URL (market streams via /stream, user data
	// via /ws/<listenKey>). Default: DefaultFuturesWsURL (or testnet).
	FuturesURL string
	// HandshakeTimeout — connection handshake timeout. Default: 10s.
	HandshakeTimeout time.Duration
	// ReadTimeout — read deadline for a single frame. The Aster server sends a
	// ping frame every 5 minutes, so on a quiet stream (user data) the deadline
	// is refreshed at least that often. Default: 6m.
	ReadTimeout time.Duration
	// WriteTimeout — write timeout for a single frame. Default: 5s.
	WriteTimeout time.Duration
	// ReconnectInitialBackoff — initial delay between reconnect attempts. Default: 200ms.
	ReconnectInitialBackoff time.Duration
	// ReconnectMaxBackoff — upper bound of backoff. Default: 10s.
	ReconnectMaxBackoff time.Duration
	// ReconnectJitter — relative jitter [0..1] added to backoff. Default: 0.2.
	ReconnectJitter float64
	// ReadBufferSize — gorilla/websocket read buffer size. Default: 64KB.
	ReadBufferSize int
	// WriteBufferSize — gorilla/websocket write buffer size. Default: 16KB.
	WriteBufferSize int
	// SubscribeBatchSize — max stream names per one SUBSCRIBE frame when
	// (re)subscribing. Default: 50 (a connection may carry up to 200 streams).
	SubscribeBatchSize int
	// SubscribeInterval — minimal gap between outgoing control frames. Aster
	// disconnects clients sending more than 10 messages/second. Default: 150ms.
	SubscribeInterval time.Duration
	// ListenKeyKeepaliveInterval — how often the SDK extends the user-data
	// stream listenKey (validity 60m, PUT extends it). Default: 30m.
	ListenKeyKeepaliveInterval time.Duration
}

// OrderbookConfig — orderbook engine settings.
type OrderbookConfig struct {
	// MaxDepth — depth of the local order book (number of levels per side).
	// Default: 1000 (matches the maximum REST snapshot limit).
	MaxDepth int
	// SnapshotLimit — REST depth limit used for the initial snapshot and for
	// resync after a sequence gap. Allowed by the exchange: 5, 10, 20, 50,
	// 100, 500, 1000. Default: 1000.
	SnapshotLimit int
}

// DefaultConfig returns a Config with all sensible defaults
// (production endpoints + production timeouts).
func DefaultConfig() Config {
	return Config{
		ChainID: DefaultChainID,
		REST: RestConfig{
			FuturesBaseURL:      DefaultFuturesRestURL,
			RequestTimeout:      10 * time.Second,
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
		},
		WS: WsConfig{
			FuturesURL:                 DefaultFuturesWsURL,
			HandshakeTimeout:           10 * time.Second,
			ReadTimeout:                6 * time.Minute,
			WriteTimeout:               5 * time.Second,
			ReconnectInitialBackoff:    200 * time.Millisecond,
			ReconnectMaxBackoff:        10 * time.Second,
			ReconnectJitter:            0.2,
			ReadBufferSize:             64 * 1024,
			WriteBufferSize:            16 * 1024,
			SubscribeBatchSize:         50,
			SubscribeInterval:          150 * time.Millisecond,
			ListenKeyKeepaliveInterval: 30 * time.Minute,
		},
		Orderbook: OrderbookConfig{
			MaxDepth:      1000,
			SnapshotLimit: 1000,
		},
		Logger:    NoopLogger(),
		Metrics:   NoopMetrics(),
		UserAgent: "go-aster/v1",
	}
}

// withDefaults returns a Config where all empty fields are filled with values
// from DefaultConfig(). Used inside NewClient — the user-supplied Config is
// never mutated.
func (c Config) withDefaults() Config {
	var def Config = DefaultConfig()

	// ChainID: the testnet trading API signs with a different EIP-712 domain
	// (714) than production (1666). An explicit ChainID always wins.
	if c.ChainID == 0 {
		c.ChainID = def.ChainID
		if c.Testnet {
			c.ChainID = TestnetChainID
		}
	}

	// Endpoints: for Testnet use *-testnet hosts by default. If the user
	// explicitly set a URL — do NOT override it.
	var defRest string = def.REST.FuturesBaseURL
	var defWs string = def.WS.FuturesURL
	if c.Testnet {
		defRest = TestnetFuturesRestURL
		defWs = TestnetFuturesWsURL
	}
	if c.REST.FuturesBaseURL == "" {
		c.REST.FuturesBaseURL = defRest
	}
	if c.REST.RequestTimeout == 0 {
		c.REST.RequestTimeout = def.REST.RequestTimeout
	}
	if c.REST.MaxIdleConns == 0 {
		c.REST.MaxIdleConns = def.REST.MaxIdleConns
	}
	if c.REST.MaxIdleConnsPerHost == 0 {
		c.REST.MaxIdleConnsPerHost = def.REST.MaxIdleConnsPerHost
	}
	if c.REST.IdleConnTimeout == 0 {
		c.REST.IdleConnTimeout = def.REST.IdleConnTimeout
	}

	if c.WS.FuturesURL == "" {
		c.WS.FuturesURL = defWs
	}
	if c.WS.HandshakeTimeout == 0 {
		c.WS.HandshakeTimeout = def.WS.HandshakeTimeout
	}
	if c.WS.ReadTimeout == 0 {
		c.WS.ReadTimeout = def.WS.ReadTimeout
	}
	if c.WS.WriteTimeout == 0 {
		c.WS.WriteTimeout = def.WS.WriteTimeout
	}
	if c.WS.ReconnectInitialBackoff == 0 {
		c.WS.ReconnectInitialBackoff = def.WS.ReconnectInitialBackoff
	}
	if c.WS.ReconnectMaxBackoff == 0 {
		c.WS.ReconnectMaxBackoff = def.WS.ReconnectMaxBackoff
	}
	if c.WS.ReconnectJitter == 0 {
		c.WS.ReconnectJitter = def.WS.ReconnectJitter
	}
	if c.WS.ReadBufferSize == 0 {
		c.WS.ReadBufferSize = def.WS.ReadBufferSize
	}
	if c.WS.WriteBufferSize == 0 {
		c.WS.WriteBufferSize = def.WS.WriteBufferSize
	}
	if c.WS.SubscribeBatchSize == 0 {
		c.WS.SubscribeBatchSize = def.WS.SubscribeBatchSize
	}
	if c.WS.SubscribeInterval == 0 {
		c.WS.SubscribeInterval = def.WS.SubscribeInterval
	}
	if c.WS.ListenKeyKeepaliveInterval == 0 {
		c.WS.ListenKeyKeepaliveInterval = def.WS.ListenKeyKeepaliveInterval
	}

	if c.Orderbook.MaxDepth == 0 {
		c.Orderbook.MaxDepth = def.Orderbook.MaxDepth
	}
	if c.Orderbook.SnapshotLimit == 0 {
		c.Orderbook.SnapshotLimit = def.Orderbook.SnapshotLimit
	}

	if c.Logger == nil {
		c.Logger = NoopLogger()
	}
	if c.Metrics == nil {
		c.Metrics = NoopMetrics()
	}
	if c.UserAgent == "" {
		c.UserAgent = def.UserAgent
	}

	return c
}

// validate checks that the required URL fields are set. Credentials are not
// enforced here because public REST/WS work without keys — the auth.Signer
// tracks an `enabled` flag and enforces credentials at call time.
func (c Config) validate() error {
	if c.REST.FuturesBaseURL == "" {
		return NewError(ErrorKindInvalidRequest, 0, "config: REST.FuturesBaseURL is empty", nil)
	}
	if c.WS.FuturesURL == "" {
		return NewError(ErrorKindInvalidRequest, 0, "config: WS.FuturesURL is empty", nil)
	}
	return nil
}
