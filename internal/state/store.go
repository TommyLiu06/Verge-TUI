package state

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type Usage struct {
	Upload   uint64 `json:"upload"`
	Download uint64 `json:"download"`
	Total    uint64 `json:"total"`
	Expire   int64  `json:"expire"`
}

type Profile struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Description string    `json:"description,omitempty"`
	URL         string    `json:"url,omitempty"`
	Home        string    `json:"home,omitempty"`
	Updated     time.Time `json:"updated"`
	Usage       *Usage    `json:"usage,omitempty"`
	UserAgent   string    `json:"user_agent,omitempty"`
	Timeout     int       `json:"timeout_seconds,omitempty"`
}

type Settings struct {
	Schema        int               `json:"schema"`
	InstallMode   string            `json:"install_mode"`
	Mode          string            `json:"mode"`
	MixedPort     int               `json:"mixed_port"`
	APIPort       int               `json:"api_port"`
	Secret        string            `json:"secret"`
	ProxyUser     string            `json:"proxy_user"`
	ProxyPassword string            `json:"proxy_password"`
	Tun           bool              `json:"tun"`
	Core          string            `json:"core"`
	Current       string            `json:"current_profile"`
	HomeGroup     map[string]string `json:"home_groups"`
	Profiles      []Profile         `json:"profiles"`
}

type Store struct{ Root string }

func Open(root string) (*Store, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	s := &Store{Root: abs}
	rootMode := os.FileMode(0700)
	if c, err := s.Read(); err == nil && c.InstallMode == "system" {
		rootMode = 0711
	}
	for _, dir := range []string{"", "profiles", "cores", "runtime"} {
		path := filepath.Join(abs, dir)
		mode := os.FileMode(0700)
		if dir == "" {
			mode = rootMode
		}
		if info, err := os.Lstat(path); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
			return nil, fmt.Errorf("data directory must be a real directory: %s", path)
		}
		if err := os.MkdirAll(path, mode); err != nil {
			return nil, err
		}
		if err := os.Chmod(path, mode); err != nil {
			return nil, err
		}
	}
	err = s.Update(func(c *Settings) error {
		if c.Schema != 0 {
			return nil
		}
		mixed, err := FreePort(0)
		if err != nil {
			return err
		}
		api, err := FreePort(mixed)
		if err != nil {
			return err
		}
		secret, err := Token()
		if err != nil {
			return err
		}
		password, err := Token()
		if err != nil {
			return err
		}
		*c = Settings{Schema: 1, InstallMode: "user", Mode: "rule", MixedPort: mixed, APIPort: api,
			Secret: secret, ProxyUser: "mihomo-tui", ProxyPassword: password, HomeGroup: map[string]string{}}
		return nil
	})
	return s, err
}

func (s *Store) Read() (Settings, error) {
	var c Settings
	b, err := os.ReadFile(filepath.Join(s.Root, "settings.json"))
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("read settings: %w", err)
	}
	if c.Schema != 1 {
		return c, fmt.Errorf("unsupported settings schema %d", c.Schema)
	}
	return c, nil
}

func (s *Store) Update(fn func(*Settings) error) error {
	f, err := os.OpenFile(filepath.Join(s.Root, "settings.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		return err
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN)
	c, err := s.Read()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := fn(&c); err != nil {
		return err
	}
	if c.HomeGroup == nil {
		c.HomeGroup = map[string]string{}
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return Write(filepath.Join(s.Root, "settings.json"), append(b, '\n'), 0600)
}

func Write(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func Token() (string, error) {
	b := make([]byte, 24)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
}

func ValidID(id string) bool {
	if id == "" || len(id) > 100 || id == "." || id == ".." {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r)) {
			return false
		}
	}
	return true
}

func (s *Store) ProfilePath(id string) (string, error) {
	if !ValidID(id) {
		return "", errors.New("invalid profile identifier")
	}
	return filepath.Join(s.Root, "profiles", id+".yaml"), nil
}

func Find(c Settings, id string) (Profile, bool) {
	for _, p := range c.Profiles {
		if p.ID == id {
			return p, true
		}
	}
	return Profile{}, false
}

func CheckPort(port int) error {
	if port < 1024 || port > 65535 {
		return errors.New("port must be between 1024 and 65535")
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	tcp, err := net.Listen("tcp4", addr)
	if err != nil {
		return fmt.Errorf("TCP port %d unavailable: %w", port, err)
	}
	defer tcp.Close()
	udp, err := net.ListenPacket("udp4", addr)
	if err != nil {
		return fmt.Errorf("UDP port %d unavailable: %w", port, err)
	}
	return udp.Close()
}

func FreePort(exclude int) (int, error) {
	for range 128 {
		var b [2]byte
		if _, err := rand.Read(b[:]); err != nil {
			return 0, err
		}
		port := 20000 + (int(b[0])<<8|int(b[1]))%40000
		if port == exclude || port == 20170 || port == 20171 {
			continue
		}
		if CheckPort(port) == nil {
			return port, nil
		}
	}
	return 0, errors.New("no free high port found; specify ports after resolving conflicts")
}
