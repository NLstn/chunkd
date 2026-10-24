package installer

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const manifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"
const paperURL = "https://fill.papermc.io/v3/projects/paper/versions/"
const userAgent = "chunkd/0.1.0 (https://github.com/NLstn/chunkd)"
const maxArtifact = 512 << 20

type Version struct {
	ID string `json:"ID"`
}
type Artifact struct {
	URL          string
	Hash         string
	Algorithm    string
	Size         int64
	Build        int64
	RequiredJava int64
}
type manifest struct {
	Versions []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		URL  string `json:"url"`
		SHA1 string `json:"sha1"`
	} `json:"versions"`
}
type versionDetail struct {
	JavaVersion struct {
		Major int64 `json:"majorVersion"`
	} `json:"javaVersion"`
	Downloads struct {
		Server struct {
			URL  string `json:"url"`
			SHA1 string `json:"sha1"`
			Size int64  `json:"size"`
		} `json:"server"`
	} `json:"downloads"`
}
type paperBuild struct {
	ID        int64  `json:"id"`
	Channel   string `json:"channel"`
	Downloads map[string]struct {
		URL       string `json:"url"`
		Size      int64  `json:"size"`
		Checksums struct {
			SHA256 string `json:"sha256"`
		} `json:"checksums"`
	} `json:"downloads"`
}

type Installer struct {
	client      *http.Client
	manifestURL string
	paperURL    string
	trusted     func(string) bool
}

func New() *Installer {
	i := &Installer{manifestURL: manifestURL, paperURL: paperURL, trusted: trustedURL}
	i.client = &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !trustedURL(r.URL.String()) {
			return fmt.Errorf("untrusted download redirect")
		}
		return nil
	}}
	return i
}

var releasePattern = regexp.MustCompile(`^(\d+)\.(\d+)(?:\.(\d+))?$`)

// M2 deliberately supports modern release servers only, not snapshots or old
// Java launchers. Exact runtime requirements come from Mojang's version detail.
func MinimumJava(version string) (int64, error) {
	m := releasePattern.FindStringSubmatch(version)
	if m == nil {
		return 0, fmt.Errorf("select a release version (1.20.5 or newer)")
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	if major >= 26 {
		return 25, nil
	}
	if major == 1 && (minor > 20 || (minor == 20 && patch >= 5)) {
		return 21, nil
	}
	return 0, fmt.Errorf("M2 supports Minecraft 1.20.5 or newer release versions")
}

func (i *Installer) Versions(ctx context.Context) ([]Version, error) {
	var m manifest
	if err := i.getJSON(ctx, i.manifestURL, &m); err != nil {
		return nil, err
	}
	versions := []Version{}
	for _, v := range m.Versions {
		if _, err := MinimumJava(v.ID); err == nil && v.Type == "release" {
			versions = append(versions, Version{ID: v.ID})
			if len(versions) == 50 {
				break
			}
		}
	}
	return versions, nil
}

func (i *Installer) Resolve(ctx context.Context, software, version string) (Artifact, error) {
	minimum, err := MinimumJava(version)
	if err != nil {
		return Artifact{}, err
	}
	if software != "vanilla" && software != "paper" {
		return Artifact{}, fmt.Errorf("software must be vanilla or paper")
	}
	var m manifest
	if err := i.getJSON(ctx, i.manifestURL, &m); err != nil {
		return Artifact{}, err
	}
	var detail versionDetail
	found := false
	for _, v := range m.Versions {
		if v.ID == version && v.Type == "release" {
			b, err := i.bytes(ctx, v.URL, 4<<20)
			if err != nil {
				return Artifact{}, err
			}
			if err := verifyBytes(b, "sha1", v.SHA1); err != nil {
				return Artifact{}, fmt.Errorf("version metadata: %w", err)
			}
			if err := json.Unmarshal(b, &detail); err != nil {
				return Artifact{}, err
			}
			found = true
			break
		}
	}
	if !found {
		return Artifact{}, fmt.Errorf("Minecraft release not found")
	}
	required := max(minimum, detail.JavaVersion.Major)
	if software == "vanilla" {
		s := detail.Downloads.Server
		a := Artifact{URL: s.URL, Hash: s.SHA1, Algorithm: "sha1", Size: s.Size, RequiredJava: required}
		return a, i.validate(a)
	}
	var builds []paperBuild
	if err := i.getJSON(ctx, i.paperURL+url.PathEscape(version)+"/builds", &builds); err != nil {
		return Artifact{}, err
	}
	var selected Artifact
	for _, b := range builds {
		if b.Channel != "STABLE" || b.ID <= selected.Build {
			continue
		}
		d, ok := b.Downloads["server:default"]
		if !ok {
			continue
		}
		selected = Artifact{URL: d.URL, Hash: d.Checksums.SHA256, Algorithm: "sha256", Size: d.Size, Build: b.ID, RequiredJava: required}
	}
	if selected.Build == 0 {
		return Artifact{}, fmt.Errorf("no stable Paper build is available for %s", version)
	}
	return selected, i.validate(selected)
}

func (i *Installer) validate(a Artifact) error {
	if !i.trusted(a.URL) {
		return fmt.Errorf("untrusted artifact URL")
	}
	h, err := newHash(a.Algorithm)
	if err != nil {
		return err
	}
	b, err := hex.DecodeString(a.Hash)
	if err != nil || len(b) != h.Size() {
		return fmt.Errorf("missing or invalid artifact checksum")
	}
	if a.Size < 1 || a.Size > maxArtifact {
		return fmt.Errorf("invalid artifact size")
	}
	return nil
}

func (i *Installer) Download(ctx context.Context, a Artifact, dir string) error {
	if err := i.validate(a); err != nil {
		return err
	}
	r, err := i.get(ctx, a.URL)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	f, err := os.CreateTemp(dir, ".server-*.jar")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	h, _ := newHash(a.Algorithm)
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(r.Body, a.Size+1))
	if copyErr == nil {
		copyErr = f.Sync()
	}
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n != a.Size || hex.EncodeToString(h.Sum(nil)) != strings.ToLower(a.Hash) {
		return fmt.Errorf("artifact size or checksum mismatch")
	}
	return os.Rename(name, filepath.Join(dir, "server.jar"))
}

func VerifyFile(path, algorithm, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h, err := newHash(algorithm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(h, io.LimitReader(f, maxArtifact+1)); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != strings.ToLower(expected) {
		return fmt.Errorf("installed server artifact checksum mismatch")
	}
	return nil
}

func newHash(algorithm string) (hash.Hash, error) {
	switch algorithm {
	case "sha1":
		return sha1.New(), nil
	case "sha256":
		return sha256.New(), nil
	}
	return nil, fmt.Errorf("unsupported checksum algorithm")
}
func verifyBytes(b []byte, algorithm, expected string) error {
	h, err := newHash(algorithm)
	if err != nil {
		return err
	}
	_, _ = h.Write(b)
	if expected == "" || hex.EncodeToString(h.Sum(nil)) != strings.ToLower(expected) {
		return fmt.Errorf("checksum mismatch")
	}
	return nil
}

func (i *Installer) get(ctx context.Context, raw string) (*http.Response, error) {
	if !i.trusted(raw) {
		return nil, fmt.Errorf("untrusted upstream URL")
	}
	r, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return nil, err
	}
	r.Header.Set("User-Agent", userAgent)
	response, err := i.client.Do(r)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != 200 {
		response.Body.Close()
		return nil, fmt.Errorf("upstream returned HTTP %d", response.StatusCode)
	}
	return response, nil
}
func (i *Installer) bytes(ctx context.Context, raw string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	r, err := i.get(ctx, raw)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	b, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("upstream metadata exceeds size limit")
	}
	return b, err
}
func (i *Installer) getJSON(ctx context.Context, raw string, target any) error {
	b, err := i.bytes(ctx, raw, 4<<20)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, target)
}
func trustedURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	switch u.Hostname() {
	case "piston-meta.mojang.com", "piston-data.mojang.com", "launchermeta.mojang.com", "launcher.mojang.com", "fill.papermc.io", "fill-data.papermc.io":
		return true
	}
	return false
}
