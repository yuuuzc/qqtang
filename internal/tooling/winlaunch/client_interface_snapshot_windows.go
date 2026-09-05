package winlaunch

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"syscall"
	"time"
)

const (
	clientServiceLocatorPointerRVA = 0x3FAF04
	clientServiceQuerySlot         = 0x14
	clientInterfaceMaximumSlots    = 512
)

// ClientInterfaceMethod identifies one entry in a live interface vtable. The
// slot count is supplied by static call-site evidence; this snapshot does not
// claim that every readable entry belongs to the interface.
type ClientInterfaceMethod struct {
	Index           int    `json:"index"`
	SlotOffset      string `json:"slot_offset"`
	Address         string `json:"address"`
	Module          string `json:"module,omitempty"`
	RVA             string `json:"rva,omitempty"`
	CodeHex         string `json:"code_hex,omitempty"`
	ResolvedAddress string `json:"resolved_address,omitempty"`
	ResolvedModule  string `json:"resolved_module,omitempty"`
	ResolvedRVA     string `json:"resolved_rva,omitempty"`
	ResolvedCodeHex string `json:"resolved_code_hex,omitempty"`
	JumpDepth       int    `json:"jump_depth,omitempty"`
}

// ClientInterfaceSnapshot joins a statically discovered Client IID with the
// concrete object and vtable registered in one running Client.exe process.
// The query writes only to a disposable scratch allocation; it does not patch
// module bytes, client objects, or vtable entries.
type ClientInterfaceSnapshot struct {
	PID                          uint32                  `json:"pid"`
	ClientModuleBase             string                  `json:"client_module_base"`
	InterfaceIDModule            string                  `json:"interface_id_module"`
	InterfaceIDModuleBase        string                  `json:"interface_id_module_base"`
	ServiceLocatorPointerAddress string                  `json:"service_locator_pointer_address"`
	ServiceLocator               string                  `json:"service_locator"`
	ServiceLocatorVTable         string                  `json:"service_locator_vtable"`
	QueryMethod                  string                  `json:"query_method"`
	InterfaceIDAddress           string                  `json:"interface_id_address"`
	InterfaceIDRVA               string                  `json:"interface_id_rva"`
	InterfaceIDHex               string                  `json:"interface_id_hex"`
	QueryResult                  string                  `json:"query_result"`
	Available                    bool                    `json:"available"`
	Object                       string                  `json:"object,omitempty"`
	VTable                       string                  `json:"vtable,omitempty"`
	RequestedSlots               int                     `json:"requested_slots"`
	Methods                      []ClientInterfaceMethod `json:"methods,omitempty"`
	Behavior                     string                  `json:"behavior"`
}

// QueryClientInterfaceSnapshot calls the Client service locator's proven
// stdcall lookup method (slot +0x14) and reads the returned object's vtable.
// interfaceIDRVA is relative to the live Client.exe module base.
func QueryClientInterfaceSnapshot(pid uint32, interfaceIDRVA uint32, slots int, timeout time.Duration) (ClientInterfaceSnapshot, error) {
	return QueryClientModuleInterfaceSnapshot(pid, "Client.exe", interfaceIDRVA, slots, timeout)
}

// QueryClientModuleInterfaceSnapshot queries an interface whose 16-byte IID
// lives in interfaceIDModule. The service locator itself remains the proven
// Client.exe global object; only the IID storage module is selectable.
func QueryClientModuleInterfaceSnapshot(pid uint32, interfaceIDModule string, interfaceIDRVA uint32, slots int, timeout time.Duration) (ClientInterfaceSnapshot, error) {
	interfaceIDModule = strings.TrimSpace(interfaceIDModule)
	result := ClientInterfaceSnapshot{
		PID:               pid,
		InterfaceIDModule: interfaceIDModule,
		InterfaceIDRVA:    fmt.Sprintf("0x%08X", interfaceIDRVA),
		RequestedSlots:    slots,
		Behavior:          "scratch-only service-interface query; no Client.exe, object, or vtable writes",
	}
	if pid == 0 {
		return result, fmt.Errorf("pid must be non-zero")
	}
	if interfaceIDModule == "" {
		return result, fmt.Errorf("interface IID module must be non-empty")
	}
	if interfaceIDRVA == 0 || interfaceIDRVA >= 0x01000000 {
		return result, fmt.Errorf("interface IID RVA 0x%X is outside the expected module image range", interfaceIDRVA)
	}
	if slots <= 0 || slots > clientInterfaceMaximumSlots {
		return result, fmt.Errorf("slot count %d is outside 1..%d", slots, clientInterfaceMaximumSlots)
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	process, err := syscall.OpenProcess(attachedProcessAccess|processCreateThread, false, pid)
	if err != nil {
		return result, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)

	moduleBase, err := waitForModule(process, pid, "Client.exe", timeout)
	if err != nil {
		return result, err
	}
	result.ClientModuleBase = formatInterfaceAddress(moduleBase)
	interfaceIDModuleBase := moduleBase
	if !strings.EqualFold(interfaceIDModule, "Client.exe") {
		interfaceIDModuleBase, err = waitForModule(process, pid, interfaceIDModule, timeout)
		if err != nil {
			return result, err
		}
	}
	result.InterfaceIDModuleBase = formatInterfaceAddress(interfaceIDModuleBase)
	locatorPointerAddress := moduleBase + clientServiceLocatorPointerRVA
	result.ServiceLocatorPointerAddress = formatInterfaceAddress(locatorPointerAddress)

	locator, err := readRemotePointer(process, locatorPointerAddress, "Client service-locator pointer")
	if err != nil {
		return result, err
	}
	result.ServiceLocator = formatInterfaceAddress(locator)
	locatorVTable, err := readRemotePointer(process, locator, "Client service-locator vtable")
	if err != nil {
		return result, err
	}
	result.ServiceLocatorVTable = formatInterfaceAddress(locatorVTable)
	queryMethod, err := readRemotePointer(process, locatorVTable+clientServiceQuerySlot, "Client service-locator query method")
	if err != nil {
		return result, err
	}
	result.QueryMethod = formatInterfaceAddress(queryMethod)

	interfaceIDAddress := interfaceIDModuleBase + uintptr(interfaceIDRVA)
	result.InterfaceIDAddress = formatInterfaceAddress(interfaceIDAddress)
	interfaceID, ok := readRemote(process, interfaceIDAddress, 16)
	if !ok || len(interfaceID) != 16 {
		return result, fmt.Errorf("read %s interface IID at %s", interfaceIDModule, result.InterfaceIDAddress)
	}
	result.InterfaceIDHex = hex.EncodeToString(interfaceID)

	page, _, allocErr := procVirtualAllocEx.Call(
		uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite,
	)
	if page == 0 || page > 0xffffffff {
		return result, fmt.Errorf("VirtualAllocEx Client interface query: 0x%X (%v)", page, allocErr)
	}
	freeAllocation := true
	defer func() {
		if freeAllocation {
			procVirtualFreeEx.Call(uintptr(process), page, 0, memRelease)
		}
	}()

	objectOutputAddress := page + 0x800
	statusAddress := page + 0x804
	stub := buildClientInterfaceQueryStub(locator, queryMethod, interfaceIDAddress, objectOutputAddress, statusAddress)
	if err := writeRemote(process, page, stub); err != nil {
		return result, fmt.Errorf("write Client interface query stub: %w", err)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))

	// A timed-out remote thread may still use the scratch page. In that case it
	// is intentionally retained instead of being freed underneath the thread.
	freeAllocation = false
	if _, err := remoteCallOne(process, page, 0, timeout); err != nil {
		return result, fmt.Errorf("run Client interface query stub: %w", err)
	}
	freeAllocation = true

	status, ok := readRemote(process, statusAddress, 4)
	if !ok || len(status) != 4 {
		return result, fmt.Errorf("read Client interface query result")
	}
	result.QueryResult = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(status))
	objectBytes, ok := readRemote(process, objectOutputAddress, 4)
	if !ok || len(objectBytes) != 4 {
		return result, fmt.Errorf("read Client interface object output")
	}
	object := uintptr(binary.LittleEndian.Uint32(objectBytes))
	if object < 0x10000 {
		return result, nil
	}
	result.Available = true
	result.Object = formatInterfaceAddress(object)
	vtable, err := readRemotePointer(process, object, "Client interface vtable")
	if err != nil {
		return result, err
	}
	result.VTable = formatInterfaceAddress(vtable)

	vtableBytes, ok := readRemote(process, vtable, slots*4)
	if !ok || len(vtableBytes) != slots*4 {
		return result, fmt.Errorf("read %d Client interface vtable slots at %s", slots, result.VTable)
	}
	modules := snapshotRemoteModules(process)
	result.Methods = make([]ClientInterfaceMethod, 0, slots)
	for index := 0; index < slots; index++ {
		methodAddress := binary.LittleEndian.Uint32(vtableBytes[index*4 : index*4+4])
		method := ClientInterfaceMethod{
			Index:      index,
			SlotOffset: fmt.Sprintf("0x%X", index*4),
			Address:    fmt.Sprintf("0x%08X", methodAddress),
		}
		for _, module := range modules {
			if methodAddress < module.base || uint64(methodAddress) >= uint64(module.base)+uint64(module.size) {
				continue
			}
			method.Module = module.name
			method.RVA = fmt.Sprintf("0x%08X", methodAddress-module.base)
			break
		}
		method.CodeHex, method.ResolvedAddress, method.ResolvedCodeHex, method.JumpDepth =
			resolveClientInterfaceMethod(process, uintptr(methodAddress), 8)
		if resolved, err := strconvInterfaceAddress(method.ResolvedAddress); err == nil {
			for _, module := range modules {
				if resolved < module.base || uint64(resolved) >= uint64(module.base)+uint64(module.size) {
					continue
				}
				method.ResolvedModule = module.name
				method.ResolvedRVA = fmt.Sprintf("0x%08X", resolved-module.base)
				break
			}
		}
		result.Methods = append(result.Methods, method)
	}
	return result, nil
}

func resolveClientInterfaceMethod(process syscall.Handle, entry uintptr, maximumDepth int) (string, string, string, int) {
	current := entry
	initialHex := ""
	resolvedHex := ""
	depth := 0
	for depth <= maximumDepth {
		code, ok := readRemote(process, current, 16)
		if !ok || len(code) < 6 {
			break
		}
		if initialHex == "" {
			initialHex = hex.EncodeToString(code)
		}
		resolvedHex = hex.EncodeToString(code)
		next := uintptr(0)
		switch {
		case code[0] == 0xE9:
			displacement := int64(int32(binary.LittleEndian.Uint32(code[1:5])))
			next = uintptr(int64(current) + 5 + displacement)
		case code[0] == 0xEB:
			next = uintptr(int64(current) + 2 + int64(int8(code[1])))
		case code[0] == 0xFF && code[1] == 0x25:
			pointerAddress := uintptr(binary.LittleEndian.Uint32(code[2:6]))
			pointer, err := readRemotePointer(process, pointerAddress, "interface jump pointer")
			if err == nil {
				next = pointer
			}
		case code[0] == 0x68 && code[5] == 0xC3:
			next = uintptr(binary.LittleEndian.Uint32(code[1:5]))
		case code[0] == 0xB8 && code[5] == 0xFF && code[6] == 0xE0:
			next = uintptr(binary.LittleEndian.Uint32(code[1:5]))
		}
		if next < 0x10000 || next > 0xffffffff || next == current || depth == maximumDepth {
			break
		}
		current = next
		depth++
	}
	return initialHex, formatInterfaceAddress(current), resolvedHex, depth
}

func strconvInterfaceAddress(text string) (uint32, error) {
	var value uint32
	_, err := fmt.Sscanf(text, "0x%X", &value)
	return value, err
}

func buildClientInterfaceQueryStub(locator, method, interfaceIDAddress, objectOutputAddress, statusAddress uintptr) []byte {
	stub := []byte{0xC7, 0x05} // mov dword ptr [objectOutputAddress], 0
	stub = binary.LittleEndian.AppendUint32(stub, uint32(objectOutputAddress))
	stub = binary.LittleEndian.AppendUint32(stub, 0)
	stub = appendPushImmediate(stub, objectOutputAddress)
	stub = appendPushImmediate(stub, interfaceIDAddress)
	stub = appendPushImmediate(stub, locator)
	stub = append(stub, 0xB8) // mov eax, method; call eax
	stub = binary.LittleEndian.AppendUint32(stub, uint32(method))
	stub = append(stub, 0xFF, 0xD0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(statusAddress))
	stub = append(stub, 0x31, 0xC0, 0xC2, 0x04, 0x00) // thread exit 0; ret 4
	return stub
}

func readRemotePointer(process syscall.Handle, address uintptr, label string) (uintptr, error) {
	data, ok := readRemote(process, address, 4)
	if !ok || len(data) != 4 {
		return 0, fmt.Errorf("read %s at 0x%08X", label, address)
	}
	value := uintptr(binary.LittleEndian.Uint32(data))
	if value < 0x10000 {
		return 0, fmt.Errorf("%s is 0x%08X", label, value)
	}
	return value, nil
}

func formatInterfaceAddress(address uintptr) string {
	return fmt.Sprintf("0x%08X", address)
}
