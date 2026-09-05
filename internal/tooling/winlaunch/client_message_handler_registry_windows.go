package winlaunch

import (
	"encoding/binary"
	"fmt"
	"sort"
	"syscall"
	"time"
)

const (
	clientMessageRegistryHeadOffset = uintptr(0x08)
	// The dispatcher passes an outer object whose legacy MSVC map header keeps
	// its node count at +0x10. Live adventure registries captured on 2026-08-16
	// contained the small counts 16 and 10 at this offset; +0x14 is a dynamic
	// pointer and must not be interpreted as the count.
	clientMessageRegistrySizeOffset = uintptr(0x10)
	clientMessageTreeNodeSize       = 0x18
	clientMessageTreeKeyOffset      = 0x0C
	clientMessageTreeValueOffset    = 0x10
	clientMessageTreeMaximumNodes   = 16384
)

var defaultClientMessageRegistrySeedSchemas = []uint32{
	0x000007DA, // RESPONSE_CREATE_ROOM
	0x000007E0, // ACK_ENTER_ROOM
	0x0000081D, // NOTIFY_PLAYER_ITEMADD
	0x00000FA7, // NOTIFY_PLAYER_DIE
	0x00000FBB, // NOTIFY_GAME_OVER
}

type ClientMessageHandlerRegistryEntry struct {
	SchemaID                   uint32 `json:"schema_id"`
	SchemaHex                  string `json:"schema_hex"`
	Node                       string `json:"node"`
	Handler                    string `json:"handler"`
	HandlerVTable              string `json:"handler_vtable,omitempty"`
	HandlerProcessMethod       string `json:"handler_process_method,omitempty"`
	HandlerProcessMethodRVA    string `json:"handler_process_method_rva,omitempty"`
	HandlerAcceptanceMethod    string `json:"handler_acceptance_method,omitempty"`
	HandlerAcceptanceMethodRVA string `json:"handler_acceptance_method_rva,omitempty"`
}

type ClientMessageHandlerRegistryCapture struct {
	PID           uint32                              `json:"pid"`
	ModuleBase    string                              `json:"module_base"`
	Registry      string                              `json:"registry"`
	Head          string                              `json:"head"`
	DeclaredSize  uint32                              `json:"declared_size"`
	TraversedSize int                                 `json:"traversed_size"`
	Entries       []ClientMessageHandlerRegistryEntry `json:"entries"`
	Behavior      string                              `json:"behavior"`
}

// FindClientMessageHandlerRegistries locates live dispatcher registries by
// finding known schema keys and validating the surrounding legacy MSVC tree.
// It is read-only and does not require a message to arrive while it runs.
func FindClientMessageHandlerRegistries(pid uint32, seedSchemas []uint32) ([]ClientMessageHandlerRegistryCapture, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	if len(seedSchemas) == 0 {
		seedSchemas = defaultClientMessageRegistrySeedSchemas
	}
	process, err := syscall.OpenProcess(readOnlyProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)

	candidates := make(map[uintptr]struct{})
	for _, schemaID := range seedSchemas {
		pattern := make([]byte, 4)
		binary.LittleEndian.PutUint32(pattern, schemaID)
		matches, searchErr := SearchProcessMemory(pid, pattern, 4096)
		if searchErr != nil {
			return nil, fmt.Errorf("search schema 0x%08X: %w", schemaID, searchErr)
		}
		for _, keyAddress := range matches {
			if keyAddress < clientMessageTreeKeyOffset+0x10000 {
				continue
			}
			nodeAddress := keyAddress - clientMessageTreeKeyOffset
			node, ok := readRemote(process, nodeAddress, clientMessageTreeNodeSize)
			if !ok || binary.LittleEndian.Uint32(node[clientMessageTreeKeyOffset:clientMessageTreeKeyOffset+4]) != schemaID {
				continue
			}
			head, found := findClientMessageTreeHead(process, nodeAddress)
			if !found || head < clientMessageRegistryHeadOffset+0x10000 {
				continue
			}
			registry := head - clientMessageRegistryHeadOffset
			header, headerOK := readRemote(process, registry, int(clientMessageRegistrySizeOffset+4))
			if !headerOK || uintptr(binary.LittleEndian.Uint32(header[clientMessageRegistryHeadOffset:clientMessageRegistryHeadOffset+4])) != head {
				continue
			}
			size := binary.LittleEndian.Uint32(header[clientMessageRegistrySizeOffset : clientMessageRegistrySizeOffset+4])
			if size == 0 || size > clientMessageTreeMaximumNodes {
				continue
			}
			candidates[registry] = struct{}{}
		}
	}

	addresses := make([]uintptr, 0, len(candidates))
	for address := range candidates {
		addresses = append(addresses, address)
	}
	sort.Slice(addresses, func(i, j int) bool { return addresses[i] < addresses[j] })
	result := make([]ClientMessageHandlerRegistryCapture, 0, len(addresses))
	for _, address := range addresses {
		capture, captureErr := ReadClientMessageHandlerRegistry(pid, address)
		if captureErr == nil {
			result = append(result, capture)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].DeclaredSize != result[j].DeclaredSize {
			return result[i].DeclaredSize > result[j].DeclaredSize
		}
		return result[i].Registry < result[j].Registry
	})
	return result, nil
}

func findClientMessageTreeHead(process syscall.Handle, start uintptr) (uintptr, bool) {
	visited := make(map[uintptr]struct{})
	current := start
	for depth := 0; depth < 512; depth++ {
		if current < 0x10000 || current > 0xffffffff {
			return 0, false
		}
		if _, duplicate := visited[current]; duplicate {
			return 0, false
		}
		visited[current] = struct{}{}
		node, ok := readRemote(process, current, clientMessageTreeNodeSize)
		if !ok {
			return 0, false
		}
		// MSVC's tree header has _Isnil set at byte +0x15.
		if node[0x15] != 0 {
			return current, true
		}
		current = uintptr(binary.LittleEndian.Uint32(node[4:8]))
	}
	return 0, false
}

// ReadClientMessageHandlerRegistry walks the legacy MSVC std::map stored in a
// captured Client dispatcher registry. It opens the target read-only and does
// not call, patch, suspend, or allocate anything in the client process.
func ReadClientMessageHandlerRegistry(pid uint32, registry uintptr) (ClientMessageHandlerRegistryCapture, error) {
	result := ClientMessageHandlerRegistryCapture{
		PID: pid, Registry: fmt.Sprintf("0x%08X", registry),
		Behavior: "read-only traversal of the Client decoded-message handler registry; no target-process execution or writes",
	}
	if pid == 0 || registry < 0x10000 || registry > 0xffffffff {
		return result, fmt.Errorf("pid and 32-bit registry address must be valid")
	}
	process, err := syscall.OpenProcess(readOnlyProcessAccess, false, pid)
	if err != nil {
		return result, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)
	moduleBase, err := waitForModule(process, pid, "Client.exe", time.Second)
	if err != nil {
		return result, err
	}
	result.ModuleBase = fmt.Sprintf("0x%08X", moduleBase)
	header, ok := readRemote(process, registry, int(clientMessageRegistrySizeOffset+4))
	if !ok {
		return result, fmt.Errorf("read Client handler registry 0x%08X", registry)
	}
	head := uintptr(binary.LittleEndian.Uint32(header[clientMessageRegistryHeadOffset : clientMessageRegistryHeadOffset+4]))
	declaredSize := binary.LittleEndian.Uint32(header[clientMessageRegistrySizeOffset : clientMessageRegistrySizeOffset+4])
	result.Head = fmt.Sprintf("0x%08X", head)
	result.DeclaredSize = declaredSize
	if head < 0x10000 || head > 0xffffffff {
		return result, fmt.Errorf("Client handler registry head is invalid: 0x%08X", head)
	}
	if declaredSize > clientMessageTreeMaximumNodes {
		return result, fmt.Errorf("Client handler registry declares %d nodes, maximum is %d", declaredSize, clientMessageTreeMaximumNodes)
	}
	headNode, ok := readRemote(process, head, clientMessageTreeNodeSize)
	if !ok {
		return result, fmt.Errorf("read Client handler registry head node 0x%08X", head)
	}
	root := uintptr(binary.LittleEndian.Uint32(headNode[4:8]))
	if declaredSize == 0 {
		return result, nil
	}
	if root == head || root < 0x10000 || root > 0xffffffff {
		return result, fmt.Errorf("Client handler registry root is invalid: 0x%08X", root)
	}

	visited := make(map[uintptr]struct{}, declaredSize)
	pending := []uintptr{root}
	for len(pending) != 0 {
		if len(visited) == int(declaredSize) {
			break
		}
		nodeAddress := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if nodeAddress == head {
			continue
		}
		if _, exists := visited[nodeAddress]; exists {
			continue
		}
		if len(visited) >= clientMessageTreeMaximumNodes {
			return result, fmt.Errorf("Client handler registry traversal exceeded %d nodes", clientMessageTreeMaximumNodes)
		}
		if nodeAddress < 0x10000 || nodeAddress > 0xffffffff {
			return result, fmt.Errorf("Client handler registry contains invalid node 0x%08X", nodeAddress)
		}
		node, nodeOK := readRemote(process, nodeAddress, clientMessageTreeNodeSize)
		if !nodeOK {
			return result, fmt.Errorf("read Client handler registry node 0x%08X", nodeAddress)
		}
		// This client build uses a shared all-zero leaf sentinel distinct from
		// the map's end/header node. It is not part of the declared node count.
		if binary.LittleEndian.Uint32(node[0:4]) == 0 && binary.LittleEndian.Uint32(node[4:8]) == 0 &&
			binary.LittleEndian.Uint32(node[8:12]) == 0 && binary.LittleEndian.Uint32(node[clientMessageTreeKeyOffset:clientMessageTreeKeyOffset+4]) == 0 {
			continue
		}
		visited[nodeAddress] = struct{}{}
		left := uintptr(binary.LittleEndian.Uint32(node[0:4]))
		right := uintptr(binary.LittleEndian.Uint32(node[8:12]))
		for _, child := range []uintptr{left, right} {
			// This Client build uses both the map header and NULL as leaf
			// sentinels. Neither value is a real tree node.
			if child != 0 && child != head {
				pending = append(pending, child)
			}
		}
		schemaID := binary.LittleEndian.Uint32(node[clientMessageTreeKeyOffset : clientMessageTreeKeyOffset+4])
		handler := binary.LittleEndian.Uint32(node[clientMessageTreeValueOffset : clientMessageTreeValueOffset+4])
		entry := ClientMessageHandlerRegistryEntry{
			SchemaID: schemaID, SchemaHex: fmt.Sprintf("0x%08X", schemaID),
			Node: fmt.Sprintf("0x%08X", nodeAddress), Handler: fmt.Sprintf("0x%08X", handler),
		}
		if handler >= 0x10000 {
			object, objectOK := readRemote(process, uintptr(handler), 4)
			if objectOK {
				vtable := binary.LittleEndian.Uint32(object)
				entry.HandlerVTable = fmt.Sprintf("0x%08X", vtable)
				if vtable >= 0x10000 {
					methods, methodsOK := readRemote(process, uintptr(vtable), 12)
					if methodsOK {
						processMethod := binary.LittleEndian.Uint32(methods[4:8])
						acceptanceMethod := binary.LittleEndian.Uint32(methods[8:12])
						entry.HandlerProcessMethod = fmt.Sprintf("0x%08X", processMethod)
						entry.HandlerProcessMethodRVA = formatModuleRVA(moduleBase, processMethod)
						entry.HandlerAcceptanceMethod = fmt.Sprintf("0x%08X", acceptanceMethod)
						entry.HandlerAcceptanceMethodRVA = formatModuleRVA(moduleBase, acceptanceMethod)
					}
				}
			}
		}
		result.Entries = append(result.Entries, entry)
	}
	result.TraversedSize = len(visited)
	sort.Slice(result.Entries, func(i, j int) bool { return result.Entries[i].SchemaID < result.Entries[j].SchemaID })
	if result.TraversedSize != int(declaredSize) {
		return result, fmt.Errorf("Client handler registry traversed %d nodes, declared %d", result.TraversedSize, declaredSize)
	}
	return result, nil
}
