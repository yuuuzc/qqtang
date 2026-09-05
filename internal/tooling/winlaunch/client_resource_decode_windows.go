package winlaunch

import (
	"encoding/binary"
	"fmt"
	"strings"
	"syscall"
	"time"
)

const (
	clientResourcePointerRVA = 0x407248
	clientResourceDecodeSlot = 0x64
	clientResourceMaxSize    = 0x100000
)

// ClientResourceDecodeResult records the exact live interface selected for a
// read-only resource decode. The decoder writes only into a new remote scratch
// allocation and never patches the client module or its resource object.
type ClientResourceDecodeResult struct {
	PID             uint32 `json:"pid"`
	ModuleBase      string `json:"module_base"`
	ResourceObject  string `json:"resource_object"`
	ResourceVTable  string `json:"resource_vtable"`
	DecodeMethod    string `json:"decode_method"`
	ResourcePath    string `json:"resource_path"`
	ExpandedSize    int    `json:"expanded_size"`
	Succeeded       bool   `json:"succeeded"`
	DecodedResource []byte `json:"-"`
}

// DecodeClientResource invokes the already initialized resource interface in
// one explicitly selected 32-bit Client.exe process. The filename and output
// buffer live in a disposable remote allocation. No module bytes, vtables, or
// persistent client state are changed.
func DecodeClientResource(pid uint32, resourcePath string, expandedSize int, timeout time.Duration) (ClientResourceDecodeResult, error) {
	result := ClientResourceDecodeResult{PID: pid, ResourcePath: resourcePath, ExpandedSize: expandedSize}
	if pid == 0 {
		return result, fmt.Errorf("pid must be non-zero")
	}
	if resourcePath == "" || strings.IndexByte(resourcePath, 0) >= 0 {
		return result, fmt.Errorf("resource path must be a non-empty C string")
	}
	if expandedSize <= 0 || expandedSize > clientResourceMaxSize {
		return result, fmt.Errorf("expanded size 0x%X is outside 1..0x%X", expandedSize, clientResourceMaxSize)
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
	result.ModuleBase = fmt.Sprintf("0x%08X", moduleBase)
	objectPointerBytes, ok := readRemote(process, moduleBase+clientResourcePointerRVA, 4)
	if !ok || len(objectPointerBytes) != 4 {
		return result, fmt.Errorf("read Client resource-interface pointer")
	}
	resourceObject := uintptr(binary.LittleEndian.Uint32(objectPointerBytes))
	if resourceObject < 0x10000 {
		return result, fmt.Errorf("Client resource-interface pointer is 0x%08X", resourceObject)
	}
	result.ResourceObject = fmt.Sprintf("0x%08X", resourceObject)
	vtableBytes, ok := readRemote(process, resourceObject, 4)
	if !ok || len(vtableBytes) != 4 {
		return result, fmt.Errorf("read Client resource-interface vtable")
	}
	vtable := uintptr(binary.LittleEndian.Uint32(vtableBytes))
	if vtable < 0x10000 {
		return result, fmt.Errorf("Client resource-interface vtable is 0x%08X", vtable)
	}
	result.ResourceVTable = fmt.Sprintf("0x%08X", vtable)
	methodBytes, ok := readRemote(process, vtable+clientResourceDecodeSlot, 4)
	if !ok || len(methodBytes) != 4 {
		return result, fmt.Errorf("read Client resource decode method")
	}
	method := uintptr(binary.LittleEndian.Uint32(methodBytes))
	if method < 0x10000 {
		return result, fmt.Errorf("Client resource decode method is 0x%08X", method)
	}
	result.DecodeMethod = fmt.Sprintf("0x%08X", method)

	filename := append([]byte(resourcePath), 0)
	filenameArea := uintptr((len(filename) + 0xfff) &^ 0xfff)
	outputArea := uintptr((expandedSize + 1 + 0xfff) &^ 0xfff)
	allocationSize := uintptr(0x1000) + filenameArea + outputArea + 0x1000
	page, _, allocErr := procVirtualAllocEx.Call(
		uintptr(process), 0, allocationSize, memReserve|memCommit, pageExecuteReadWrite,
	)
	if page == 0 || page > 0xffffffff {
		return result, fmt.Errorf("VirtualAllocEx Client resource decode: 0x%X (%v)", page, allocErr)
	}
	freeAllocation := true
	defer func() {
		if freeAllocation {
			procVirtualFreeEx.Call(uintptr(process), page, 0, memRelease)
		}
	}()

	filenameAddress := page + 0x1000
	outputAddress := filenameAddress + filenameArea
	statusAddress := outputAddress + outputArea
	stub := buildClientResourceDecodeStub(resourceObject, method, filenameAddress, outputAddress, statusAddress, expandedSize)
	if err := writeRemote(process, page, stub); err != nil {
		return result, fmt.Errorf("write Client resource decode stub: %w", err)
	}
	if err := writeRemote(process, filenameAddress, filename); err != nil {
		return result, fmt.Errorf("write Client resource filename: %w", err)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))

	// A timed-out remote thread may still be executing, so retain its code and
	// buffers rather than freeing memory out from under it.
	freeAllocation = false
	if _, err := remoteCallOne(process, page, 0, timeout); err != nil {
		return result, fmt.Errorf("run Client resource decode stub: %w", err)
	}
	freeAllocation = true
	status, ok := readRemote(process, statusAddress, 4)
	if !ok || len(status) != 4 {
		return result, fmt.Errorf("read Client resource decode status")
	}
	result.Succeeded = binary.LittleEndian.Uint32(status) != 0
	if !result.Succeeded {
		return result, nil
	}
	decoded, ok := readRemote(process, outputAddress, expandedSize)
	if !ok || len(decoded) != expandedSize {
		return result, fmt.Errorf("read decoded Client resource")
	}
	result.DecodedResource = decoded
	return result, nil
}

func buildClientResourceDecodeStub(resourceObject, method, filenameAddress, outputAddress, statusAddress uintptr, expandedSize int) []byte {
	stub := []byte{0xB9} // mov ecx, resourceObject
	stub = binary.LittleEndian.AppendUint32(stub, uint32(resourceObject))
	stub = appendPushImmediate(stub, outputAddress)
	stub = appendPushImmediate(stub, uintptr(expandedSize))
	stub = appendPushImmediate(stub, filenameAddress)
	stub = append(stub, 0xB8) // mov eax, method; call eax
	stub = binary.LittleEndian.AppendUint32(stub, uint32(method))
	stub = append(stub, 0xFF, 0xD0, 0x0F, 0xB6, 0xC0) // call eax; movzx eax, al
	stub = append(stub, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(statusAddress))
	stub = append(stub, 0x31, 0xC0, 0xC2, 0x04, 0x00) // thread exit 0; ret 4
	return stub
}
