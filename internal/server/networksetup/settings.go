package networksetup

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const SchemaVersion = 3

type Mode string

const (
	ModeLocal      Mode = "local"
	ModeLANHost    Mode = "lan-host"
	ModeRemoteHost Mode = "remote-host"
)

type Settings struct {
	SchemaVersion  int    `json:"schema_version"`
	Mode           Mode   `json:"mode"`
	ServerIP       string `json:"server_ip"`
	ClientServerIP string `json:"client_server_ip"`
	GMRemote       bool   `json:"gm_remote"`
}

func Default() Settings {
	return Settings{
		SchemaVersion:  SchemaVersion,
		Mode:           ModeLocal,
		ServerIP:       "127.0.0.1",
		ClientServerIP: "127.0.0.1",
	}
}

func (settings Settings) Validate() error {
	if settings.SchemaVersion != SchemaVersion {
		return fmt.Errorf("network schema_version %d, want %d", settings.SchemaVersion, SchemaVersion)
	}
	serverHost, address, isAddress, err := normalizeEndpointHost(settings.ServerIP)
	if err != nil {
		return fmt.Errorf("network server_ip %q is not a valid IPv4 address or DNS name", settings.ServerIP)
	}
	switch settings.Mode {
	case ModeLocal:
		if !isAddress || !address.IsLoopback() {
			return fmt.Errorf("local mode server_ip must be loopback")
		}
	case ModeLANHost:
		if !isAddress || !address.IsPrivate() {
			return fmt.Errorf("%s server_ip must be a private LAN address", settings.Mode)
		}
	case ModeRemoteHost:
		if isAddress && (!address.IsGlobalUnicast() || address.IsLoopback() || address.IsPrivate()) {
			return fmt.Errorf("%s server_ip must be a public unicast IPv4 address", settings.Mode)
		}
		if !isAddress && serverHost == "" {
			return fmt.Errorf("%s server_ip must be a public IPv4 address or DNS name", settings.Mode)
		}
	default:
		return fmt.Errorf("unsupported network mode %q", settings.Mode)
	}
	_, clientAddress, clientIsAddress, err := normalizeEndpointHost(settings.ClientServerIP)
	if err != nil || (clientIsAddress && !isUnicastIPv4(clientAddress)) {
		return fmt.Errorf("network client_server_ip %q is not a unicast IPv4 address or DNS name", settings.ClientServerIP)
	}
	if settings.GMRemote && settings.Mode != ModeLANHost && settings.Mode != ModeRemoteHost {
		return fmt.Errorf("gm_remote requires lan-host or remote-host mode")
	}
	return nil
}

func (settings Settings) ResolveServerIPv4() (netip.Addr, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return resolveEndpointIPv4(ctx, settings.ServerIP, settings.Mode == ModeRemoteHost, net.DefaultResolver.LookupNetIP)
}

func resolveServerIPv4(ctx context.Context, host string, lookup func(context.Context, string, string) ([]netip.Addr, error)) (netip.Addr, error) {
	return resolveEndpointIPv4(ctx, host, true, lookup)
}

func resolveEndpointIPv4(ctx context.Context, host string, publicOnly bool, lookup func(context.Context, string, string) ([]netip.Addr, error)) (netip.Addr, error) {
	normalized, address, isAddress, err := normalizeEndpointHost(host)
	if err != nil {
		return netip.Addr{}, err
	}
	if isAddress {
		if !isUnicastIPv4(address) || publicOnly && (address.IsLoopback() || address.IsPrivate()) {
			return netip.Addr{}, fmt.Errorf("endpoint %q is not an allowed IPv4 address", normalized)
		}
		return address, nil
	}
	addresses, err := lookup(ctx, "ip4", normalized)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("resolve public server DNS name %q: %w", normalized, err)
	}
	candidates := make([]netip.Addr, 0, len(addresses))
	for _, candidate := range addresses {
		candidate = candidate.Unmap()
		if !isUnicastIPv4(candidate) || publicOnly && (candidate.IsLoopback() || candidate.IsPrivate()) {
			continue
		}
		candidates = append(candidates, candidate)
	}
	if len(candidates) == 0 {
		return netip.Addr{}, fmt.Errorf("DNS name %q has no allowed IPv4 record", normalized)
	}
	sort.Slice(candidates, func(left, right int) bool { return candidates[left].Less(candidates[right]) })
	return candidates[0], nil
}

func normalizeEndpointHost(value string) (string, netip.Addr, bool, error) {
	value = strings.TrimSpace(value)
	if address, err := netip.ParseAddr(value); err == nil {
		address = address.Unmap()
		if !address.Is4() {
			return "", netip.Addr{}, false, fmt.Errorf("address is not IPv4")
		}
		return address.String(), address, true, nil
	}
	host := strings.ToLower(strings.TrimSuffix(value, "."))
	if !validDNSName(host) {
		return "", netip.Addr{}, false, fmt.Errorf("invalid DNS name")
	}
	return host, netip.Addr{}, false, nil
}

func validDNSName(host string) bool {
	if len(host) == 0 || len(host) > 253 || !strings.Contains(host, ".") {
		return false
	}
	hasLetter := false
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			switch {
			case character >= 'a' && character <= 'z':
				hasLetter = true
			case character >= '0' && character <= '9', character == '-':
			default:
				return false
			}
		}
	}
	return hasLetter
}

func isUnicastIPv4(address netip.Addr) bool {
	return address.Is4() && (address.IsLoopback() || address.IsPrivate() || address.IsGlobalUnicast()) && !address.IsUnspecified()
}

func (settings Settings) RunsServer() bool {
	return settings.Mode == ModeLocal || settings.Mode == ModeLANHost || settings.Mode == ModeRemoteHost
}

func (settings Settings) BindIP() string {
	if settings.Mode == ModeLANHost || settings.Mode == ModeRemoteHost {
		return "0.0.0.0"
	}
	return settings.ServerIP
}

func Load(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, fmt.Errorf("read network settings: %w", err)
	}
	var settings Settings
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil {
		return Settings{}, fmt.Errorf("decode network settings: %w", err)
	}
	if err := settings.Validate(); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

func Save(path string, settings Settings) error {
	settings.SchemaVersion = SchemaVersion
	settings.ServerIP, _, _, _ = normalizeEndpointHost(settings.ServerIP)
	settings.ClientServerIP, _, _, _ = normalizeEndpointHost(settings.ClientServerIP)
	if err := settings.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("encode network settings: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create network settings directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".network-*.json")
	if err != nil {
		return fmt.Errorf("create network settings temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err = temporary.Write(append(data, '\n')); err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write network settings: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace network settings: %w", err)
	}
	return nil
}

func PrivateIPv4Candidates() []string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	var result []string
	for _, networkInterface := range interfaces {
		if networkInterface.Flags&net.FlagUp == 0 || networkInterface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, addressErr := networkInterface.Addrs()
		if addressErr != nil {
			continue
		}
		for _, value := range addresses {
			prefix, parseErr := netip.ParsePrefix(value.String())
			if parseErr != nil || !prefix.Addr().Is4() || !prefix.Addr().IsPrivate() {
				continue
			}
			text := prefix.Addr().String()
			if _, duplicate := seen[text]; duplicate {
				continue
			}
			seen[text] = struct{}{}
			result = append(result, text)
		}
	}
	sort.Strings(result)
	return result
}
