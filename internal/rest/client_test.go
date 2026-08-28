/*
FILE: internal/rest/client_test.go

DESCRIPTION:
Tests of the REST transport: signed payload wire format (sorted params,
nonce/signer injection, trailing signature verifiable by re-signing),
GET vs POST parameter placement, error mapping (4xx body, bare status,
2xx safety net) and rate-limit header collection.
*/

package rest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/tonymontanov/go-aster/internal/asterr"
	"github.com/tonymontanov/go-aster/internal/auth"
)

// Public demonstration credentials from the Aster V3 docs.
const (
	testPrivateKey = "0x4fd0a42218f3eae43a6ce26d22544e986139a01e5b34a62db53757ffca81bae1"
	testSigner     = "0x21cF8Ae13Bb72632562c6Fff438652Ba1a151bb0"
	testUser       = "0x63DD5aCC6b1aa0f563956C0e534DD30B6dcF7C4e"
)

func newTestSigner(t *testing.T) *auth.Signer {
	t.Helper()
	var s *auth.Signer
	var err error
	s, err = auth.NewSigner(testUser, testSigner, testPrivateKey, 0)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	return s
}

func newTestClient(t *testing.T, srv *httptest.Server, cfg Config) *Client {
	t.Helper()
	return NewClient(srv.URL, newTestSigner(t), cfg, "go-aster/test", nil)
}

// verifySignedPayload checks the wire payload contract: the signature is the
// LAST parameter, and re-signing the preceding string with the same key
// yields the same signature (deterministic RFC 6979 ECDSA).
func verifySignedPayload(t *testing.T, payload string) url.Values {
	t.Helper()
	var idx int = strings.LastIndex(payload, "&signature=")
	if idx < 0 {
		t.Fatalf("payload has no trailing signature: %q", payload)
	}
	var signedPart string = payload[:idx]
	var gotSig string = payload[idx+len("&signature="):]

	var refSigner *auth.Signer = newTestSigner(t)
	var wantSig string
	var err error
	wantSig, err = refSigner.SignMessage(signedPart)
	if err != nil {
		t.Fatalf("reference SignMessage: %v", err)
	}
	if gotSig != wantSig {
		t.Fatalf("signature not verifiable:\n got %s\nwant %s", gotSig, wantSig)
	}

	var params url.Values
	params, err = url.ParseQuery(signedPart)
	if err != nil {
		t.Fatalf("parse signed payload: %v", err)
	}
	if params.Get("nonce") == "" {
		t.Fatal("nonce missing from signed payload")
	}
	if params.Get("signer") != testSigner {
		t.Fatalf("signer = %q, want %q", params.Get("signer"), testSigner)
	}
	// The payload must be the canonical sorted encoding of its parameters.
	if params.Encode() != signedPart {
		t.Fatalf("payload is not sorted-canonical:\n got %s\nwant %s", signedPart, params.Encode())
	}
	return params
}

func TestDoSignedPostBody(t *testing.T) {
	var gotBody string
	var gotContentType string
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw, _ = io.ReadAll(r.Body)
		gotBody = string(raw)
		gotContentType = r.Header.Get("Content-Type")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	var c *Client = newTestClient(t, srv, Config{})
	var params url.Values = url.Values{}
	params.Set("symbol", "BTCUSDT")
	params.Set("side", "BUY")

	var err error
	_, _, err = c.Do(context.Background(), Options{Method: "POST", Path: "/fapi/v3/order", Params: params, Signed: true})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Fatalf("Content-Type = %q", gotContentType)
	}
	var sent url.Values = verifySignedPayload(t, gotBody)
	if sent.Get("symbol") != "BTCUSDT" || sent.Get("side") != "BUY" {
		t.Fatalf("business params lost: %v", sent)
	}
}

func TestDoSignedGetQuery(t *testing.T) {
	var gotQuery string
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	var c *Client = newTestClient(t, srv, Config{})
	var params url.Values = url.Values{}
	params.Set("symbol", "BTCUSDT")

	var err error
	_, _, err = c.Do(context.Background(), Options{Method: "GET", Path: "/fapi/v3/openOrders", Params: params, Signed: true})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = verifySignedPayload(t, gotQuery)
}

func TestDoUnsignedHasNoAuthParams(t *testing.T) {
	var gotQuery string
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	var c *Client = newTestClient(t, srv, Config{})
	var params url.Values = url.Values{}
	params.Set("symbol", "BTCUSDT")

	var err error
	_, _, err = c.Do(context.Background(), Options{Method: "GET", Path: "/fapi/v3/depth", Params: params})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if strings.Contains(gotQuery, "signature") || strings.Contains(gotQuery, "nonce") {
		t.Fatalf("unsigned request leaked auth params: %q", gotQuery)
	}
}

func TestDoErrorMapping(t *testing.T) {
	var cases = []struct {
		name     string
		status   int
		body     string
		wantKind asterr.ErrorKind
		wantCode int64
	}{
		{"invalid symbol", 400, `{"code":-1121,"msg":"Invalid symbol."}`, asterr.ErrorKindInvalidRequest, -1121},
		{"rate limited", 429, `{"code":-1003,"msg":"Too many requests."}`, asterr.ErrorKindRateLimit, -1003},
		{"auth", 401, `{"code":-1022,"msg":"Signature for this request is not valid."}`, asterr.ErrorKindAuth, -1022},
		{"exchange reject", 400, `{"code":-2010,"msg":"Order would immediately trigger."}`, asterr.ErrorKindExchange, -2010},
		{"bare status", 503, `upstream unavailable`, asterr.ErrorKindNetwork, 0},
		{"ip ban", 418, ``, asterr.ErrorKindRateLimit, 0},
		{"error with 2xx", 200, `{"code":-4225,"msg":"Nonce Expired"}`, asterr.ErrorKindInvalidRequest, -4225},
	}

	var tc struct {
		name     string
		status   int
		body     string
		wantKind asterr.ErrorKind
		wantCode int64
	}
	for _, tc = range cases {
		t.Run(tc.name, func(t *testing.T) {
			var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			var c *Client = newTestClient(t, srv, Config{})
			var err error
			_, _, err = c.Do(context.Background(), Options{Method: "GET", Path: "/x"})
			if err == nil {
				t.Fatal("expected error")
			}
			var apiErr *asterr.Error
			var ok bool
			apiErr, ok = err.(*asterr.Error)
			if !ok {
				t.Fatalf("error type %T", err)
			}
			if apiErr.Kind != tc.wantKind {
				t.Fatalf("kind = %v, want %v", apiErr.Kind, tc.wantKind)
			}
			if apiErr.AsterCode != tc.wantCode {
				t.Fatalf("code = %d, want %d", apiErr.AsterCode, tc.wantCode)
			}
		})
	}
}

func TestDoSuccessWithPositiveCodePayload(t *testing.T) {
	// {"code":"200",...} (cancel-all ack) and similar non-negative payloads
	// must NOT be treated as errors.
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":"200","msg":"The operation of cancel all open order is done."}`))
	}))
	defer srv.Close()

	var c *Client = newTestClient(t, srv, Config{})
	var err error
	_, _, err = c.Do(context.Background(), Options{Method: "DELETE", Path: "/fapi/v3/allOpenOrders"})
	if err != nil {
		t.Fatalf("positive code payload misclassified as error: %v", err)
	}
}

func TestDoRateLimitHeadersAndObserver(t *testing.T) {
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-MBX-USED-WEIGHT-1M", "12")
		w.Header().Set("X-MBX-ORDER-COUNT-1M", "3")
		w.Header().Set("X-Other", "ignored")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	var observed struct {
		endpoint string
		method   string
		headers  map[string]string
		meta     RequestMeta
		calls    int
	}
	var cfg Config = Config{
		RateLimitEventObserver: func(endpoint, method string, headers map[string]string, meta RequestMeta) {
			observed.endpoint = endpoint
			observed.method = method
			observed.headers = headers
			observed.meta = meta
			observed.calls++
		},
	}

	var c *Client = newTestClient(t, srv, cfg)
	var meta RequestMeta = RequestMeta{OrderCount: 2, Symbols: []string{"BTCUSDT"}, Category: "place"}
	var rateLimits map[string]string
	var err error
	_, rateLimits, err = c.Do(context.Background(), Options{Method: "POST", Path: "/fapi/v3/batchOrders", Signed: true, Meta: meta})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	if rateLimits["X-Mbx-Used-Weight-1m"] != "12" || rateLimits["X-Mbx-Order-Count-1m"] != "3" {
		t.Fatalf("rate limit headers not collected: %v", rateLimits)
	}
	if len(rateLimits) != 2 {
		t.Fatalf("unrelated headers leaked: %v", rateLimits)
	}
	if observed.calls != 1 || observed.endpoint != "/fapi/v3/batchOrders" || observed.method != "POST" {
		t.Fatalf("observer call wrong: %+v", observed)
	}
	if observed.meta.OrderCount != 2 || observed.meta.Category != "place" {
		t.Fatalf("meta not forwarded: %+v", observed.meta)
	}
}

func TestDoSignedWithoutCredentials(t *testing.T) {
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request must not reach the network without credentials")
	}))
	defer srv.Close()

	var disabled *auth.Signer
	var err error
	disabled, err = auth.NewSigner("", "", "", 0)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	var c *Client = NewClient(srv.URL, disabled, Config{}, "go-aster/test", nil)

	_, _, err = c.Do(context.Background(), Options{Method: "POST", Path: "/fapi/v3/order", Signed: true})
	if !asterr.IsAuth(err) {
		t.Fatalf("expected auth error, got %v", err)
	}
}
