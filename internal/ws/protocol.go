/*
FILE: internal/ws/protocol.go

DESCRIPTION:
Wire structures of the Aster WS protocol (Binance-style):
  - opRequest      — SUBSCRIBE/UNSUBSCRIBE control frames with a correlation id;
  - combinedEnvelope — {"stream":"<name>","data":<payload>} wrapper used on
    the combined endpoint (/stream);
  - controlReply   — {"result":null,"id":N} acks and {"error":{...},"id":N}
    rejections; also matches LIST_SUBSCRIPTIONS responses.

The market connection runs in combined mode (dynamic SUBSCRIBE over /stream);
the user-data connection is a raw stream (/ws/<listenKey>) whose frames are
event payloads without an envelope.
*/

package ws

import "github.com/tonymontanov/go-aster/internal/codec"

// opRequest — SUBSCRIBE / UNSUBSCRIBE control frame.
type opRequest struct {
	Method string   `json:"method"`
	Params []string `json:"params"`
	ID     uint64   `json:"id"`
}

// combinedEnvelope — combined stream event wrapper.
type combinedEnvelope struct {
	Stream string           `json:"stream"`
	Data   codec.RawMessage `json:"data"`
}

// wsError — error object of a rejected control frame.
type wsError struct {
	Code int64  `json:"code"`
	Msg  string `json:"msg"`
}

// controlReply — control frame response. ID pairs it with the request.
type controlReply struct {
	Result codec.RawMessage `json:"result"`
	Error  *wsError         `json:"error"`
	ID     uint64           `json:"id"`
}
