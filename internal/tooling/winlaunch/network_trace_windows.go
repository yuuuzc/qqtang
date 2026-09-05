package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

const attachedProcessAccess = 0x100000 | 0x0400 | 0x0010 | 0x0020 | 0x0008

type LoginNetworkCall struct {
	ElapsedMS      int64  `json:"elapsed_ms"`
	API            string `json:"api"`
	Module         string `json:"module"`
	Library        string `json:"library"`
	CallsObserved  uint32 `json:"calls_observed"`
	Caller         string `json:"caller"`
	ParentCaller   string `json:"parent_caller,omitempty"`
	ContextPointer string `json:"context_pointer,omitempty"`
	SavedFrame     string `json:"saved_frame,omitempty"`
	ThreadID       uint32 `json:"thread_id"`
	Hostname       string `json:"hostname,omitempty"`
	Socket         uint32 `json:"socket,omitempty"`
	AddressPointer string `json:"address_pointer,omitempty"`
	AddressLength  uint32 `json:"address_length,omitempty"`
	Family         uint16 `json:"family,omitempty"`
	Port           uint16 `json:"port,omitempty"`
	IPv4           string `json:"ipv4,omitempty"`
	Result         int32  `json:"result"`
	WSAError       int32  `json:"wsa_error,omitempty"`
	RedirectIPv4   string `json:"redirect_ipv4,omitempty"`
	RedirectPort   uint16 `json:"redirect_port,omitempty"`
}

type LoginNetworkCapture struct {
	PID       uint32             `json:"pid"`
	ElapsedMS int64              `json:"elapsed_ms"`
	Calls     []LoginNetworkCall `json:"calls"`
}

type loginNetworkStub struct {
	module       string
	library      string
	api          string
	iatAddress   uintptr
	originalIAT  []byte
	record       uintptr
	recordSize   int
	lastObserved uint32
}

type LoginNetworkTracer struct {
	pid      uint32
	process  syscall.Handle
	started  time.Time
	stubs    []loginNetworkStub
	redirect *LoginNetworkRedirect
}

type LoginNetworkRedirect struct {
	IPv4 [4]byte
	Port uint16
}

func InstallLoginNetworkTracer(pid uint32, timeout time.Duration) (*LoginNetworkTracer, error) {
	return installLoginNetworkTracer(pid, timeout, nil)
}

func InstallLoginNetworkRedirectTracer(pid uint32, timeout time.Duration, redirect LoginNetworkRedirect) (*LoginNetworkTracer, error) {
	if redirect.Port == 0 {
		return nil, fmt.Errorf("redirect port must be non-zero")
	}
	return installLoginNetworkTracer(pid, timeout, &redirect)
}

// InstallSSONetworkRedirectTracer waits specifically for the lazily loaded
// SSOCommon.dll and then wraps only that module's Winsock imports. This avoids
// racing the first user login click while keeping the redirect scoped to the
// retired TXSSO component.
func InstallSSONetworkRedirectTracer(pid uint32, timeout time.Duration, redirect LoginNetworkRedirect) (*LoginNetworkTracer, error) {
	if redirect.Port == 0 {
		return nil, fmt.Errorf("redirect port must be non-zero")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &LoginNetworkTracer{pid: pid, process: process, started: time.Now(), redirect: &redirect}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()
	moduleBase, err := waitForModule(process, pid, "SSOCommon.dll", timeout)
	if err != nil {
		return nil, err
	}
	var installErrors []string
	for _, libraryName := range []string{"WS2_32.dll", "WSOCK32.dll"} {
		connectStub, stubErr := installAttachedConnectTrace(process, pid, "SSOCommon.dll", moduleBase, libraryName, timeout, &redirect)
		if stubErr == nil {
			tracer.stubs = append(tracer.stubs, connectStub)
		} else {
			installErrors = append(installErrors, fmt.Sprintf("%s connect: %v", libraryName, stubErr))
		}
		hostStub, hostErr := installAttachedGetHostByNameTrace(process, pid, "SSOCommon.dll", moduleBase, libraryName, timeout)
		if hostErr == nil {
			tracer.stubs = append(tracer.stubs, hostStub)
		} else {
			installErrors = append(installErrors, fmt.Sprintf("%s gethostbyname: %v", libraryName, hostErr))
		}
	}
	if len(tracer.stubs) == 0 {
		return nil, fmt.Errorf("no SSOCommon Winsock imports could be traced: %v", installErrors)
	}
	failed = false
	return tracer, nil
}

func installLoginNetworkTracer(pid uint32, timeout time.Duration, redirect *LoginNetworkRedirect) (*LoginNetworkTracer, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &LoginNetworkTracer{pid: pid, process: process, started: time.Now(), redirect: redirect}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()

	// The login button crosses several old component boundaries. In particular,
	// ClientBase and TXSSO's SSOCommon may own the actual connect call even when
	// the visible UI and higher-level request originate in TenQQt. Patch only the
	// IAT slots of these known, in-process game components.
	moduleNames := []string{
		"ClientBase.dll",
		"SSOCommon.dll",
		"TenQQt.dll",
		"TenSLX.dll",
		"NetCenter.dll",
		"Core.dll",
		"QQTDir.dll",
		"QQTDownloadCenter.dll",
		"QQTHelp.dll",
		"QQTModules.dll",
	}
	probeTimeout := timeout
	if probeTimeout > 250*time.Millisecond {
		probeTimeout = 250 * time.Millisecond
	}
	var installErrors []string
	for _, moduleName := range moduleNames {
		moduleBase, moduleErr := waitForModule(process, pid, moduleName, probeTimeout)
		if moduleErr != nil {
			installErrors = append(installErrors, moduleErr.Error())
			continue
		}
		for _, libraryName := range []string{"WS2_32.dll", "WSOCK32.dll"} {
			connectStub, stubErr := installAttachedConnectTrace(process, pid, moduleName, moduleBase, libraryName, timeout, redirect)
			if stubErr == nil {
				tracer.stubs = append(tracer.stubs, connectStub)
			} else {
				installErrors = append(installErrors, fmt.Sprintf("%s %s connect: %v", moduleName, libraryName, stubErr))
			}
			hostStub, hostErr := installAttachedGetHostByNameTrace(process, pid, moduleName, moduleBase, libraryName, timeout)
			if hostErr == nil {
				tracer.stubs = append(tracer.stubs, hostStub)
			} else {
				installErrors = append(installErrors, fmt.Sprintf("%s %s gethostbyname: %v", moduleName, libraryName, hostErr))
			}
		}
	}
	if len(tracer.stubs) == 0 {
		return nil, fmt.Errorf("no game-component Winsock imports could be traced: %v", installErrors)
	}
	failed = false
	return tracer, nil
}

func (tracer *LoginNetworkTracer) Capture(duration time.Duration) LoginNetworkCapture {
	deadline := time.Now().Add(duration)
	result := LoginNetworkCapture{PID: tracer.pid}
	for time.Now().Before(deadline) {
		for index := range tracer.stubs {
			stub := &tracer.stubs[index]
			record, ok := readRemote(tracer.process, stub.record, stub.recordSize)
			if !ok {
				continue
			}
			calls := binary.LittleEndian.Uint32(record[0:4])
			if calls == 0 || calls == stub.lastObserved {
				continue
			}
			stub.lastObserved = calls
			call := LoginNetworkCall{
				ElapsedMS: time.Since(tracer.started).Milliseconds(), API: stub.api, Module: stub.module, Library: stub.library,
				CallsObserved: calls, Caller: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[4:8])),
				ThreadID: binary.LittleEndian.Uint32(record[8:12]),
			}
			switch stub.api {
			case "connect":
				call.Socket = binary.LittleEndian.Uint32(record[12:16])
				addressPointer := binary.LittleEndian.Uint32(record[16:20])
				call.AddressPointer = fmt.Sprintf("0x%08X", addressPointer)
				call.AddressLength = binary.LittleEndian.Uint32(record[20:24])
				call.Family = uint16(binary.LittleEndian.Uint32(record[24:28]))
				rawPort := uint16(binary.LittleEndian.Uint32(record[28:32]))
				call.Port = rawPort<<8 | rawPort>>8
				rawAddress := binary.LittleEndian.Uint32(record[32:36])
				call.IPv4 = fmt.Sprintf("%d.%d.%d.%d", byte(rawAddress), byte(rawAddress>>8), byte(rawAddress>>16), byte(rawAddress>>24))
				call.Result = int32(binary.LittleEndian.Uint32(record[36:40]))
				call.WSAError = int32(binary.LittleEndian.Uint32(record[40:44]))
				call.ContextPointer = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[44:48]))
				call.ParentCaller = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[48:52]))
				call.SavedFrame = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[52:56]))
				if tracer.redirect != nil && equalFoldASCII(stub.module, "SSOCommon.dll") {
					call.RedirectIPv4 = fmt.Sprintf("%d.%d.%d.%d", tracer.redirect.IPv4[0], tracer.redirect.IPv4[1], tracer.redirect.IPv4[2], tracer.redirect.IPv4[3])
					call.RedirectPort = tracer.redirect.Port
				}
			case "gethostbyname":
				for end := 24; end < len(record); end++ {
					if record[end] == 0 {
						call.Hostname = string(record[24:end])
						break
					}
				}
				call.Result = int32(binary.LittleEndian.Uint32(record[16:20]))
			}
			result.Calls = append(result.Calls, call)
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	result.ElapsedMS = time.Since(tracer.started).Milliseconds()
	return result
}

func (tracer *LoginNetworkTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	for index := len(tracer.stubs) - 1; index >= 0; index-- {
		stub := tracer.stubs[index]
		ensureRemoteBytes(tracer.process, stub.iatAddress, stub.originalIAT, pageReadWrite)
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}

func installAttachedConnectTrace(process syscall.Handle, pid uint32, moduleName string, moduleBase uintptr, libraryName string, timeout time.Duration, redirect *LoginNetworkRedirect) (loginNetworkStub, error) {
	iatAddress, targetAddress, err := findAttachedImportAddress(process, pid, moduleBase, libraryName, "connect", timeout)
	if err != nil {
		return loginNetworkStub{}, err
	}
	getLastError, err := findRemoteExport(process, pid, "WS2_32.dll", "WSAGetLastError", timeout, 0)
	if err != nil {
		return loginNetworkStub{}, err
	}
	setLastError, err := findRemoteExport(process, pid, "WS2_32.dll", "WSASetLastError", timeout, 0)
	if err != nil {
		return loginNetworkStub{}, err
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return loginNetworkStub{}, fmt.Errorf("VirtualAllocEx %s connect trace: 0x%X (%v)", moduleName, page, allocErr)
	}
	recordAddress := page + 0x300
	stub := []byte{0x53, 0x56, 0x57, 0x8D, 0x74, 0x24, 0x0C} // save EBX/ESI/EDI; ESI=original ESP
	appendAbsoluteIncrement := func(address uintptr) {
		stub = append(stub, 0xF0, 0xFF, 0x05)
		stub = binary.LittleEndian.AppendUint32(stub, uint32(address))
	}
	appendEAXStore := func(address uintptr) {
		stub = append(stub, 0xA3)
		stub = binary.LittleEndian.AppendUint32(stub, uint32(address))
	}
	appendECXStore := func(address uintptr) {
		stub = append(stub, 0x89, 0x0D)
		stub = binary.LittleEndian.AppendUint32(stub, uint32(address))
	}
	appendAbsoluteIncrement(recordAddress)
	stub = append(stub, 0x8B, 0x06)
	appendEAXStore(recordAddress + 4)
	stub = append(stub, 0x8B, 0xC1)
	appendEAXStore(recordAddress + 44)
	stub = append(stub, 0x8B, 0x46, 0x14)
	appendEAXStore(recordAddress + 48)
	stub = append(stub, 0x8B, 0x46, 0x10)
	appendEAXStore(recordAddress + 52)
	stub = append(stub, 0x64, 0xA1, 0x24, 0x00, 0x00, 0x00)
	appendEAXStore(recordAddress + 8)
	stub = append(stub, 0x8B, 0x46, 0x04)
	appendEAXStore(recordAddress + 12)
	stub = append(stub, 0x8B, 0x4E, 0x08)
	appendECXStore(recordAddress + 16)
	stub = append(stub, 0x8B, 0x46, 0x0C)
	appendEAXStore(recordAddress + 20)
	stub = append(stub, 0x0F, 0xB7, 0x01)
	appendEAXStore(recordAddress + 24)
	stub = append(stub, 0x0F, 0xB7, 0x41, 0x02)
	appendEAXStore(recordAddress + 28)
	stub = append(stub, 0x8B, 0x41, 0x04)
	appendEAXStore(recordAddress + 32)
	if redirect != nil && equalFoldASCII(moduleName, "SSOCommon.dll") {
		// Rewrite only SSOCommon's caller-owned sockaddr after recording the
		// original endpoint. The wrapper constructs this sockaddr on its stack,
		// so the change cannot leak into other components or future calls.
		stub = append(stub, 0x66, 0xC7, 0x41, 0x02)
		networkPort := redirect.Port<<8 | redirect.Port>>8
		stub = binary.LittleEndian.AppendUint16(stub, networkPort)
		stub = append(stub, 0xC7, 0x41, 0x04)
		stub = binary.LittleEndian.AppendUint32(stub, binary.LittleEndian.Uint32(redirect.IPv4[:]))
	}
	stub = append(stub, 0xFF, 0x76, 0x0C, 0xFF, 0x76, 0x08, 0xFF, 0x76, 0x04, 0xB8)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(targetAddress))
	stub = append(stub, 0xFF, 0xD0, 0x8B, 0xD8, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordAddress+36))
	stub = append(stub, 0xB8)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(getLastError))
	stub = append(stub, 0xFF, 0xD0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordAddress+40))
	stub = append(stub, 0x50, 0xB8)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(setLastError))
	stub = append(stub, 0xFF, 0xD0, 0x8B, 0xC3, 0x5F, 0x5E, 0x5B, 0xC2, 0x0C, 0x00)
	if err := writeRemote(process, page, stub); err != nil {
		return loginNetworkStub{}, err
	}
	originalIAT, ok := readRemote(process, iatAddress, 4)
	if !ok {
		return loginNetworkStub{}, fmt.Errorf("read %s connect IAT", moduleName)
	}
	pointer := make([]byte, 4)
	binary.LittleEndian.PutUint32(pointer, uint32(page))
	if !ensureRemoteBytes(process, iatAddress, pointer, pageReadWrite) {
		return loginNetworkStub{}, fmt.Errorf("patch %s connect IAT", moduleName)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	return loginNetworkStub{module: moduleName, library: libraryName, api: "connect", iatAddress: iatAddress, originalIAT: originalIAT, record: recordAddress, recordSize: 56}, nil
}

func installAttachedGetHostByNameTrace(process syscall.Handle, pid uint32, moduleName string, moduleBase uintptr, libraryName string, timeout time.Duration) (loginNetworkStub, error) {
	iatAddress, targetAddress, err := findAttachedImportAddress(process, pid, moduleBase, libraryName, "gethostbyname", timeout)
	if err != nil {
		return loginNetworkStub{}, err
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return loginNetworkStub{}, fmt.Errorf("VirtualAllocEx %s gethostbyname trace: 0x%X (%v)", moduleName, page, allocErr)
	}
	recordAddress := page + 0x200
	stub := []byte{0x53, 0x56, 0x57, 0x8D, 0x74, 0x24, 0x0C, 0xF0, 0xFF, 0x05}
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordAddress))
	stub = append(stub, 0x8B, 0x06, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordAddress+4))
	stub = append(stub, 0x64, 0xA1, 0x24, 0x00, 0x00, 0x00, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordAddress+8))
	stub = append(stub, 0x8B, 0x46, 0x04, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordAddress+12))
	// Copy the hostname while the caller's input pointer is guaranteed valid.
	// The previous tracer retained only the pointer, which old SSO code reused
	// before the 1 ms poll could read it.
	stub = append(stub, 0x8B, 0x56, 0x04, 0xBF)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordAddress+24))
	stub = append(stub, 0xB9, 0xFF, 0x00, 0x00, 0x00)
	copyLoop := len(stub)
	stub = append(stub, 0x8A, 0x02, 0x88, 0x07, 0x42, 0x47, 0x84, 0xC0)
	zeroJump := len(stub)
	stub = append(stub, 0x74, 0x00, 0x49)
	loopJump := len(stub)
	stub = append(stub, 0x75, 0x00, 0xC6, 0x07, 0x00)
	copyDone := len(stub)
	stub[zeroJump+1] = byte(copyDone - (zeroJump + 2))
	stub[loopJump+1] = byte(copyLoop - (loopJump + 2))
	stub = append(stub, 0xFF, 0x76, 0x04, 0xB8)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(targetAddress))
	stub = append(stub, 0xFF, 0xD0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordAddress+16))
	stub = append(stub, 0x5F, 0x5E, 0x5B, 0xC2, 0x04, 0x00)
	if err := writeRemote(process, page, stub); err != nil {
		return loginNetworkStub{}, err
	}
	originalIAT, ok := readRemote(process, iatAddress, 4)
	if !ok {
		return loginNetworkStub{}, fmt.Errorf("read %s gethostbyname IAT", moduleName)
	}
	pointer := make([]byte, 4)
	binary.LittleEndian.PutUint32(pointer, uint32(page))
	if !ensureRemoteBytes(process, iatAddress, pointer, pageReadWrite) {
		return loginNetworkStub{}, fmt.Errorf("patch %s gethostbyname IAT", moduleName)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	return loginNetworkStub{module: moduleName, library: libraryName, api: "gethostbyname", iatAddress: iatAddress, originalIAT: originalIAT, record: recordAddress, recordSize: 280}, nil
}

// findAttachedImportAddress first uses the normal import-name table. Several
// QQTang-era binaries have ordinal or runtime-rewritten lookup thunks, while
// their loader-populated FirstThunk remains valid. The fallback resolves the
// exact export in the target process and matches only that pointer inside the
// named import descriptor's FirstThunk.
func findAttachedImportAddress(process syscall.Handle, pid uint32, moduleBase uintptr, libraryName, symbolName string, timeout time.Duration) (uintptr, uintptr, error) {
	if iatAddress, err := findImportAddress(process, moduleBase, libraryName, symbolName); err == nil {
		targetAddress, pointerErr := waitForExecutablePointer(process, iatAddress, timeout)
		if pointerErr == nil {
			return iatAddress, targetAddress, nil
		}
	}
	targetAddress, err := findRemoteExport(process, pid, libraryName, symbolName, timeout, 0)
	if err != nil {
		return 0, 0, fmt.Errorf("resolve %s!%s: %w", libraryName, symbolName, err)
	}
	iatAddress, err := findImportAddressByValue(process, moduleBase, libraryName, targetAddress)
	if err != nil {
		return 0, 0, err
	}
	return iatAddress, targetAddress, nil
}

func findImportAddressByValue(process syscall.Handle, moduleBase uintptr, libraryName string, targetAddress uintptr) (uintptr, error) {
	header, ok := readRemote(process, moduleBase, 4096)
	if !ok || len(header) < 0x100 || header[0] != 'M' || header[1] != 'Z' {
		return 0, fmt.Errorf("invalid remote PE header")
	}
	peOffset := int(binary.LittleEndian.Uint32(header[0x3C:0x40]))
	optional := peOffset + 24
	if optional+112 > len(header) || binary.LittleEndian.Uint16(header[optional:optional+2]) != 0x10B {
		return 0, fmt.Errorf("remote module is not PE32")
	}
	importRVA := binary.LittleEndian.Uint32(header[optional+104 : optional+108])
	if importRVA == 0 {
		return 0, fmt.Errorf("remote module has no import directory")
	}
	for descriptorIndex := 0; descriptorIndex < 512; descriptorIndex++ {
		descriptor, descriptorOK := readRemote(process, moduleBase+uintptr(importRVA)+uintptr(descriptorIndex*20), 20)
		if !descriptorOK {
			return 0, fmt.Errorf("read import descriptor %d", descriptorIndex)
		}
		originalThunk := binary.LittleEndian.Uint32(descriptor[0:4])
		nameRVA := binary.LittleEndian.Uint32(descriptor[12:16])
		firstThunk := binary.LittleEndian.Uint32(descriptor[16:20])
		if originalThunk == 0 && nameRVA == 0 && firstThunk == 0 {
			break
		}
		remoteLibrary, nameOK := readCString(process, moduleBase+uintptr(nameRVA), 512)
		if !nameOK || !equalFoldASCII(remoteLibrary, libraryName) {
			continue
		}
		for index := 0; index < 16384; index++ {
			entryAddress := moduleBase + uintptr(firstThunk) + uintptr(index*4)
			entry, entryOK := readRemote(process, entryAddress, 4)
			if !entryOK {
				return 0, fmt.Errorf("read first thunk %d", index)
			}
			value := binary.LittleEndian.Uint32(entry)
			if value == 0 {
				break
			}
			if uintptr(value) == targetAddress {
				return entryAddress, nil
			}
		}
		return 0, fmt.Errorf("target 0x%08X not found in %s FirstThunk", targetAddress, libraryName)
	}
	return 0, fmt.Errorf("library %s not found in imports", libraryName)
}

func equalFoldASCII(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		leftByte, rightByte := left[index], right[index]
		if leftByte >= 'a' && leftByte <= 'z' {
			leftByte -= 'a' - 'A'
		}
		if rightByte >= 'a' && rightByte <= 'z' {
			rightByte -= 'a' - 'A'
		}
		if leftByte != rightByte {
			return false
		}
	}
	return true
}
