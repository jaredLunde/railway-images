package railwayimages

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/jaredLunde/railway-images/client/sign"
)

type Options struct {
	// The URL of your service
	URL string
	// Your service API key
	SecretKey string
	// If a signature secret key is provided, it will be used to sign URLs
	// locally instead of making a request to the server to sign the request.
	SignatureSecretKey string
}

// Create a new API client.
func NewClient(opt Options) (*Client, error) {
	if opt.URL == "" {
		return nil, fmt.Errorf("URL is required")
	}

	u, err := url.Parse(opt.URL)
	if err != nil {
		return nil, err
	}

	transport := http.DefaultTransport
	if opt.SecretKey != "" {
		transport = &SigningTransport{transport: transport, SecretKey: opt.SecretKey}
	}

	return &Client{
		URL:                u,
		SignatureSecretKey: opt.SignatureSecretKey,
		transport:          transport,
	}, nil
}

type SigningTransport struct {
	URL       *url.URL
	transport http.RoundTripper
	SecretKey string
}

func (t *SigningTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("x-api-key", t.SecretKey)
	return t.transport.RoundTrip(req)
}

type Client struct {
	URL                *url.URL
	SignatureSecretKey string
	transport          http.RoundTripper
}

// Get a signed URL for a given path. If a signature secret key is provided
// in the client options, the URL will be signed locally. Otherwise, a request
// will be made to the server to sign the URL.
func (c *Client) Sign(path string) (string, error) {
	u := *c.URL

	if c.SignatureSecretKey != "" {
		u.Path = path
		uri, err := sign.SignURL(&u, c.SignatureSecretKey)
		if err != nil {
			return "", err
		}
		return *uri, nil
	}

	signPath, err := url.JoinPath("/sign", path)
	if err != nil {
		return "", err
	}

	u.Path = signPath
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	res, err := c.transport.RoundTrip(req)
	if err != nil {
		return "", err
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

// Get a file from the storage server
func (c *Client) Get(key string) (*http.Response, error) {
	u := *c.URL
	path, err := url.JoinPath("/files", key)
	if err != nil {
		return nil, err
	}
	u.Path = path
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	res, err := c.transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	return res, nil
}

// Put a file to the storage server
func (c *Client) Put(key string, r io.Reader) error {
	// Create URL
	u := *c.URL
	u.Path = fmt.Sprintf("/files/%s", key)

	// Create request
	req, err := http.NewRequest(http.MethodPut, u.String(), r)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set content type if possible
	if rc, ok := r.(io.ReadCloser); ok {
		defer rc.Close()
	}

	// Send request
	res, err := c.transport.RoundTrip(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer res.Body.Close()

	// Read error response body if status is not 201
	if res.StatusCode != http.StatusCreated {
		body, err := io.ReadAll(res.Body)
		if err != nil {
			return fmt.Errorf("unexpected status code %d and failed to read error body: %w", res.StatusCode, err)
		}
		return fmt.Errorf("unexpected status code %d: %s", res.StatusCode, string(body))
	}

	return nil
}

// Delete a file from the storage server
func (c *Client) Delete(key string) error {
	u := *c.URL
	path, err := url.JoinPath("/files", key)
	if err != nil {
		return err
	}
	u.Path = path
	req, err := http.NewRequest(http.MethodDelete, u.String(), nil)
	if err != nil {
		return err
	}

	res, err := c.transport.RoundTrip(req)
	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}

	return nil
}

type ListResult struct {
	Keys     []string `json:"keys"`
	NextPage string   `json:"next_page,omitempty"`
	HasMore  bool     `json:"has_more"`
}

type ListOptions struct {
	// The maximum number of keys to return
	Limit int
	// The key to start listing from
	StartingAt string
	// If true, list unlinked (soft deleted) files
	Unlinked bool
}

// List files in the storage server
func (c *Client) List(opts ListOptions) (*ListResult, error) {
	u := *c.URL
	u.Path = "/files"

	// Build query parameters
	q := u.Query()
	if opts.Limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", opts.Limit))
	}
	if opts.StartingAt != "" {
		q.Set("starting_at", opts.StartingAt)
	}
	if opts.Unlinked {
		q.Set("unlinked", "true")
	}
	u.RawQuery = q.Encode()

	// Create and send request
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	res, err := c.transport.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer res.Body.Close()

	// Handle non-200 responses
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", res.StatusCode, string(body))
	}

	// Parse response
	var result ListResult
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}
