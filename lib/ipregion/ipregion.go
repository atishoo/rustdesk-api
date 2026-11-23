// Package ipregion resolves observed IP addresses using the bundled offline databases.
package ipregion

import (
	"fmt"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lionsoul2014/ip2region/binding/golang/service"
)

var (
	mu       sync.Mutex
	resolver *service.Ip2Region
)

// Init validates and loads both databases once, before accepting requests.
func Init(resourcesPath string) error {
	v4, err := service.NewV4Config(service.BufferCache, filepath.Join(resourcesPath, "ip2region", "ip2region_v4.xdb"), 1)
	if err != nil {
		return fmt.Errorf("load IPv4 region database: %w", err)
	}
	v6, err := service.NewV6Config(service.BufferCache, filepath.Join(resourcesPath, "ip2region", "ip2region_v6.xdb"), 1)
	if err != nil {
		return fmt.Errorf("load IPv6 region database: %w", err)
	}
	next, err := service.NewIp2Region(v4, v6)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	if resolver != nil {
		resolver.Close()
	}
	resolver = next
	return nil
}

func Close() {
	mu.Lock()
	defer mu.Unlock()
	if resolver != nil {
		resolver.Close()
		resolver = nil
	}
}

// ParseIP accepts an IP or a bracketed/IPv4 socket address without changing its stored source.
func ParseIP(raw string) (netip.Addr, error) {
	value := strings.TrimSpace(raw)
	address, err := netip.ParseAddr(value)
	if err != nil {
		if endpoint, endpointErr := netip.ParseAddrPort(value); endpointErr == nil {
			address, err = endpoint.Addr(), nil
		} else if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
			address, err = netip.ParseAddr(value[1 : len(value)-1])
		}
	}
	return address.Unmap().WithZone(""), err
}

// Lookup returns a display-ready location snapshot; empty source values stay empty.
func Lookup(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	address, err := ParseIP(raw)
	if err != nil {
		return "未知"
	}
	switch {
	case address.IsLoopback():
		return "本机地址"
	case address.IsPrivate(), address.IsLinkLocalUnicast(), netip.MustParsePrefix("100.64.0.0/10").Contains(address):
		return "内网地址"
	case address.IsUnspecified():
		return "未指定地址"
	case address.IsMulticast():
		return "组播地址"
	}
	// The upstream searcher updates an I/O counter even with full-buffer caching.
	mu.Lock()
	defer mu.Unlock()
	if resolver == nil {
		return "未知"
	}
	region, err := resolver.Search(address.AsSlice())
	if err != nil || region == "" {
		return "未知"
	}
	// The bundled format is country|province|city|ISP|ISO country code.
	parts := strings.Split(region, "|")
	if len(parts) == 5 {
		parts = parts[:4]
	}
	labels := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && part != "0" && (len(labels) == 0 || labels[len(labels)-1] != part) {
			labels = append(labels, part)
		}
	}
	if len(labels) == 0 {
		return "未知"
	}
	return strings.Join(labels, " · ")
}
