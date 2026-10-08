package state

// EngineSettings are application overrides; optional values inherit the profile.
type EngineSettings struct {
	AllowLAN     bool                   `json:"allow_lan"`
	IPv6         *bool                  `json:"ipv6,omitempty"`
	UnifiedDelay *bool                  `json:"unified_delay,omitempty"`
	LogLevel     string                 `json:"log_level,omitempty"`
	Tun          *TunSettings           `json:"tun,omitempty"`
	DNS          map[string]DNSSettings `json:"dns,omitempty"`
}

type TunSettings struct {
	Stack        string   `json:"stack" yaml:"stack"`
	Device       string   `json:"device" yaml:"device,omitempty"`
	AutoRoute    bool     `json:"auto-route" yaml:"auto-route"`
	AutoDetect   bool     `json:"auto-detect-interface" yaml:"auto-detect-interface"`
	StrictRoute  bool     `json:"strict-route" yaml:"strict-route"`
	MTU          int      `json:"mtu" yaml:"mtu"`
	DNSHijack    []string `json:"dns-hijack" yaml:"dns-hijack"`
	RouteExclude []string `json:"route-exclude-address" yaml:"route-exclude-address"`
}

func DefaultTun() TunSettings {
	return TunSettings{Stack: "system", AutoRoute: true, AutoDetect: true, MTU: 1500, DNSHijack: []string{"any:53"}}
}

type DNSSettings struct {
	Enabled            bool     `json:"enabled"`
	Mode               string   `json:"mode"`
	IPv6               bool     `json:"ipv6"`
	Nameservers        []string `json:"nameservers"`
	DefaultNameservers []string `json:"default_nameservers"`
	Fallback           []string `json:"fallback"`
	FakeIPRange        string   `json:"fake_ip_range"`
}

func DefaultDNS() DNSSettings {
	return DNSSettings{Mode: "fake-ip", Nameservers: []string{"https://1.1.1.1/dns-query", "https://dns.alidns.com/dns-query"}, DefaultNameservers: []string{"1.1.1.1", "223.5.5.5"}, FakeIPRange: "198.18.0.1/16"}
}
