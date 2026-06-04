package duplosdk

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultTimeout = 60 * time.Second

// Client is the API client for the DuploCloud AI Helpdesk service.
type Client struct {
	httpClient *http.Client
	HostURL    string
	Token      string
}

// NewClient constructs a new Client. sslNoVerify disables certificate checking
// for development environments only.
func NewClient(hostURL, token string, sslNoVerify bool) (*Client, error) {
	if hostURL == "" {
		return nil, fmt.Errorf("duplo_host must not be empty")
	}
	if token == "" {
		return nil, fmt.Errorf("duplo_token must not be empty")
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: sslNoVerify}, //nolint:gosec
	}
	return &Client{
		httpClient: &http.Client{
			Timeout:   defaultTimeout,
			Transport: transport,
		},
		HostURL: strings.TrimRight(hostURL, "/"),
		Token:   token,
	}, nil
}

// ── HTTP helpers ──────────────────────────────────────────────────────────

func (c *Client) doRequest(method, path string, body interface{}) ([]byte, ClientError) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, newClientError(0, fmt.Errorf("marshalling request: %w", err))
		}
		reqBody = strings.NewReader(string(b))
	}

	req, err := http.NewRequest(method, c.HostURL+path, reqBody)
	if err != nil {
		return nil, newClientError(0, fmt.Errorf("creating request: %w", err))
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, newClientError(0, fmt.Errorf("executing request: %w", err))
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, newClientError(resp.StatusCode, fmt.Errorf("reading response: %w", err))
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, newClientError(resp.StatusCode, fmt.Errorf("not found"))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newClientError(resp.StatusCode, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody)))
	}

	return respBody, nil
}

func (c *Client) getAPI(path string, out interface{}) ClientError {
	body, err := c.doRequest(http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if jsonErr := json.Unmarshal(body, out); jsonErr != nil {
		return newClientError(0, fmt.Errorf("unmarshalling response: %w", jsonErr))
	}
	return nil
}

func (c *Client) postAPI(path string, req, out interface{}) ClientError {
	body, err := c.doRequest(http.MethodPost, path, req)
	if err != nil {
		return err
	}
	if out != nil {
		if jsonErr := json.Unmarshal(body, out); jsonErr != nil {
			return newClientError(0, fmt.Errorf("unmarshalling response: %w", jsonErr))
		}
	}
	return nil
}

func (c *Client) putAPI(path string, req, out interface{}) ClientError {
	body, err := c.doRequest(http.MethodPut, path, req)
	if err != nil {
		return err
	}
	if out != nil {
		if jsonErr := json.Unmarshal(body, out); jsonErr != nil {
			return newClientError(0, fmt.Errorf("unmarshalling response: %w", jsonErr))
		}
	}
	return nil
}

func (c *Client) deleteAPI(path string) ClientError {
	_, err := c.doRequest(http.MethodDelete, path, nil)
	return err
}
