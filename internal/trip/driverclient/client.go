package driverclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	driver "github.com/bytepharaoh/Mobix/internal/driver/domain"
	"github.com/bytepharaoh/Mobix/pkg/authn"
	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
)

type Client struct {
	base       *url.URL
	delegation authn.Delegation
	http       *http.Client
}

func New(base *url.URL, delegation authn.Delegation) *Client {
	return &Client{base: base, delegation: delegation, http: &http.Client{Timeout: 3 * time.Second}}
}

func (c *Client) Profile(ctx context.Context, driverID string) (driver.DispatchProfile, error) {
	var result driver.DispatchProfile
	err := c.request(ctx, http.MethodGet, c.base.JoinPath("internal", "drivers", driverID, "dispatch-profile"), nil, &result)
	return result, err
}

func (c *Client) Candidates(ctx context.Context, search driver.CandidateSearch) ([]driver.Candidate, error) {
	var result []driver.Candidate
	body, err := json.Marshal(search)
	if err != nil {
		return nil, fmt.Errorf("encode driver search: %w", err)
	}
	err = c.request(ctx, http.MethodPost, c.base.JoinPath("internal", "dispatch", "candidates"), body, &result)
	return result, err
}

func (c *Client) request(ctx context.Context, method string, endpoint *url.URL, body []byte, dest any) error {
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build driver request: %w", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	c.delegation.Sign(request, "driver", authn.Actor{Role: authn.Service})
	response, err := c.http.Do(request)
	if err != nil {
		return apperrors.ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound && method == http.MethodGet {
		return driver.ErrDriverNotFound
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(dest) != nil {
		return apperrors.ErrUnavailable
	}
	return nil
}
