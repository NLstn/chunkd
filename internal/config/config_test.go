package config

import "testing"

func TestSafeDefaultsAndDevRestrictions(t *testing.T) {
	t.Setenv("CHUNKD_LISTEN", "")
	t.Setenv("CHUNKD_DATA_DIR", "")
	t.Setenv("CHUNKD_ORIGIN", "")
	t.Setenv("CHUNKD_DEV", "")
	c, err := Load()
	if err != nil || !c.SecureCookies || c.Listen != "127.0.0.1:8080" {
		t.Fatalf("config=%+v err=%v", c, err)
	}
	t.Setenv("CHUNKD_DEV", "true")
	t.Setenv("CHUNKD_ORIGIN", "http://127.0.0.1:5173")
	c, err = Load()
	if err != nil || c.SecureCookies {
		t.Fatalf("config=%+v err=%v", c, err)
	}
	t.Setenv("CHUNKD_LISTEN", "0.0.0.0:8080")
	if _, err := Load(); err == nil {
		t.Fatal("insecure development mode accepted public listen address")
	}
	t.Setenv("CHUNKD_LISTEN", "127.0.0.1:8080")
	t.Setenv("CHUNKD_DEV", "false")
	if _, err := Load(); err == nil {
		t.Fatal("production accepted HTTP origin")
	}
}
