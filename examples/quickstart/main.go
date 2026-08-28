/*
FILE: examples/quickstart/main.go

DESCRIPTION:
Minimal end-to-end example of the go-aster SDK Futures section:
  - public REST: server time, symbol rules, order book snapshot;
  - public WS: live order book + best bid/ask;
  - (commented) private REST: order placement with credentials from env.

USAGE:

	go run ./examples/quickstart                     # public data only
	ASTER_USER=0x... ASTER_PRIVATE_KEY=0x... \
	  go run ./examples/quickstart                   # + private section

Testnet: add ASTER_TESTNET=1 (requires a testnet API wallet from
https://www.asterdex-testnet.com/en/api-wallet).
*/

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	aster "github.com/tonymontanov/go-aster"
	"github.com/tonymontanov/go-aster/futures"
	futurestypes "github.com/tonymontanov/go-aster/futures/types"
)

func main() {
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var client *aster.Client
	var err error
	client, err = aster.NewClient(aster.Config{
		User:       os.Getenv("ASTER_USER"),
		PrivateKey: os.Getenv("ASTER_PRIVATE_KEY"),
		Testnet:    os.Getenv("ASTER_TESTNET") == "1",
	})
	if err != nil {
		fmt.Println("client init failed:", err)
		os.Exit(1)
	}
	defer func() {
		_ = client.Close()
	}()

	var f *futures.Client = client.Futures().(*futures.Client)
	var symbol string = "BTCUSDT"

	// --- Public REST -----------------------------------------------------
	var serverTime int64
	serverTime, err = f.MarketData().GetServerTime(ctx)
	if err != nil {
		fmt.Println("server time failed:", err)
		os.Exit(1)
	}
	fmt.Println("server time:", time.UnixMilli(serverTime).UTC())

	var symbolInfo futurestypes.SymbolInfo
	symbolInfo, err = f.MarketData().GetSymbolInfo(ctx, symbol)
	if err != nil {
		fmt.Println("symbol info failed:", err)
		os.Exit(1)
	}
	fmt.Printf("%s: tickSize=%s stepSize=%s minNotional=%s\n",
		symbol, symbolInfo.TickSize, symbolInfo.StepSize, symbolInfo.MinNotional)

	// --- Public WS: live order book --------------------------------------
	err = f.Stream().WatchOrderbook(ctx, symbol, 5,
		func(book futurestypes.OrderBookSnapshot) {
			if len(book.Bids) > 0 && len(book.Asks) > 0 {
				fmt.Printf("book u=%d best %s@%s | %s@%s\n",
					book.LastUpdateID,
					book.Bids[0].Size, book.Bids[0].Price,
					book.Asks[0].Size, book.Asks[0].Price)
			}
		},
		func(watchErr error) {
			fmt.Println("orderbook stream error:", watchErr)
		},
	)
	if err != nil {
		fmt.Println("watch orderbook failed:", err)
		os.Exit(1)
	}

	// --- Private REST (requires credentials) ------------------------------
	if os.Getenv("ASTER_PRIVATE_KEY") != "" {
		var balances []futurestypes.Balance
		balances, err = f.Account().GetBalances(ctx)
		if err != nil {
			fmt.Println("balances failed:", err)
		} else {
			var i int
			for i = 0; i < len(balances); i++ {
				fmt.Printf("balance %s: wallet=%s available=%s\n",
					balances[i].Asset, balances[i].WalletBalance, balances[i].AvailableBalance)
			}
		}
	}

	<-ctx.Done()
	fmt.Println("bye")
}
