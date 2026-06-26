package duplosdk

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const defaultTimeout = 60 * time.Second

// Client is the API client for the DuploCloud AI service.
type Client struct {
	httpClient *http.Client
	HostURL    string
	Token      string
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
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
		HostURL: strings.TrimRight(hostURL, "/"),
		Token:   token,
	}, nil
}

// ── HTTP helpers ──────────────────────────────────────────────────────────

func (c *Client) doRequest(method, path string, body interface{}) ([]byte, ClientError) {
	url := c.HostURL + path
	log.Printf("[TRACE] duplo-request: %s %s", method, url)

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, newClientError(0, fmt.Errorf("marshalling request: %w", err))
		}
		reqBody = strings.NewReader(string(b))
	}

	req, err := http.NewRequest(method, url, reqBody)
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
	body, err := c.doRequest(method, path, req)
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
