package cmd

import (
	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/platform"
	"io"
	"net/http"
	"time"
)

// authenticatedHTTPClient returns an http.Client configured with auth headers
// and a doRequest function that adds auth headers automatically
type authenticatedClient struct {
	client  *http.Client
	token   string
	baseURL string
}

func newAuthenticatedClient(cfg *config.Config) *authenticatedClient {
	baseURL := cfg.APIURL
	if baseURL == "" {
		baseURL = config.DefaultAPIURL
	}

	token := ""
	auth, _ := config.ReadAuth()
	if auth != nil {
		token = auth.Token
	}

	return &authenticatedClient{
		client:  &http.Client{Timeout: 120 * time.Second},
		token:   token,
		baseURL: baseURL,
	}
}

func (c *authenticatedClient) Get(url string) (*http.Response, error) {
	return c.get(url, false)
}

// GetClosing is Get on a connection that is closed after the answer instead
// of being kept for the next request: for a probe the server may answer and
// then drop, so the request after it never goes out on a dying socket.
func (c *authenticatedClient) GetClosing(url string) (*http.Response, error) {
	return c.get(url, true)
}

func (c *authenticatedClient) get(url string, closing bool) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Close = closing
	platform.Prepare(req)
	req.Header.Set("Accept", "application/json")
	if err := platform.Authorize(req, c.token); err != nil {
		return nil, err
	}
	return c.client.Do(req)
}

func (c *authenticatedClient) Post(url string, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return nil, err
	}
	platform.Prepare(req)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	if err := platform.Authorize(req, c.token); err != nil {
		return nil, err
	}
	return c.client.Do(req)
}

func (c *authenticatedClient) Delete(url string) (*http.Response, error) {
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return nil, err
	}
	platform.Prepare(req)
	req.Header.Set("Accept", "application/json")
	if err := platform.Authorize(req, c.token); err != nil {
		return nil, err
	}
	return c.client.Do(req)
}
