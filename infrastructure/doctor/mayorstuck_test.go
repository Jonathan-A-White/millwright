package doctor

import "testing"

func TestProxyAddressReadsTheHostAndPortAProxySettingPointsAt(t *testing.T) {
	cases := []struct {
		value string
		want  string
		ok    bool
	}{
		{"http://127.0.0.1:8080", "127.0.0.1:8080", true},
		{"127.0.0.1:8080", "127.0.0.1:8080", true},
		{"http://user:pw@proxy.example:3128/", "proxy.example:3128", true},
		{"https://proxy.example", "proxy.example:443", true},
		{"http://proxy.example", "proxy.example:80", true},
		{"socks5://localhost", "localhost:1080", true},
		{"http://[::1]:8080", "[::1]:8080", true},
		{"", "", false},
		{"http://", "", false},
		{"gopher://proxy.example", "", false},
		{"http://proxy.example:port", "", false},
	}
	for _, c := range cases {
		got, ok := proxyAddress(c.value)
		if got != c.want || ok != c.ok {
			t.Errorf("proxyAddress(%q) = %q, %v; want %q, %v", c.value, got, ok, c.want, c.ok)
		}
	}
}

func TestOnlyTheSixProxySettingsAreProxyNames(t *testing.T) {
	for _, name := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		if !isProxyName(name) {
			t.Errorf("%s should be a proxy name", name)
		}
	}
	for _, name := range []string{"NO_PROXY", "no_proxy", "PATH", ""} {
		if isProxyName(name) {
			t.Errorf("%s should not be a proxy name", name)
		}
	}
}
