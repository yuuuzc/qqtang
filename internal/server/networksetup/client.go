package networksetup

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type clientEndpointFile struct {
	name string
	keys []string
}

var clientEndpointFiles = []clientEndpointFile{
	{name: "DirCfg.ini", keys: []string{"DirIP"}},
	{name: "p2psvrInfo.ini", keys: []string{"tcpip", "udpip", "stunip"}},
	{name: "caserver.ini", keys: []string{"ip", "ip2"}},
	{name: "webserver.ini", keys: []string{"ip", "ip2"}},
}

// ApplyClientTarget updates only the explicit server-address keys used by the
// prepared client. Original client files under source/ are never touched.
func ApplyClientTarget(clientRoot, serverIP string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return applyClientTarget(ctx, clientRoot, serverIP, net.DefaultResolver.LookupNetIP)
}

func applyClientTarget(ctx context.Context, clientRoot, serverHost string, lookup func(context.Context, string, string) ([]netip.Addr, error)) error {
	address, err := resolveEndpointIPv4(ctx, serverHost, false, lookup)
	if err != nil {
		return fmt.Errorf("resolve client server address %q: %w", serverHost, err)
	}
	host := address.String()
	type endpointUpdate struct {
		name     string
		path     string
		original []byte
		updated  []byte
	}
	updates := make([]endpointUpdate, 0, len(clientEndpointFiles))
	for _, endpointFile := range clientEndpointFiles {
		path := filepath.Join(clientRoot, endpointFile.name)
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read client endpoint file %s: %w", endpointFile.name, readErr)
		}
		updated := data
		for _, key := range endpointFile.keys {
			pattern := regexp.MustCompile(`(?mi)^` + regexp.QuoteMeta(key) + `=[^\r\n]*`)
			if !pattern.Match(updated) {
				return fmt.Errorf("client endpoint file %s has no %s key", endpointFile.name, key)
			}
			updated = pattern.ReplaceAll(updated, []byte(key+"="+host))
		}
		updates = append(updates, endpointUpdate{
			name: endpointFile.name, path: path,
			original: append([]byte(nil), data...), updated: append([]byte(nil), updated...),
		})
	}
	for index, update := range updates {
		if err := replaceFile(update.path, update.updated); err != nil {
			rollbackErrors := make([]string, 0, index)
			for rollbackIndex := index - 1; rollbackIndex >= 0; rollbackIndex-- {
				previous := updates[rollbackIndex]
				if rollbackErr := replaceFile(previous.path, previous.original); rollbackErr != nil {
					rollbackErrors = append(rollbackErrors, fmt.Sprintf("restore %s: %v", previous.name, rollbackErr))
				}
			}
			if len(rollbackErrors) != 0 {
				return fmt.Errorf("update client endpoint file %s: %w; rollback failed: %s", update.name, err, strings.Join(rollbackErrors, "; "))
			}
			return fmt.Errorf("update client endpoint file %s: %w", update.name, err)
		}
	}
	return nil
}

func replaceFile(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".endpoint-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := file.Name()
	defer os.Remove(temporaryPath)
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
