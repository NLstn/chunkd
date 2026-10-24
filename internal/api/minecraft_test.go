package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NLstn/chunkd/internal/domain"
	"github.com/NLstn/chunkd/internal/installer"
	"github.com/NLstn/chunkd/internal/testutil"
)

type apiDownloads struct{}

func (apiDownloads) Versions(context.Context) ([]installer.Version, error) {
	return []installer.Version{{ID: "1.21.11"}}, nil
}
func (apiDownloads) Resolve(context.Context, string, string) (installer.Artifact, error) {
	h := sha256.Sum256([]byte("fixture"))
	return installer.Artifact{Hash: hex.EncodeToString(h[:]), Algorithm: "sha256", RequiredJava: 25, Size: 7}, nil
}
func (apiDownloads) Download(_ context.Context, _ installer.Artifact, dir string) error {
	return os.WriteFile(filepath.Join(dir, "server.jar"), []byte("fixture"), 0600)
}

func TestAuthenticatedProvisioningAndBoundLifecycleActions(t *testing.T) {
	java := testutil.FakeJava(t)
	t.Setenv("CHUNKD_JAVA_PATHS", java)
	a := testApp(t)
	a.minecraft.Downloads = apiDownloads{}
	cookie := login(t, a)
	body := fmt.Sprintf(`{"Name":"API world","Software":"vanilla","Version":"1.21.11","JavaPath":%q,"MemoryMB":512,"Port":25565,"EULAAccepted":true}`, java)
	if w := request(a, "POST", "/odata/CreateServer", body, nil); w.Code != 401 {
		t.Fatalf("anonymous provision permitted: %d", w.Code)
	}
	for _, path := range []string{"/odata/GetJavaRuntimes()", "/odata/GetMinecraftVersions()"} {
		w := request(a, "GET", path, "", cookie)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	w := request(a, "POST", "/odata/CreateServer", body, cookie)
	if w.Code != 202 || w.Header().Get("Location") == "" {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var job domain.Job
	if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	waitJob := func(id uint) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			w := request(a, "GET", fmt.Sprintf("/odata/Jobs(%d)", id), "", cookie)
			var current domain.Job
			if err := json.Unmarshal(w.Body.Bytes(), &current); err != nil {
				t.Fatal(err)
			}
			if current.State == "Failed" {
				t.Fatalf("job failed: %s", current.Message)
			}
			if current.State == "Succeeded" {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("job timeout")
	}
	waitJob(job.ID)
	for _, action := range []string{"Start", "Stop"} {
		path := fmt.Sprintf("/odata/Servers(%d)/Chunkd.%s", job.ServerID, action)
		if w := request(a, "POST", path, "{}", nil); w.Code != 401 {
			t.Fatal("anonymous lifecycle permitted")
		}
		w := request(a, "POST", path, "{}", cookie)
		if w.Code != 202 {
			t.Fatalf("%s: %d %s", action, w.Code, w.Body.String())
		}
		var operation domain.Job
		if err := json.Unmarshal(w.Body.Bytes(), &operation); err != nil {
			t.Fatal(err)
		}
		waitJob(operation.ID)
	}
}
