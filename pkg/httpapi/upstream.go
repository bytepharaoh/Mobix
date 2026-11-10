package httpapi

import (
	"fmt"
	"net/url"
)

func Upstream(address string) (*url.URL, error) {
	u, err := url.Parse(address)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") ||
		u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, fmt.Errorf("upstream address must be an HTTP(S) origin")
	}
	u.Path = ""
	return u, nil
}
