package installer

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fixtures(t *testing.T) (*Installer, []byte) {
	t.Helper()
	jar := []byte("verified test fixture, not Minecraft")
	sha := sha1.Sum(jar)
	sha256sum := sha256.Sum256(jar)
	detail := fmt.Sprintf(`{"javaVersion":{"majorVersion":21},"downloads":{"server":{"url":"https://fixtures.invalid/server.jar","sha1":%q,"size":%d}}}`, hex.EncodeToString(sha[:]), len(jar))
	detailHash := sha1.Sum([]byte(detail))
	manifest := fmt.Sprintf(`{"versions":[{"id":"1.21.11","type":"release","url":"https://fixtures.invalid/version.json","sha1":%q},{"id":"1.19","type":"release"},{"id":"snapshot","type":"snapshot"}]}`, hex.EncodeToString(detailHash[:]))
	builds := fmt.Sprintf(`[{"id":999,"channel":"EXPERIMENTAL","downloads":{}},{"id":10,"channel":"STABLE","downloads":{"server:default":{"url":"https://fixtures.invalid/server.jar","size":%d,"checksums":{"sha256":%q}}}}]`, len(jar), hex.EncodeToString(sha256sum[:]))
	i := New()
	i.manifestURL = "https://fixtures.invalid/manifest.json"
	i.paperURL = "https://fixtures.invalid/paper/"
	i.trusted = func(u string) bool { return strings.HasPrefix(u, "https://fixtures.invalid/") }
	i.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("User-Agent") != userAgent {
			t.Error("missing identified User-Agent")
		}
		body := map[string]string{"/manifest.json": manifest, "/version.json": detail, "/server.jar": string(jar), "/paper/1.21.11/builds": builds}[r.URL.Path]
		if body == "" {
			return nil, fmt.Errorf("unexpected fixture URL %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	return i, jar
}

func TestOfficialMetadataContractsAndVerifiedDownloads(t *testing.T) {
	i, jar := fixtures(t)
	versions, err := i.Versions(context.Background())
	if err != nil || len(versions) != 1 || versions[0].ID != "1.21.11" {
		t.Fatalf("versions=%v err=%v", versions, err)
	}
	for _, software := range []string{"vanilla", "paper"} {
		t.Run(software, func(t *testing.T) {
			a, err := i.Resolve(context.Background(), software, "1.21.11")
			if err != nil {
				t.Fatal(err)
			}
			if a.RequiredJava != 21 {
				t.Fatal("wrong Java requirement")
			}
			if software == "paper" && (a.Build != 10 || a.Algorithm != "sha256") {
				t.Fatal("did not select stable build")
			}
			dir := t.TempDir()
			if err := i.Download(context.Background(), a, dir); err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(filepath.Join(dir, "server.jar"))
			if string(b) != string(jar) {
				t.Fatal("wrong artifact")
			}
			if err := VerifyFile(filepath.Join(dir, "server.jar"), a.Algorithm, a.Hash); err != nil {
				t.Fatal(err)
			}
			bad := a
			bad.Hash = strings.Repeat("0", len(a.Hash))
			badDir := t.TempDir()
			if err := i.Download(context.Background(), bad, badDir); err == nil {
				t.Fatal("accepted checksum mismatch")
			}
			files, _ := os.ReadDir(badDir)
			if len(files) != 0 {
				t.Fatal("failed download left partial files")
			}
		})
	}
	_, err = i.Resolve(context.Background(), "vanilla", "1.19")
	if err == nil {
		t.Fatal("unsupported version accepted")
	}
	_, err = i.Resolve(context.Background(), "modded", "1.21.11")
	if err == nil {
		t.Fatal("unsupported software accepted")
	}
}

func TestMetadataTamperingAndTrustBoundary(t *testing.T) {
	i, _ := fixtures(t)
	original := i.client.Transport
	i.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		res, err := original.RoundTrip(r)
		if err == nil && r.URL.Path == "/version.json" {
			res.Body = io.NopCloser(strings.NewReader(`{"javaVersion":{"majorVersion":1}}`))
		}
		return res, err
	})
	if _, err := i.Resolve(context.Background(), "vanilla", "1.21.11"); err == nil {
		t.Fatal("accepted tampered version descriptor")
	}
	for _, u := range []string{"http://piston-data.mojang.com/a", "https://127.0.0.1/a", "https://piston-data.mojang.com.evil.example/a", "https://user@fill.papermc.io/a", "https://fill.papermc.io:8080/a"} {
		if trustedURL(u) {
			t.Fatalf("trusted %s", u)
		}
	}
	if !trustedURL("https://fill-data.papermc.io/v1/objects/hash/server.jar") {
		t.Fatal("official download rejected")
	}
	if java, err := MinimumJava("26.1"); err != nil || java != 25 {
		t.Fatal("new release Java floor incorrect")
	}
}
