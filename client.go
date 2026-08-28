/*
FILE: client.go

DESCRIPTION:
The main public SDK Client. Holds shared resources (REST client, signer,
config, logger) and provides lazy domain sub-clients on demand.
In v1 only the Futures profile (USD-M perpetual futures, /fapi/v3) is
supported; the Spot profile is reserved and implemented in a separate
iteration (v2).

MAIN FUNCTIONS:
  - NewClient(cfg)        : constructor with Config validation and defaults.
  - (Client).Futures()    : returns the Futures-profile sub-client.
                            Created lazily on first access.
  - (Client).Close()      : gracefully shuts down background operations (WS
                            streams, connection pools). Non-blocking.

MAIN ENTITIES:
  - Client                : root SDK object.
  - futuresClientFactory  : internal contract through which the futures package
                            creates its client. This avoids an import cycle
                            between the root (where Client lives) and futures
                            (where futures.Client lives).

DEPENDENCIES:
- internal/auth, internal/rest: signing and REST transport.
- sync: lazy sub-client initialization.
*/

package aster

import (
	"sync"

	"github.com/tonymontanov/go-aster/internal/auth"
	"github.com/tonymontanov/go-aster/internal/rest"
)

// Client — root SDK object.
type Client struct {
	cfg    Config
	signer *auth.Signer
	rest   *rest.Client
	logger Logger

	futuresOnce sync.Once
	futuresVal  any

	spotOnce sync.Once
	spotVal  any
}

// NewClient creates the root SDK client. cfg goes through withDefaults + validate.
// If PrivateKey is set — the Signer will be enabled and sign private calls;
// otherwise the client can only access public endpoints.
func NewClient(cfg Config) (*Client, error) {
	cfg = cfg.withDefaults()
	var err error = cfg.validate()
	if err != nil {
		return nil, err
	}

	var signer *auth.Signer
	signer, err = auth.NewSigner(cfg.User, cfg.Signer, cfg.PrivateKey, cfg.ChainID)
	if err != nil {
		return nil, NewError(ErrorKindAuth, 0, "config: invalid credentials", err)
	}

	var restCfg rest.Config = rest.Config{
		RequestTimeout:      cfg.REST.RequestTimeout,
		MaxIdleConns:        cfg.REST.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.REST.MaxIdleConnsPerHost,
		IdleConnTimeout:     cfg.REST.IdleConnTimeout,
	}
	// Forward the event-observer via a thin adapter. The RateLimitEvent struct
	// lives in the root aster package and CANNOT be passed directly into
	// internal/rest (import cycle). Therefore rest calls the callback with flat
	// arguments (endpoint, method, headers, meta), and here we assemble
	// RateLimitEvent for the final subscriber.
	if cfg.RateLimitEventObserver != nil {
		var userObserver func(RateLimitEvent) = cfg.RateLimitEventObserver
		restCfg.RateLimitEventObserver = func(endpoint, method string, headers map[string]string, meta rest.RequestMeta) {
			userObserver(RateLimitEvent{
				Endpoint:   endpoint,
				Method:     method,
				Headers:    headers,
				OrderCount: meta.OrderCount,
				Symbols:    meta.Symbols,
				Category:   RateLimitCategory(meta.Category),
			})
		}
	}
	var restClient *rest.Client = rest.NewClient(cfg.REST.FuturesBaseURL, signer, restCfg, cfg.UserAgent, cfg.Logger)

	return &Client{
		cfg:    cfg,
		signer: signer,
		rest:   restClient,
		logger: cfg.Logger,
	}, nil
}

// Config returns a copy of the final config (after withDefaults). Useful for
// diagnostics and metrics.
func (c *Client) Config() Config { return c.cfg }

// Logger returns the current logger.
func (c *Client) Logger() Logger { return c.logger }

// Signer returns the internal/auth.Signer (for internal SDK sub-packages).
// Exported for use by futures/spot sub-packages — user code should not access
// the signer directly.
func (c *Client) Signer() *auth.Signer { return c.signer }

// REST returns the internal/rest.Client (for internal SDK sub-packages).
func (c *Client) REST() *rest.Client { return c.rest }

// Close releases resources (idle HTTP connections). Safe to call multiple times.
// WS streams terminate on cancellation of their contexts.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.rest.Close()
	return nil
}

// futuresClientFactory — futures client builder function. Registered by the
// futures package via RegisterFuturesFactory in init(). This avoids the
// import cycle.
var futuresClientFactory func(c *Client) any

// RegisterFuturesFactory registers the futures client factory. Must be called
// from the futures package's init(). Idempotent.
func RegisterFuturesFactory(f func(c *Client) any) {
	if futuresClientFactory == nil {
		futuresClientFactory = f
	}
}

// Futures returns the futures sub-client. The return type is any because the
// root package cannot import futures (which imports the root). The caller
// immediately type-asserts to *futures.Client.
//
// Usage idiom:
//
//	var futuresClient *futures.Client = client.Futures().(*futures.Client)
//
// Lazy: created on first access via the registered factory.
func (c *Client) Futures() any {
	c.futuresOnce.Do(func() {
		if futuresClientFactory == nil {
			c.logger.Warn("aster.Client.Futures: futures factory is not registered; import _ \"github.com/tonymontanov/go-aster/futures\"")
			return
		}
		c.futuresVal = futuresClientFactory(c)
	})
	return c.futuresVal
}

// spotClientFactory — spot client builder function. Registered by the spot
// package in init() exactly as futures (see RegisterFuturesFactory).
var spotClientFactory func(c *Client) any

// RegisterSpotFactory registers the spot client factory. Must be called from
// the spot package's init(). Idempotent.
//
// Spot and Futures are independent domains: enabling one does NOT require the
// other. This lets applications import only the needed profile without pulling
// unused code into the binary:
//
//	import _ "github.com/tonymontanov/go-aster/spot"     // spot only
//	import _ "github.com/tonymontanov/go-aster/futures"  // futures only
//
// The root aster package does NOT import spot or futures — this avoids the
// import cycle (both packages import the root aster for aster.Config /
// aster.NewError etc.).
//
// Reserved for v2: the spot package ships in a later iteration.
func RegisterSpotFactory(f func(c *Client) any) {
	if spotClientFactory == nil {
		spotClientFactory = f
	}
}

// Spot returns the spot sub-client. See Futures() — semantics are identical.
//
// Reserved for v2: returns nil with a warning until the spot package ships
// and is imported.
func (c *Client) Spot() any {
	c.spotOnce.Do(func() {
		if spotClientFactory == nil {
			c.logger.Warn("aster.Client.Spot: spot factory is not registered; import _ \"github.com/tonymontanov/go-aster/spot\"")
			return
		}
		c.spotVal = spotClientFactory(c)
	})
	return c.spotVal
}
