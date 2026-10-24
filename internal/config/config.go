package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
)

type Config struct {
	Listen        string
	DataDir       string
	Origin        string
	SecureCookies bool
}

func Load() (Config, error) {
	c := Config{Listen: env("CHUNKD_LISTEN", "127.0.0.1:8080"), DataDir: env("CHUNKD_DATA_DIR", "./data"), Origin: env("CHUNKD_ORIGIN", "https://localhost"), SecureCookies: true}
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return c, fmt.Errorf("CHUNKD_LISTEN: %w", err)
	}
	u, err := url.Parse(c.Origin)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return c, fmt.Errorf("CHUNKD_ORIGIN must be an http(s) origin without a path")
	}
	switch env("CHUNKD_DEV", "false") {
	case "true":
		if u.Scheme != "http" || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1") {
			return c, fmt.Errorf("development mode requires a loopback HTTP origin")
		}
		host, _, _ := net.SplitHostPort(c.Listen)
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			return c, fmt.Errorf("development mode requires a loopback listen address")
		}
		c.SecureCookies = false
	case "false":
		if u.Scheme != "https" {
			return c, fmt.Errorf("production requires an HTTPS origin")
		}
	default:
		return c, fmt.Errorf("CHUNKD_DEV must be true or false")
	}
	return c, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
