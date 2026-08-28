/*
FILE: futures/client.go

DESCRIPTION:
Futures-profile client. Composes the domain sub-clients (Trading / Account /
MarketData / Stream) over the shared resources of the root aster.Client
(REST transport, signer, config, logger). WS connections (market + user data)
are created lazily by the StreamClient on first Watch* call.

MAIN FUNCTIONS:
  - NewClient(parent)      : constructor (also wired into aster.Client.Futures()
                             via init-registration).
  - Trading()/Account()/MarketData()/Stream(): domain sub-client accessors.

CONCURRENCY:
All sub-clients are safe for concurrent use. Lazy WS connections are guarded
by sync.Once inside StreamClient.

DEPENDENCIES:
- root aster package: shared Config/Logger/REST/Signer.
*/

package futures

import (
	aster "github.com/tonymontanov/go-aster"
)

// init registers the futures factory in the root client, enabling
// aster.Client.Futures() for applications that import this package.
func init() {
	aster.RegisterFuturesFactory(func(parent *aster.Client) any { return NewClient(parent) })
}

// Client — futures-profile client.
type Client struct {
	parent *aster.Client

	trading    *TradingClient
	account    *AccountClient
	marketData *MarketDataClient
	stream     *StreamClient
}

// NewClient creates the futures client over the root aster.Client.
func NewClient(parent *aster.Client) *Client {
	if parent == nil {
		return nil
	}
	var c *Client = &Client{parent: parent}
	c.trading = newTradingClient(c)
	c.account = newAccountClient(c)
	c.marketData = newMarketDataClient(c)
	c.stream = newStreamClient(c)
	return c
}

// Trading returns the trading domain sub-client.
func (c *Client) Trading() *TradingClient { return c.trading }

// Account returns the account domain sub-client.
func (c *Client) Account() *AccountClient { return c.account }

// MarketData returns the market data domain sub-client.
func (c *Client) MarketData() *MarketDataClient { return c.marketData }

// Stream returns the WS streams domain sub-client.
func (c *Client) Stream() *StreamClient { return c.stream }

// internal shortcuts for sub-clients.
func (c *Client) logger() aster.Logger          { return c.parent.Logger() }
func (c *Client) rest() restDoer                { return c.parent.REST() }
func (c *Client) config() aster.Config          { return c.parent.Config() }
func (c *Client) signerEnabled() bool           { return c.parent.Signer().Enabled() }
func (c *Client) metrics() aster.CounterFactory { return c.parent.Config().Metrics }
