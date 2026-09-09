package portal

import (
	"errors"
	"net/http"
	"net/url"
)

// resolveLocation handles server-provided navigation and download locations.
func (c *Client) resolveLocation(location string) (string, error) {
	u, err := url.Parse(location)
	if err != nil {
		return "", errors.New("invalid portal location")
	}
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return "", errors.New("invalid portal base URL")
	}
	u = base.ResolveReference(u)
	if err := c.checkDestination(u); err != nil {
		return "", err
	}
	return u.String(), nil
}

// checkDestination pins every request to the origin chosen by New. Do not
// include the rejected URL in errors: its query may contain a session token.
func (c *Client) checkDestination(u *url.URL) error {
	if u == nil || c.allowedOrigin == nil || u.Opaque != "" || u.User != nil ||
		u.Scheme != c.allowedOrigin.Scheme || u.Host != c.allowedOrigin.Host {
		return errors.New("blocked request outside the configured portal origin")
	}
	return nil
}

// CheckRedirect runs before net/http sends the redirected request. This also
// protects credentials replayed by 307/308 and custom AJAX token headers,
// which net/http's default cross-host header policy does not remove.
func (c *Client) checkRedirect(req *http.Request, via []*http.Request) error {
	if err := c.checkDestination(req.URL); err != nil {
		return err
	}
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	return nil
}
