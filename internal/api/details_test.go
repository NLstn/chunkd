package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NLstn/chunkd/internal/domain"
	"github.com/NLstn/chunkd/internal/minecraft"
	"github.com/NLstn/chunkd/internal/supervisor"
)

func TestServerDetailsAndSettings(t *testing.T) {
	a := testApp(t)
	cookie := login(t, a)
	s := domain.Server{Name: "Details", State: "Stopped", Port: 25565, MemoryMB: 2048}
	if err := a.db.Create(&s).Error; err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(a.cfg.DataDir, "servers", "1")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	original := "server-port=25565\nonline-mode=true\nmotd=Original\nmax-players=20\n"
	file := filepath.Join(dir, "server.properties")
	if err := os.WriteFile(file, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/odata/GetServerDetails(ServerID=1,After=0)", "/odata/GetServerSettings(ServerID=1)"} {
		if w := request(a, "GET", path, "", nil); w.Code != 401 {
			t.Fatal("anonymous details allowed")
		}
		if w := request(a, "GET", path, "", cookie); w.Code != 200 || !json.Valid(w.Body.Bytes()) {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	w := request(a, "GET", "/odata/GetServerDetails(ServerID=1,After=0)", "", cookie)
	var details struct {
		Value supervisor.Details `json:"value"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &details); err != nil {
		t.Fatal(err)
	}
	if details.Value.MemoryMB != nil || details.Value.CPUPercent != nil {
		t.Fatal("stopped metrics must be null")
	}
	body := `{"MOTD":"Updated world","MaxPlayers":30,"Difficulty":"hard","GameMode":"survival","ViewDistance":12,"PVP":false,"WhiteList":true}`
	for _, action := range []string{"SaveSettings", "SendCommand"} {
		if w := request(a, "POST", "/odata/Servers(1)/Chunkd."+action, body, nil); w.Code != 401 {
			t.Fatalf("anonymous %s allowed", action)
		}
	}
	if w := request(a, "POST", "/odata/Servers(1)/Chunkd.SendCommand", `{"Command":"say hello"}`, cookie); w.Code != 409 {
		t.Fatalf("command allowed on stopped server: %d", w.Code)
	}
	for i := 0; i < 2; i++ {
		w = request(a, "POST", "/odata/Servers(1)/Chunkd.SaveSettings", body, cookie)
		if w.Code != 204 {
			t.Fatalf("save: %d %s", w.Code, w.Body.String())
		}
	}
	saved, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "server-port=25565\nonline-mode=true") || strings.Count(string(saved), "motd=") != 1 {
		t.Fatalf("properties corrupted: %s", saved)
	}
	w = request(a, "GET", "/odata/GetServerSettings(ServerID=1)", "", cookie)
	var settings struct {
		Value minecraft.Settings `json:"value"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}
	if settings.Value.MOTD != "Updated world" || settings.Value.MaxPlayers != 30 || !settings.Value.WhiteList {
		t.Fatalf("wrong persisted settings: %s", w.Body.String())
	}
	bad := strings.Replace(body, `"MaxPlayers":30`, `"MaxPlayers":0`, 1)
	if w := request(a, "POST", "/odata/Servers(1)/Chunkd.SaveSettings", bad, cookie); w.Code != 400 {
		t.Fatalf("invalid settings: %d %s", w.Code, w.Body.String())
	}
	a.db.Model(&s).Update("state", "Running")
	if w := request(a, "POST", "/odata/Servers(1)/Chunkd.SaveSettings", body, cookie); w.Code != 409 {
		t.Fatalf("running settings: %d %s", w.Code, w.Body.String())
	}
	if w := request(a, "GET", "/odata/GetServerDetails(ServerID=999,After=0)", "", cookie); w.Code != 404 || !json.Valid(w.Body.Bytes()) {
		t.Fatalf("missing server: %d %s", w.Code, w.Body.String())
	}
}

func TestSettingsRefuseUnsafeFiles(t *testing.T) {
	a := testApp(t)
	s := domain.Server{Name: "Settings", State: "Stopped"}
	if err := a.db.Create(&s).Error; err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(a.cfg.DataDir, "servers", "1")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.properties")
	if err := os.WriteFile(outside, []byte("motd=outside\n"), 0600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "server.properties")
	if err := os.Symlink(outside, file); err != nil {
		t.Skip(err)
	}
	if _, err := a.minecraft.Settings(s.ID); err == nil {
		t.Fatal("read symlinked settings")
	}
	settings := minecraft.Settings{MOTD: "new", MaxPlayers: 20, Difficulty: "easy", GameMode: "survival", ViewDistance: 10}
	if err := a.minecraft.SaveSettings(s.ID, settings); err == nil {
		t.Fatal("wrote symlinked settings")
	}
	b, _ := os.ReadFile(outside)
	if string(b) != "motd=outside\n" {
		t.Fatal("changed external file")
	}
	os.Remove(file)
	os.WriteFile(file, []byte("motd=first\\\ncontinued\n"), 0600)
	if err := a.minecraft.SaveSettings(s.ID, settings); err == nil {
		t.Fatal("silently rewrote line continuation")
	}
}
