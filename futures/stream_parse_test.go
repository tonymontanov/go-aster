/*
FILE: futures/stream_parse_test.go

DESCRIPTION:
Regression tests for WS payload decoding on fixtures from the official Aster
V3 docs (Websocket Market Streams / User Data Streams sections). They pin the
CASE-SENSITIVE key contract of the wire format: "e"/"E", "U"/"u", "b"/"B",
"s"/"S", "x"/"X", "l"/"L", "n"/"N", "t"/"T" are DIFFERENT fields. A
case-insensitive codec (the json-iterator default) collapses each pair onto
one struct field and every stream fails with "readUint64: unexpected
character" — exactly what broke the first live run of the desk connector.
*/

package futures

import (
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/tonymontanov/go-aster/futures/types"
	"github.com/tonymontanov/go-aster/internal/codec"
	"github.com/tonymontanov/go-aster/orderbook"
)

// Fixtures — payload examples from the official docs (comments stripped).
const (
	fixtureDepthUpdate = `{"e":"depthUpdate","E":1571889248277,"T":1571889248276,"s":"BTCUSDT","U":390497796,"u":390497878,"pu":390497794,` +
		`"b":[["7403.89","0.002"],["7403.90","3.906"]],"a":[["7405.96","3.340"],["7406.63","4.525"]]}`
	fixtureBookTicker = `{"e":"bookTicker","u":400900217,"E":1568014460893,"T":1568014460891,"s":"BNBUSDT",` +
		`"b":"25.35190000","B":"31.21000000","a":"25.36520000","A":"40.66000000"}`
	fixtureMarkPrice = `{"e":"markPriceUpdate","E":1562305380000,"s":"BTCUSDT","p":"11794.15000000","i":"11784.62659091",` +
		`"P":"11784.25641265","r":"0.00038167","T":1562306400000}`
	fixtureAggTrade = `{"e":"aggTrade","E":123456789,"s":"BTCUSDT","a":5933014,"p":"0.001","q":"100","f":100,"l":105,"T":123456785,"m":true}`
	fixtureKline    = `{"e":"kline","E":123456789,"s":"BTCUSDT","k":{"t":123400000,"T":123460000,"s":"BTCUSDT","i":"1m","f":100,"L":200,` +
		`"o":"0.0010","c":"0.0020","h":"0.0025","l":"0.0015","v":"1000","n":100,"x":false,"q":"1.0000","V":"500","Q":"0.500","B":"123456"}}`
	fixtureMiniTicker       = `{"e":"24hrMiniTicker","E":123456789,"s":"BTCUSDT","c":"0.0025","o":"0.0010","h":"0.0025","l":"0.0010","v":"10000","q":"18"}`
	fixtureOrderTradeUpdate = `{"e":"ORDER_TRADE_UPDATE","E":1568879465651,"T":1568879465650,"o":{"s":"BTCUSDT","c":"TEST","S":"SELL",` +
		`"o":"TRAILING_STOP_MARKET","f":"GTC","q":"0.001","p":"0","ap":"0","sp":"7103.04","x":"NEW","X":"NEW","i":8886774,"l":"0","z":"0",` +
		`"L":"0","N":"USDT","n":"0","T":1568879465651,"t":0,"b":"0","a":"9.91","m":false,"R":false,"wt":"CONTRACT_PRICE",` +
		`"ot":"TRAILING_STOP_MARKET","ps":"LONG","cp":false,"AP":"7476.89","cr":"5.0","rp":"0"}}`
	fixtureAccountUpdate = `{"e":"ACCOUNT_UPDATE","E":1564745798939,"T":1564745798938,"a":{"m":"ORDER",` +
		`"B":[{"a":"USDT","wb":"122624.12345678","cw":"100.12345678","bc":"50.12345678"},{"a":"BUSD","wb":"1.00000000","cw":"0.00000000","bc":"-49.12345678"}],` +
		`"P":[{"s":"BTCUSDT","pa":"0","ep":"0.00000","cr":"200","up":"0","mt":"isolated","iw":"0.00000000","ps":"BOTH"},` +
		`{"s":"BTCUSDT","pa":"20","ep":"6563.66500","cr":"0","up":"2850.21200","mt":"isolated","iw":"13200.70726908","ps":"LONG"},` +
		`{"s":"BTCUSDT","pa":"-10","ep":"6563.86000","cr":"-45.04000000","up":"-1423.15600","mt":"isolated","iw":"6570.42511771","ps":"SHORT"}]}}`
)

// mustDecimal asserts got == want (numeric equality, not textual).
func mustDecimal(t *testing.T, field string, got decimal.Decimal, want string) {
	t.Helper()
	if !got.Equal(decimal.RequireFromString(want)) {
		t.Fatalf("%s = %s, want %s", field, got.String(), want)
	}
}

func TestParseDepthUpdateFixture(t *testing.T) {
	var raw rawDepthUpdate
	if err := codec.Unmarshal([]byte(fixtureDepthUpdate), &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if raw.EventTime != 1571889248277 || raw.TxTime != 1571889248276 || raw.Symbol != "BTCUSDT" {
		t.Fatalf("header fields: %+v", raw)
	}
	// U / u / pu — the sequencing triple; U and u differ only by case.
	if raw.FirstUpdateID != 390497796 || raw.FinalUpdateID != 390497878 || raw.PrevFinalID != 390497794 {
		t.Fatalf("U/u/pu: %d/%d/%d", raw.FirstUpdateID, raw.FinalUpdateID, raw.PrevFinalID)
	}
	var delta orderbook.Delta
	var err error
	delta, err = convertDepthUpdate(raw)
	if err != nil {
		t.Fatalf("convertDepthUpdate: %v", err)
	}
	if delta.FirstUpdateID != 390497796 || delta.FinalUpdateID != 390497878 || delta.PrevFinalUpdateID != 390497794 {
		t.Fatalf("delta sequencing: %+v", delta)
	}
	if len(delta.Bids) != 2 || len(delta.Asks) != 2 {
		t.Fatalf("levels: bids=%d asks=%d", len(delta.Bids), len(delta.Asks))
	}
}

func TestParseBookTickerFixture(t *testing.T) {
	var raw rawBookTickerEvent
	if err := codec.Unmarshal([]byte(fixtureBookTicker), &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if raw.UpdateID != 400900217 || raw.EventTime != 1568014460893 || raw.TxTime != 1568014460891 || raw.Symbol != "BNBUSDT" {
		t.Fatalf("header fields: %+v", raw)
	}
	// b/B and a/A — price vs quantity, case is the only difference.
	if raw.BidPrice != "25.35190000" || raw.BidQty != "31.21000000" || raw.AskPrice != "25.36520000" || raw.AskQty != "40.66000000" {
		t.Fatalf("b/B a/A: %+v", raw)
	}
}

func TestParseMarkPriceFixture(t *testing.T) {
	var raw rawMarkPriceEvent
	if err := codec.Unmarshal([]byte(fixtureMarkPrice), &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if raw.EventTime != 1562305380000 || raw.Symbol != "BTCUSDT" || raw.NextFundingTime != 1562306400000 {
		t.Fatalf("header fields: %+v", raw)
	}
	// p/P — mark price vs estimated settle price.
	if raw.MarkPrice != "11794.15000000" || raw.EstimatedSettlePrice != "11784.25641265" ||
		raw.IndexPrice != "11784.62659091" || raw.FundingRate != "0.00038167" {
		t.Fatalf("p/P i r: %+v", raw)
	}
}

func TestParseAggTradeFixture(t *testing.T) {
	var raw rawAggTradeEvent
	if err := codec.Unmarshal([]byte(fixtureAggTrade), &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if raw.EventTime != 123456789 || raw.Symbol != "BTCUSDT" || raw.AggTradeID != 5933014 ||
		raw.Price != "0.001" || raw.Quantity != "100" || raw.FirstTradeID != 100 || raw.LastTradeID != 105 ||
		raw.TradeTime != 123456785 || !raw.IsBuyerMaker {
		t.Fatalf("fields: %+v", raw)
	}
}

func TestParseKlineFixture(t *testing.T) {
	var raw rawKlineEvent
	if err := codec.Unmarshal([]byte(fixtureKline), &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if raw.EventTime != 123456789 || raw.Symbol != "BTCUSDT" {
		t.Fatalf("header fields: %+v", raw)
	}
	var k = raw.Kline
	// t/T — open vs close time; v/V and q/Q — total vs taker-buy volumes.
	if k.OpenTime != 123400000 || k.CloseTime != 123460000 || k.Interval != "1m" ||
		k.Open != "0.0010" || k.Close != "0.0020" || k.High != "0.0025" || k.Low != "0.0015" ||
		k.Volume != "1000" || k.TakerBuyBase != "500" || k.QuoteVolume != "1.0000" || k.TakerBuyQuote != "0.500" ||
		k.Trades != 100 || k.IsClosed {
		t.Fatalf("kline fields: %+v", k)
	}
}

func TestParseMiniTickerFixture(t *testing.T) {
	var raw rawMiniTickerEvent
	if err := codec.Unmarshal([]byte(fixtureMiniTicker), &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if raw.EventTime != 123456789 || raw.Symbol != "BTCUSDT" || raw.Close != "0.0025" || raw.Open != "0.0010" ||
		raw.High != "0.0025" || raw.Low != "0.0010" || raw.Volume != "10000" || raw.QuoteVolume != "18" {
		t.Fatalf("fields: %+v", raw)
	}
}

func TestParseOrderTradeUpdateFixture(t *testing.T) {
	var raw rawOrderUpdateEvent
	if err := codec.Unmarshal([]byte(fixtureOrderTradeUpdate), &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	var upd types.OrderUpdate = convertOrderUpdate(raw)
	if upd.EventTs != 1568879465651 || upd.TxTs != 1568879465650 {
		t.Fatalf("E/T: %+v", upd)
	}
	// s/S — symbol vs side; x/X — execution type vs status; l/L — last filled
	// qty vs price; n/N — commission vs asset; t/T — trade id vs trade time;
	// ap/AP — average price vs activation price.
	if upd.Symbol != "BTCUSDT" || upd.Side != types.SideTypeSell || upd.ClientOrderID != "TEST" || upd.OrderID != 8886774 {
		t.Fatalf("identity: %+v", upd)
	}
	if string(upd.ExecutionType) != "NEW" || upd.Status != types.OrderStatusNew {
		t.Fatalf("x/X: %q / %q", upd.ExecutionType, upd.Status)
	}
	if string(upd.Type) != "TRAILING_STOP_MARKET" || string(upd.OrigType) != "TRAILING_STOP_MARKET" ||
		string(upd.TimeInForce) != "GTC" || string(upd.PositionSide) != "LONG" || string(upd.WorkingType) != "CONTRACT_PRICE" {
		t.Fatalf("enums: %+v", upd)
	}
	if upd.CommissionAsset != "USDT" || upd.TradeTime != 1568879465651 || upd.TradeID != 0 || upd.IsMaker || upd.IsReduceOnly || upd.IsClosePosition {
		t.Fatalf("n/N t/T flags: %+v", upd)
	}
	mustDecimal(t, "OrigQuantity", upd.OrigQuantity, "0.001")
	mustDecimal(t, "StopPrice", upd.StopPrice, "7103.04")
	mustDecimal(t, "AvgPrice", upd.AvgPrice, "0")
	mustDecimal(t, "ActivationPrice", upd.ActivationPrice, "7476.89")
	mustDecimal(t, "CallbackRate", upd.CallbackRate, "5.0")
	mustDecimal(t, "AsksNotional", upd.AsksNotional, "9.91")
	mustDecimal(t, "BidsNotional", upd.BidsNotional, "0")
	mustDecimal(t, "LastFilledPrice", upd.LastFilledPrice, "0")
	mustDecimal(t, "Commission", upd.Commission, "0")
	mustDecimal(t, "RealizedProfit", upd.RealizedProfit, "0")
}

func TestParseAccountUpdateFixture(t *testing.T) {
	var raw rawAccountUpdateEvent
	if err := codec.Unmarshal([]byte(fixtureAccountUpdate), &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	var upd types.AccountUpdate = convertAccountUpdate(raw)
	if upd.EventTs != 1564745798939 || upd.TxTs != 1564745798938 || string(upd.Reason) != "ORDER" {
		t.Fatalf("header: %+v", upd)
	}
	// a (update data) vs A — and inside: B (balances) vs b, P (positions) vs p.
	if len(upd.Balances) != 2 || len(upd.Positions) != 3 {
		t.Fatalf("B/P: balances=%d positions=%d", len(upd.Balances), len(upd.Positions))
	}
	if upd.Balances[0].Asset != "USDT" || upd.Balances[1].Asset != "BUSD" {
		t.Fatalf("balance assets: %+v", upd.Balances)
	}
	mustDecimal(t, "WalletBalance", upd.Balances[0].WalletBalance, "122624.12345678")
	mustDecimal(t, "CrossWalletBalance", upd.Balances[0].CrossWalletBalance, "100.12345678")
	mustDecimal(t, "BalanceChange", upd.Balances[1].BalanceChange, "-49.12345678")

	var long types.PositionUpdate = upd.Positions[1]
	if long.Symbol != "BTCUSDT" || string(long.PositionSide) != "LONG" || !strings.EqualFold(string(long.MarginType), "isolated") {
		t.Fatalf("position identity: %+v", long)
	}
	mustDecimal(t, "PositionAmt", long.PositionAmt, "20")
	mustDecimal(t, "EntryPrice", long.EntryPrice, "6563.665")
	mustDecimal(t, "UnrealizedPnl", long.UnrealizedPnl, "2850.212")
	mustDecimal(t, "IsolatedWallet", long.IsolatedWallet, "13200.70726908")
	mustDecimal(t, "AccumulatedRealized(short)", upd.Positions[2].AccumulatedRealized, "-45.04")
}
