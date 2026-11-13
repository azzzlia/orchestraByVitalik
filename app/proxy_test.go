package app

import (
	"strings"
	"testing"
)

func TestProxyPAC(t *testing.T) {
	pac := proxyPAC("100.74.167.6:3128")
	for _, want := range []string{`dnsDomainIs(host, ".anthropic.com")`, `host == "claude.ai"`, `PROXY 100.74.167.6:3128`, `return "DIRECT"`} {
		if !strings.Contains(pac, want) {
			t.Errorf("PAC lacks %s:\n%s", want, pac)
		}
	}
	if strings.Contains(proxyPAC(""), "PROXY") {
		t.Error("no proxy configured, PAC must be DIRECT only")
	}
}
