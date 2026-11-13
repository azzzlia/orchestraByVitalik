package app

import (
	"fmt"
	"os"
	"strings"

	"claude-squad/session/tmux"
)

// proxiedDomains go through ClaudeProxy; everything else goes direct.
var proxiedDomains = []string{
	"claude.ai", "claude.com", "anthropic.com", "clau.de", "claudeusercontent.com",
	"openai.com", "chatgpt.com", "oaistatic.com", "oaiusercontent.com",
	"auth0.com", "identrust.com",
}

// defaultNoProxy: local and work-VPN hosts never go through the proxy.
var defaultNoProxy = []string{
	"localhost", "127.0.0.1", "::1", ".local", ".lan", ".localhost", ".bx", "bx",
	".example.com", ".example.net",
	"10.0.0.0/8", "192.168.0.0/16", "172.16.0.0/12", "100.64.0.0/10",
}

// applyClaudeProxy gives sessions Squad starts (and Squad itself) the proxy settings.
func applyClaudeProxy(proxy string, extra []string) {
	if proxy == "" {
		return
	}
	url := "http://" + strings.TrimPrefix(proxy, "http://")
	no := strings.Join(append(append([]string{}, defaultNoProxy...), extra...), ",")
	for _, kv := range [][2]string{
		{"HTTPS_PROXY", url}, {"https_proxy", url},
		{"NO_PROXY", no}, {"no_proxy", no},
	} {
		tmux.ExtraEnv = append(tmux.ExtraEnv, kv[0]+"="+kv[1])
		_ = os.Setenv(kv[0], kv[1])
	}
}

// proxyPAC is the auto-config file for macOS: only proxiedDomains go to the proxy.
func proxyPAC(proxy string) string {
	if proxy == "" {
		return "function FindProxyForURL(url, host) { return \"DIRECT\"; }\n"
	}
	var b strings.Builder
	b.WriteString("function FindProxyForURL(url, host) {\n")
	for _, d := range proxiedDomains {
		fmt.Fprintf(&b, "  if (host == %q || dnsDomainIs(host, %q)) return \"PROXY %s\";\n", d, "."+d, strings.TrimPrefix(proxy, "http://"))
	}
	b.WriteString("  return \"DIRECT\";\n}\n")
	return b.String()
}
