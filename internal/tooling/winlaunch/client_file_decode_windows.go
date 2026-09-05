package winlaunch

import (
	"encoding/binary"
	"fmt"
	"strings"
	"syscall"
	"time"
)

// ClientFileDecodeResult records a call into an audited client-side file
// decoder. Both the path and expanded output are disposable remote buffers;
// the source file is opened read-only by the original decoder.
type ClientFileDecodeResult struct {
	PID           uint32 `json:"pid"`
	Module        string `json:"module"`
	ModuleBase    string `json:"module_base"`
	MethodRVA     string `json:"method_rva"`
	Method        string `json:"method"`
	ResourcePath  string `json:"resource_path"`
	ExpandedSize  int    `json:"expanded_size"`
	Succeeded     bool   `json:"succeeded"`
	DecodedOutput []byte `json:"-"`
}

// DecodeClientModuleFile invokes a thiscall(path,expandedSize,output) method
// whose signature was established from the original DLL. The this register is
// accepted explicitly because some decoders use module state while others do
// not; zero is valid only for a decoder whose implementation does not read it.
func DecodeClientModuleFile(pid uint32, module string, methodRVA uint32, thisAddress uintptr, resourcePath string, expandedSize int, timeout time.Duration) (ClientFileDecodeResult, error) {
	result := ClientFileDecodeResult{
		PID: pid, Module: module, MethodRVA: fmt.Sprintf("0x%08X", methodRVA),
		ResourcePath: resourcePath, ExpandedSize: expandedSize,
	}
	if pid == 0 {
		return result, fmt.Errorf("pid must be non-zero")
	}
	module = strings.TrimSpace(module)
	if module == "" || methodRVA == 0 {
		return result, fmt.Errorf("module and method RVA must be non-empty")
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
	moduleBase, err := waitForModule(process, pid, module, timeout)
	if err != nil {
		return result, err
	}
	result.ModuleBase = fmt.Sprintf("0x%08X", moduleBase)
	method := moduleBase + uintptr(methodRVA)
	result.Method = fmt.Sprintf("0x%08X", method)

	filename := append([]byte(resourcePath), 0)
	filenameArea := uintptr((len(filename) + 0xfff) &^ 0xfff)
	outputArea := uintptr((expandedSize + 1 + 0xfff) &^ 0xfff)
	allocationSize := uintptr(0x1000) + filenameArea + outputArea + 0x1000
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, allocationSize, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xffffffff {
		return result, fmt.Errorf("VirtualAllocEx Client file decode: 0x%X (%v)", page, allocErr)
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
	stub := buildClientFileDecodeStub(method, thisAddress, filenameAddress, outputAddress, statusAddress, expandedSize)
	if err := writeRemote(process, page, stub); err != nil {
		return result, fmt.Errorf("write Client file decode stub: %w", err)
	}
	if err := writeRemote(process, filenameAddress, filename); err != nil {
		return result, fmt.Errorf("write Client file path: %w", err)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))

	freeAllocation = false
	if _, err := remoteCallOne(process, page, 0, timeout); err != nil {
		return result, fmt.Errorf("run Client file decode stub: %w", err)
	}
	freeAllocation = true
	status, ok := readRemote(process, statusAddress, 4)
	if !ok || len(status) != 4 {
		return result, fmt.Errorf("read Client file decode status")
	}
	result.Succeeded = binary.LittleEndian.Uint32(status) != 0
	if !result.Succeeded {
		return result, nil
	}
	decoded, ok := readRemote(process, outputAddress, expandedSize)
	if !ok || len(decoded) != expandedSize {
		return result, fmt.Errorf("read decoded Client file")
	}
	result.DecodedOutput = decoded
	return result, nil
}

func buildClientFileDecodeStub(method, thisAddress, filenameAddress, outputAddress, statusAddress uintptr, expandedSize int) []byte {
	stub := []byte{0xB9}
	stub = binary.LittleEndian.AppendUint32(stub, uint32(thisAddress))
	stub = appendPushImmediate(stub, outputAddress)
	stub = appendPushImmediate(stub, uintptr(expandedSize))
	stub = appendPushImmediate(stub, filenameAddress)
	stub = append(stub, 0xB8)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(method))
	stub = append(stub, 0xFF, 0xD0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(statusAddress))
	stub = append(stub, 0x31, 0xC0, 0xC2, 0x04, 0x00)
	return stub
}
