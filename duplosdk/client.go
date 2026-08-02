package duplosdk

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

const defaultTimeout = 60 * time.Second

// bodyLogEnv turns on request/response BODY logging. Off unless the env var is
// set to a truthy value, because bodies routinely carry credentials and would
// otherwise end up in any log a user shares. Intended for local debugging
// against a dev host:
//
//	DUPLO_LOG_HTTP_BODIES=1 TF_LOG_PROVIDER=TRACE terraform apply
//
// Values are redacted by key name before anything is written — see redactBody.
const bodyLogEnv = "DUPLO_LOG_HTTP_BODIES"

// maxLoggedBody caps each logged body so a large list response cannot bury the
// rest of the log.
const maxLoggedBody = 8192

func bodyLoggingEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(bodyLogEnv))) {
	case "", "0", "false", "no":
		return false
	}
	return true
}

// secretJSONKeyRe matches JSON keys whose values must never reach a log:
// passwords, tokens, secrets, keys, and the credential value field. Matched on
// the key name so it works without knowing any particular resource's shape.
var secretJSONKeyRe = regexp.MustCompile(
	`(?i)"([^"]*(password|secret|token|apikey|api_key|credential|privatekey|private_key|certificateauthoritydata)[^"]*)"\s*:\s*"[^"]*"`)

// redactBody replaces secret-looking JSON string values with REDACTED and
// truncates the result. Best-effort: it is a debugging aid, not a guarantee, so
// never enable body logging against a host whose data you would not paste into
// a ticket.
func redactBody(b []byte) string {
	if len(b) == 0 {
		return "(empty)"
	}
	s := secretJSONKeyRe.ReplaceAllString(string(b), `"$1":"REDACTED"`)
	if len(s) > maxLoggedBody {
		s = s[:maxLoggedBody] + fmt.Sprintf("... (truncated, %d bytes total)", len(b))
	}
	return s
}

// Client is the API client for the DuploCloud AI service.
type Client struct {
	httpClient *http.Client
	HostURL    string
	Token      string

	// timeout is the per-request deadline applied when a caller does not ask for
	// a longer one. It lives here rather than on http.Client.Timeout so a single
	// slow operation can be given more room without loosening every other call:
	// http.Client.Timeout is a hard ceiling that would clamp a longer per-request
	// context. See callAPIWithTimeout.
	timeout time.Duration
}

// NewClient constructs a new Client. sslNoVerify disables certificate checking
// for development environments only. A timeout <= 0 falls back to
// defaultTimeout.
func NewClient(hostURL, token string, sslNoVerify bool, timeout time.Duration) (*Client, error) {
	if hostURL == "" {
		return nil, fmt.Errorf("duplo_host must not be empty")
	}
	if token == "" {
		return nil, fmt.Errorf("duplo_token must not be empty")
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: sslNoVerify}, //nolint:gosec
	}
	return &Client{
		// No http.Client.Timeout: it is a ceiling over the whole call and would
		// clamp the longer per-request deadlines that slow operations need. Every
		// request instead carries a context deadline (c.timeout by default), so
		// nothing can hang indefinitely.
		httpClient: &http.Client{Transport: transport},
		HostURL:    strings.TrimRight(hostURL, "/"),
		Token:      token,
		timeout:    timeout,
	}, nil
}

// ── HTTP helpers ──────────────────────────────────────────────────────────

func (c *Client) doRequest(method, path string, body interface{}) ([]byte, ClientError) {
	return c.doRequestWithTimeout(c.timeout, method, path, body)
}

// doRequestWithTimeout issues one request under an explicit deadline. A
// non-positive or shorter-than-default timeout falls back to the client
// default, so a caller can only ever ask for MORE room, never less.
func (c *Client) doRequestWithTimeout(timeout time.Duration, method, path string, body interface{}) ([]byte, ClientError) {
	if timeout < c.timeout {
		timeout = c.timeout
	}
	url := c.HostURL + path
	log.Printf("[TRACE] duplo-request: %s %s (deadline=%s)", method, url, timeout)

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, newClientError(0, fmt.Errorf("marshalling request: %w", err))
		}
		if bodyLoggingEnabled() {
			log.Printf("[TRACE] duplo-request-body: %s %s: %s", method, url, redactBody(b))
		}
		reqBody = strings.NewReader(string(b))
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		log.Printf("[TRACE] duplo-request: %s %s: cannot build request: %s", method, url, err)
		return nil, newClientError(0, fmt.Errorf("creating request: %w", err))
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("[TRACE] duplo-request: %s %s: failed: %s", method, url, err)
		return nil, newClientError(0, fmt.Errorf("executing request: %w", err))
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, newClientError(resp.StatusCode, fmt.Errorf("reading response: %w", err))
	}

	log.Printf("[TRACE] duplo-response: %s %s: status=%d", method, url, resp.StatusCode)
	if bodyLoggingEnabled() {
		log.Printf("[TRACE] duplo-response-body: %s %s: status=%d: %s",
			method, url, resp.StatusCode, redactBody(respBody))
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, newClientError(resp.StatusCode, fmt.Errorf("not found"))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Surface whatever the backend returned. Many auth/permission failures
		// (401/403) come back with an empty body and put the reason in the
		// WWW-Authenticate header instead, so fall back to that + the HTTP
		// status text rather than emitting a blank message.
		detail := strings.TrimSpace(string(respBody))
		if detail == "" {
			detail = http.StatusText(resp.StatusCode)
			if wa := strings.TrimSpace(resp.Header.Get("WWW-Authenticate")); wa != "" {
				detail += " (WWW-Authenticate: " + wa + ")"
			}
		}
		log.Printf("[TRACE] duplo-response: %s %s: error body: %q", method, url, string(respBody))
		return nil, newClientError(resp.StatusCode, fmt.Errorf("API error %d: %s", resp.StatusCode, detail))
	}

	return respBody, nil
}

// callAPI issues a request with an arbitrary HTTP method and optionally decodes
// the response body into out. A nil req sends no body; a nil out discards the
// response. This one helper backs every CRUD verb, including the non-REST verbs
// a resource Endpoint may configure.
func (c *Client) callAPI(method, path string, req, out interface{}) ClientError {
	return c.callAPIWithTimeout(0, method, path, req, out)
}

// callAPIWithTimeout is callAPI under an explicit per-request deadline. Use for
// operations the backend performs synchronously and slowly — a cloud teardown
// can run for minutes, and disconnecting mid-flight does not merely lose the
// reply, it cancels the server's work.
func (c *Client) callAPIWithTimeout(timeout time.Duration, method, path string, req, out interface{}) ClientError {
	body, err := c.doRequestWithTimeout(timeout, method, path, req)
	if err != nil {
		return err
	}
	if out != nil {
		// UseNumber so JSON numbers decode as json.Number (not float64),
		// preserving int64 precision beyond 2^53.
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.UseNumber()
		if jsonErr := dec.Decode(out); jsonErr != nil {
			return newClientError(0, fmt.Errorf("unmarshalling response: %w", jsonErr))
		}
	}
	return nil
}
