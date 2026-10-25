package minecraft

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/NLstn/chunkd/internal/domain"
	"github.com/NLstn/chunkd/internal/supervisor"
	"gorm.io/gorm"
)

type Settings struct {
	MOTD         string
	MaxPlayers   int64
	Difficulty   string
	GameMode     string
	ViewDistance int64
	PVP          bool
	WhiteList    bool
}

func (m *Manager) server(id uint) (domain.Server, error) {
	var s domain.Server
	err := m.db.First(&s, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = ErrNotFound
	}
	return s, err
}

func (m *Manager) Details(id uint, after int64) (supervisor.Details, error) {
	if _, err := m.server(id); err != nil {
		return supervisor.Details{}, err
	}
	return m.Processes.Details(id, after), nil
}

func (m *Manager) Command(id uint, command string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.server(id)
	if err != nil {
		return err
	}
	if m.busy[id] || s.State != "Running" {
		return ErrConflict
	}
	if err := m.Processes.Command(id, command); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return nil
}

func (m *Manager) properties(id uint) (*os.Root, []byte, error) {
	// OpenRoot confines resolution even if a server creates malicious symlinks.
	base, err := os.OpenRoot(m.root)
	if err != nil {
		return nil, nil, err
	}
	defer base.Close()
	name := strconv.FormatUint(uint64(id), 10)
	info, err := base.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("unsafe server directory")
	}
	root, err := base.OpenRoot(name)
	if err != nil {
		return nil, nil, err
	}
	info, err = root.Lstat("server.properties")
	if err != nil || !info.Mode().IsRegular() {
		root.Close()
		return nil, nil, fmt.Errorf("server.properties must be a regular file")
	}
	f, err := root.Open("server.properties")
	if err != nil {
		root.Close()
		return nil, nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 1<<20+1))
	if err == nil && len(b) > 1<<20 {
		err = fmt.Errorf("properties file is too large")
	}
	if err != nil {
		root.Close()
		return nil, nil, err
	}
	return root, b, nil
}

func parseSettings(b []byte) Settings {
	values := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			values[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	s := Settings{MOTD: "A Minecraft Server", MaxPlayers: 20, Difficulty: "easy", GameMode: "survival", ViewDistance: 10, PVP: true}
	if v, ok := values["motd"]; ok {
		s.MOTD = v
	}
	if v, err := strconv.ParseInt(values["max-players"], 10, 64); err == nil {
		s.MaxPlayers = v
	}
	if v := values["difficulty"]; v != "" {
		s.Difficulty = v
	}
	if v := values["gamemode"]; v != "" {
		s.GameMode = v
	}
	if v, err := strconv.ParseInt(values["view-distance"], 10, 64); err == nil {
		s.ViewDistance = v
	}
	if v, err := strconv.ParseBool(values["pvp"]); err == nil {
		s.PVP = v
	}
	if v, err := strconv.ParseBool(values["white-list"]); err == nil {
		s.WhiteList = v
	}
	return s
}

func (m *Manager) Settings(id uint) (Settings, error) {
	if _, err := m.server(id); err != nil {
		return Settings{}, err
	}
	root, b, err := m.properties(id)
	if err != nil {
		return Settings{}, err
	}
	defer root.Close()
	return parseSettings(b), nil
}

func (m *Manager) SaveSettings(id uint, s Settings) error {
	if len(s.MOTD) > 200 || strings.ContainsAny(s.MOTD, "\r\n\x00\\") || s.MaxPlayers < 1 || s.MaxPlayers > 1000 || s.ViewDistance < 2 || s.ViewDistance > 32 || !oneOf(s.Difficulty, "peaceful", "easy", "normal", "hard") || !oneOf(s.GameMode, "survival", "creative", "adventure", "spectator") {
		return fmt.Errorf("%w: check message, players (1–1000), view distance (2–32), difficulty and game mode", ErrInvalid)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	server, err := m.server(id)
	if err != nil {
		return err
	}
	if m.closed || m.busy[id] || (server.State != "Stopped" && server.State != "Crashed") {
		return fmt.Errorf("%w: stop the server before saving settings", ErrConflict)
	}
	root, b, err := m.properties(id)
	if err != nil {
		return err
	}
	// Refuse line continuations rather than accidentally changing an unrelated
	// property's meaning when removing managed entries or appending overrides.
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSuffix(line, "\r")
		trailing := len(line) - len(strings.TrimRight(line, "\\"))
		if trailing%2 != 0 {
			root.Close()
			return fmt.Errorf("%w: properties with line continuations must be edited on the host", ErrInvalid)
		}
	}
	defer root.Close()
	values := map[string]string{"motd": s.MOTD, "max-players": strconv.FormatInt(s.MaxPlayers, 10), "difficulty": s.Difficulty, "gamemode": s.GameMode, "view-distance": strconv.FormatInt(s.ViewDistance, 10), "pvp": strconv.FormatBool(s.PVP), "white-list": strconv.FormatBool(s.WhiteList)}
	// Write managed values after existing properties. Leave all other
	// settings (including Chunkd's assigned port and authentication) untouched.
	var out strings.Builder
	// Remove old managed entries to keep repeated saves bounded.
	for _, line := range strings.Split(string(b), "\n") {
		key, _, _ := strings.Cut(strings.TrimSpace(line), "=")
		if _, managed := values[strings.TrimSpace(key)]; !managed && line != "# Managed by Chunkd; applies on next start" {
			if strings.TrimSpace(line) == "" {
				continue
			}
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	out.WriteString("# Managed by Chunkd; applies on next start\n")
	for _, k := range []string{"motd", "max-players", "difficulty", "gamemode", "view-distance", "pvp", "white-list"} {
		fmt.Fprintf(&out, "%s=%s\n", k, values[k])
	}
	// Exclusive creation prevents following an existing temporary-file symlink.
	f, err := root.OpenFile(".chunkd-properties.tmp", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(".chunkd-properties.tmp")
	_, err = io.WriteString(f, out.String())
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return root.Rename(".chunkd-properties.tmp", "server.properties")
}

func oneOf(value string, values ...string) bool {
	for _, v := range values {
		if value == v {
			return true
		}
	}
	return false
}
