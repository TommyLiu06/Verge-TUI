package profile

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"verge-tui/internal/state"
	"verge-tui/internal/testutil"
)

func TestDNSListenWithManagedCore(t *testing.T) {
	binary := os.Getenv("MIHOMO_TEST_BINARY")
	if binary == "" {
		t.Skip("set MIHOMO_TEST_BINARY to an application-managed core")
	}
	for _, tc := range []struct {
		name, mode string
		overwrite  bool
		listen     bool
	}{
		{"system-inherits-subscription", "system", false, true},
		{"system-overwrites-listen", "system", true, true},
		{"system-empty-listen-inherits", "system", true, false},
		{"user-removes-all-listeners", "user", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ports := map[int]bool{}
			freePort := func() int {
				t.Helper()
				for {
					port, err := state.FreePort(0)
					if err != nil {
						t.Fatal(err)
					}
					if !ports[port] {
						ports[port] = true
						return port
					}
				}
			}
			profileAddress := net.JoinHostPort("127.0.0.1", strconv.Itoa(freePort()))
			overrideAddress := net.JoinHostPort("127.0.0.1", strconv.Itoa(freePort()))
			dns := state.DefaultDNS()
			dns.Enabled, dns.Mode = tc.overwrite, "redir-host"
			dns.Nameservers, dns.DefaultNameservers = []string{"127.0.0.1:9"}, []string{"127.0.0.1"}
			dns.Listen = ""
			if tc.listen {
				dns.Listen = overrideAddress
			}
			settings := state.Settings{InstallMode: tc.mode, Current: "active", Mode: "rule", MixedPort: freePort(), ProxyUser: "dns-test", ProxyPassword: "private", Engine: state.EngineSettings{DNS: map[string]state.DNSSettings{"active": dns}}}
			source := DefaultConfig + "hosts: {verge-dns.test: 127.0.0.1}\ndns:\n  enable: true\n  use-hosts: true\n  enhanced-mode: redir-host\n  nameserver: [127.0.0.1:9]\n  listen: " + profileAddress + "\n"
			payload, err := RuntimeData(settings, []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			root := testutil.Dir(t)
			config := filepath.Join(root, "config.yaml")
			if err := os.WriteFile(config, payload, 0600); err != nil {
				t.Fatal(err)
			}
			log, err := os.Create(filepath.Join(root, "core.log"))
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(binary, "-d", root, "-f", config)
			cmd.Stdout, cmd.Stderr = log, log
			if err := cmd.Start(); err != nil {
				log.Close()
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() { cmd.Wait(); close(done) }()
			t.Cleanup(func() { cmd.Process.Kill(); <-done; log.Close() })
			wait := func(check func() error) {
				t.Helper()
				deadline := time.Now().Add(5 * time.Second)
				for {
					err := check()
					if err == nil {
						return
					}
					select {
					case <-done:
						output, _ := os.ReadFile(log.Name())
						t.Fatalf("core exited: %v\n%s", err, output)
					default:
					}
					if time.Now().After(deadline) {
						t.Fatal(err)
					}
					time.Sleep(20 * time.Millisecond)
				}
			}
			if tc.mode == "system" {
				address := profileAddress
				if tc.overwrite && tc.listen {
					address = overrideAddress
				}
				for _, network := range []string{"udp", "tcp"} {
					wait(func() error { return queryStaticDNS(network, address) })
				}
				return
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
			defer server.Close()
			proxyURL := &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(settings.MixedPort)), User: url.UserPassword(settings.ProxyUser, settings.ProxyPassword)}
			transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: time.Second}
			target := strings.Replace(server.URL, "127.0.0.1", "verge-dns.test", 1)
			wait(func() error {
				response, err := client.Get(target)
				if err != nil {
					return err
				}
				response.Body.Close()
				if response.StatusCode != http.StatusNoContent {
					return fmt.Errorf("authenticated proxy returned HTTP %d", response.StatusCode)
				}
				return nil
			})
			for _, address := range []string{profileAddress, overrideAddress} {
				tcp, err := net.Listen("tcp", address)
				if err != nil {
					t.Fatalf("user mode retained TCP DNS listener %s: %v", address, err)
				}
				defer tcp.Close()
				udp, err := net.ListenPacket("udp", address)
				if err != nil {
					t.Fatalf("user mode retained UDP DNS listener %s: %v", address, err)
				}
				defer udp.Close()
			}
		})
	}
}

func queryStaticDNS(network, address string) error {
	query := []byte{0x6d, 0x74, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0}
	for _, label := range []string{"verge-dns", "test"} {
		query = append(query, byte(len(label)))
		query = append(query, label...)
	}
	query = append(query, 0, 0, 1, 0, 1)
	conn, err := net.DialTimeout(network, address, time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Second))
	if network == "tcp" {
		query = append([]byte{0, byte(len(query))}, query...)
	}
	if _, err := conn.Write(query); err != nil {
		return err
	}
	response := make([]byte, 512)
	if network == "tcp" {
		length := make([]byte, 2)
		if _, err := io.ReadFull(conn, length); err != nil {
			return err
		}
		response = make([]byte, binary.BigEndian.Uint16(length))
		_, err = io.ReadFull(conn, response)
	} else {
		var count int
		count, err = conn.Read(response)
		response = response[:count]
	}
	if err != nil {
		return err
	}
	if len(response) < 16 || binary.BigEndian.Uint16(response[:2]) != 0x6d74 || response[2]&0x80 == 0 || response[3]&15 != 0 || binary.BigEndian.Uint16(response[6:8]) != 1 || string(response[len(response)-4:]) != "\x7f\x00\x00\x01" {
		return fmt.Errorf("unexpected %s DNS answer: %x", network, response)
	}
	return nil
}
