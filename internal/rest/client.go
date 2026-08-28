/*
FILE: internal/rest/client.go

DESCRIPTION:
Low-level SDK REST client. A thin layer over http.Client that:
  1. assembles the parameter payload (url-encoded, sorted by key);
  2. signs the request when required (auth.Signer, EIP-712 — the signature is
     appended as the LAST parameter of the payload);
  3. places the payload into the query string (GET) or the
     application/x-www-form-urlencoded body (POST/PUT/DELETE) per the Aster
     V3 convention;
  4. executes the HTTP call with deadline from ctx or Config.RequestTimeout;
  5. collects the X-MBX-USED-WEIGHT-* / X-MBX-ORDER-COUNT-* rate-limit
     headers and notifies the observer;
  6. maps errors to *asterr.Error with the correct category.

ASTER SPECIFICS:
Unlike OKX/Bybit there is NO response envelope: a successful body is the
payload itself, an error body is `{"code": <negative int>, "msg": "..."}`
delivered with a 4xx/5xx status. As a safety net a 2xx body that textually
starts with `{"code":-` is also treated as an exchange error (the docs show
such payloads for e.g. nonce expiry).

IMPORT NOTE:
  - Does NOT import the root aster package (it imports rest), to avoid an
    import cycle. All required types (Error/ErrorKind/Logger/Config) live in
    internal/asterr, internal/asterlog, and the local Config.
*/

package rest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tonymontanov/go-aster/internal/asterlog"
	"github.com/tonymontanov/go-aster/internal/asterr"
	"github.com/tonymontanov/go-aster/internal/auth"
	"github.com/tonymontanov/go-aster/internal/codec"
)

// Config — REST transport parameters. Populated from the public
// aster.RestConfig in the root package (explicit struct conversion is done
// there to avoid an import cycle).
type Config struct {
	RequestTimeout      time.Duration
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
	// RateLimitEventObserver — optional callback. Receives request metadata
	// (endpoint, method, rate-limit headers, RequestMeta) that the root
	// aster.Client converts into the public aster.RateLimitEvent.
	// nil → no-op.
	RateLimitEventObserver func(endpoint, method string, headers map[string]string, meta RequestMeta)
}

// RequestMeta — request metadata known at the domain layer
// (futures/trading.go, futures/account.go) that is needed by an external
// rate-limiter for accurate limit tracking. Populated by the calling method
// and forwarded through rest.Options to RateLimitEventObserver. If empty, the
// observer receives zero values (count=0, no symbols, category="").
type RequestMeta struct {
	// OrderCount — number of orders affected by the request. 1 for single, N
	// for batch, 0 for non-trading. See aster.RateLimitEvent.OrderCount.
	OrderCount int
	// Symbols — list of symbols. See aster.RateLimitEvent.Symbols.
	Symbols []string
	// Category — string representation of aster.RateLimitCategory
	// ("place"/"amend"/"cancel"/"query"/"market"/""). Passed as a string
	// to avoid an import cycle internal/rest ↔ root aster.
	Category string
}

// Options — parameters for a single REST request.
type Options struct {
	Method string
	Path   string
	// Params — business request parameters. For signed requests the SDK adds
	// nonce/signer/signature on top; the map is mutated in place.
	Params url.Values
	Signed bool
	// Meta — metadata for RateLimitEventObserver. If zero, the observer
	// receives zeros. Populated by futures/* domain methods where symbol /
	// batch size / request category are known.
	Meta RequestMeta
}

// Response — raw response payload. Aster has no envelope: the body is the
// payload itself. Unmarshal decodes it into a typed destination.
type Response struct {
	// Raw — response body bytes.
	Raw codec.RawMessage
	// Status — HTTP status code.
	Status int
}

// Unmarshal decodes the response body into an arbitrary dest.
func (r Response) Unmarshal(dest any) error {
	if len(r.Raw) == 0 || bytes.Equal(r.Raw, []byte("null")) {
		return nil
	}
	return codec.Unmarshal(r.Raw, dest)
}

// Client — low-level REST client.
type Client struct {
	httpClient             *http.Client
	signer                 *auth.Signer
	baseURL                string
	userAgent              string
	logger                 asterlog.Logger
	rateLimitEventObserver func(endpoint, method string, headers map[string]string, meta RequestMeta)
}

// NewClient creates a REST client.
func NewClient(baseURL string, signer *auth.Signer, cfg Config, ua string, log asterlog.Logger) *Client {
	if log == nil {
		log = asterlog.Noop()
	}
	var transport *http.Transport = &http.Transport{
		MaxIdleConns:        cfg.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:     cfg.IdleConnTimeout,
		ForceAttemptHTTP2:   true,
	}
	var httpClient *http.Client = &http.Client{
		Timeout:   cfg.RequestTimeout,
		Transport: transport,
	}
	return &Client{
		httpClient:             httpClient,
		signer:                 signer,
		baseURL:                strings.TrimRight(baseURL, "/"),
		userAgent:              ua,
		logger:                 log,
		rateLimitEventObserver: cfg.RateLimitEventObserver,
	}
}

// Signer returns the signer used by this client (for domain-layer checks).
func (c *Client) Signer() *auth.Signer { return c.signer }

// Close closes idle transport connections.
func (c *Client) Close() {
	if c == nil || c.httpClient == nil {
		return
	}
	if t, ok := c.httpClient.Transport.(*http.Transport); ok {
		t.CloseIdleConnections()
	}
}

/*
Do executes a single REST call and returns the raw Response, the collected
X-MBX-* rate-limit headers, and an error.

Error semantics:
  - transport failures (DNS, timeout, ctx cancel) → ErrorKindNetwork;
  - non-2xx with a parseable {code,msg} body → category via MapAsterCode;
  - non-2xx without a parseable body → category via MapHTTPStatus;
  - 2xx body that starts with {"code":- → exchange error (safety net);
  - signing failure (disabled signer) → ErrorKindAuth.
*/
func (c *Client) Do(ctx context.Context, opts Options) (Response, map[string]string, error) {
	var resp Response

	var params url.Values = opts.Params
	if params == nil {
		params = url.Values{}
	}

	var payload string
	var err error
	if opts.Signed {
		if !c.signer.Enabled() {
			return resp, map[string]string{}, asterr.New(asterr.ErrorKindAuth, 0, "rest: credentials required (private key not configured)", auth.ErrSignerDisabled)
		}
		payload, err = c.signer.SignParams(params)
		if err != nil {
			return resp, map[string]string{}, asterr.New(asterr.ErrorKindAuth, 0, "rest: sign request", err)
		}
	} else {
		payload = params.Encode()
	}

	var method string = strings.ToUpper(opts.Method)
	var fullURL string = c.baseURL + opts.Path
	var bodyReader io.Reader
	var hasBody bool
	// Aster V3 convention: GET carries parameters in the query string;
	// POST/PUT/DELETE carry them in the x-www-form-urlencoded body. The signed
	// payload string is sent VERBATIM — the signature covers exactly these bytes.
	if method == http.MethodGet {
		if payload != "" {
			fullURL = fullURL + "?" + payload
		}
	} else if payload != "" {
		bodyReader = bytes.NewBufferString(payload)
		hasBody = true
	}

	var req *http.Request
	req, err = http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return resp, map[string]string{}, asterr.New(asterr.ErrorKindInvalidRequest, 0, "rest: build request", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	if hasBody {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	var httpResp *http.Response
	var started time.Time = time.Now()
	httpResp, err = c.httpClient.Do(req)
	if err != nil {
		return resp, map[string]string{}, classifyTransportError(err)
	}
	defer func() {
		_ = httpResp.Body.Close()
	}()

	var rateLimits map[string]string = collectRateLimitHeaders(httpResp.Header)

	// Notify the observer BEFORE parsing the body: even if the response is
	// invalid JSON or contains an exchange error, the observer must still fire
	// so that the external rate-limiter can update its accounting.
	if c.rateLimitEventObserver != nil {
		c.rateLimitEventObserver(opts.Path, method, rateLimits, opts.Meta)
	}

	var raw []byte
	raw, err = io.ReadAll(httpResp.Body)
	if err != nil {
		return resp, rateLimits, asterr.New(asterr.ErrorKindNetwork, 0, "rest: read body", err)
	}
	resp.Raw = raw
	resp.Status = httpResp.StatusCode

	c.logger.Debug(
		"rest.Do",
		asterlog.Str("method", method),
		asterlog.Str("path", opts.Path),
		asterlog.Int("status", int64(httpResp.StatusCode)),
		asterlog.Int("durationMs", time.Since(started).Milliseconds()),
		asterlog.Int("bytes", int64(len(raw))),
	)

	if httpResp.StatusCode >= 200 && httpResp.StatusCode < 300 {
		// Safety net: the docs show exchange errors (e.g. -4225 Nonce Expired)
		// that may arrive with a 2xx status. A genuine payload never starts
		// with a negative "code" field.
		if bytes.HasPrefix(raw, errBodyPrefix) {
			var apiErr apiErrorBody
			if parseErr := codec.Unmarshal(raw, &apiErr); parseErr == nil && apiErr.Code < 0 {
				return resp, rateLimits, &asterr.Error{
					Kind:       asterr.MapAsterCode(apiErr.Code, apiErr.Msg),
					HTTPStatus: httpResp.StatusCode,
					AsterCode:  apiErr.Code,
					Message:    apiErr.Msg,
				}
			}
		}
		return resp, rateLimits, nil
	}

	var apiErr apiErrorBody
	if err = codec.Unmarshal(raw, &apiErr); err == nil && apiErr.Code != 0 {
		return resp, rateLimits, &asterr.Error{
			Kind:       asterr.MapAsterCode(apiErr.Code, apiErr.Msg),
			HTTPStatus: httpResp.StatusCode,
			AsterCode:  apiErr.Code,
			Message:    apiErr.Msg,
		}
	}
	return resp, rateLimits, &asterr.Error{
		Kind:       asterr.MapHTTPStatus(httpResp.StatusCode),
		HTTPStatus: httpResp.StatusCode,
		Message:    truncate(string(raw), 256),
	}
}

// apiErrorBody — Aster error payload: {"code": -1121, "msg": "Invalid symbol."}.
type apiErrorBody struct {
	Code int64  `json:"code"`
	Msg  string `json:"msg"`
}

// errBodyPrefix — textual prefix of an exchange error delivered with 2xx.
var errBodyPrefix []byte = []byte(`{"code":-`)

// collectRateLimitHeaders extracts the X-MBX-USED-WEIGHT-* and
// X-MBX-ORDER-COUNT-* families from response headers. Keys keep the canonical
// http.Header form (e.g. "X-Mbx-Used-Weight-1m"). Always returns a non-nil map.
func collectRateLimitHeaders(h http.Header) map[string]string {
	var out map[string]string = map[string]string{}
	var key string
	var values []string
	for key, values = range h {
		if len(values) == 0 {
			continue
		}
		if strings.HasPrefix(key, "X-Mbx-Used-Weight-") || strings.HasPrefix(key, "X-Mbx-Order-Count-") {
			out[key] = values[0]
		}
	}
	return out
}

// classifyTransportError converts a network/ctx error into a *asterr.Error.
func classifyTransportError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return asterr.New(asterr.ErrorKindNetwork, 0, "rest: context canceled", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return asterr.New(asterr.ErrorKindNetwork, 0, "rest: deadline exceeded", err)
	}
	return asterr.New(asterr.ErrorKindNetwork, 0, "rest: transport error", err)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
