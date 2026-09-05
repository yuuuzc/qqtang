package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

const (
	clientInitializeShopRVA = uintptr(0x169E00)
	clientEnterShopRVA      = uintptr(0x16A7D0)
)

var (
	clientInitializeShopSignature = []byte{0x55, 0x8B, 0xEC, 0x83, 0xEC, 0x40}
	clientEnterShopSignature      = []byte{0x55, 0x8B, 0xEC, 0x83, 0xEC, 0x4C}
)

type ShopFlowCapture struct {
	PID             uint32 `json:"pid"`
	RandomIDCalls   uint32 `json:"random_server_id_calls"`
	RandomIDSource  string `json:"random_server_id_source"`
	InitializeCalls uint32 `json:"initialize_shop_calls"`
	EnterCalls      uint32 `json:"enter_shop_calls"`
	ServerID        uint32 `json:"server_id"`
	DirectoryObject string `json:"directory_object,omitempty"`
	RandomIDPatch   string `json:"random_server_id_patch"`
	InitializePatch string `json:"initialize_shop_patch"`
	EnterPatch      string `json:"enter_shop_patch"`
	Behavior        string `json:"behavior"`
}

type shopFlowHook struct {
	address  uintptr
	original []byte
}

// ShopFlowTracer transparently counts the two native Python bindings. On an
// unprepared client it may install a temporary selector override; on the
// prepared client it recognizes and preserves the build-time static selector.
// It is diagnostic only; Close restores every entry point it changed.
type ShopFlowTracer struct {
	pid              uint32
	serverID         uint32
	process          syscall.Handle
	page             uintptr
	record           uintptr
	directory        uintptr
	randomRecord     uintptr
	randomBaseline   uint32
	randomPrepatched bool
	randomStatic     bool
	random           shopFlowHook
	init             shopFlowHook
	enter            shopFlowHook
}

func InstallShopFlowTracer(pid, serverID uint32, timeout time.Duration) (*ShopFlowTracer, error) {
	if pid == 0 || serverID == 0 {
		return nil, fmt.Errorf("pid and server ID must be non-zero")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &ShopFlowTracer{pid: pid, serverID: serverID, process: process}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()
	clientBase, err := waitForModule(process, pid, "Client.exe", timeout)
	if err != nil {
		return nil, err
	}
	directoryBase, err := waitForModule(process, pid, "QQTDir.dll", timeout)
	if err != nil {
		return nil, err
	}
	tracer.random = shopFlowHook{address: directoryBase + qqtDirRandomShopServerMethodRVA}
	tracer.init = shopFlowHook{address: clientBase + clientInitializeShopRVA}
	tracer.enter = shopFlowHook{address: clientBase + clientEnterShopRVA}
	checks := []struct {
		name      string
		hook      *shopFlowHook
		signature []byte
	}{
		{"InitializeShop", &tracer.init, clientInitializeShopSignature},
		{"EnterShop", &tracer.enter, clientEnterShopSignature},
	}
	for _, check := range checks {
		check.hook.original, _ = readRemote(process, check.hook.address, len(check.signature))
		if len(check.hook.original) != len(check.signature) || !equalBytes(check.hook.original, check.signature) {
			return nil, fmt.Errorf("%s signature mismatch at 0x%08X: got %X", check.name, check.hook.address, check.hook.original)
		}
	}
	tracer.random.original, _ = readRemote(process, tracer.random.address, len(qqtDirRandomShopServerMethodSignature))
	if !equalBytes(tracer.random.original, qqtDirRandomShopServerMethodSignature) {
		entry := tracer.random.original
		staticPrefix := []byte{0x8B, 0x44, 0x24, 0x08, 0xC7, 0x00}
		if equalBytes(entry, staticPrefix) {
			// Prepared-client selector: mov eax,[esp+8]; mov [eax],id;
			// xor eax,eax; ret 8. It has no runtime counter and must remain
			// untouched by this diagnostic.
			full, ok := readRemote(process, tracer.random.address, 16)
			expected := []byte{0x8B, 0x44, 0x24, 0x08, 0xC7, 0x00, 0, 0, 0, 0, 0x33, 0xC0, 0xC2, 0x08, 0x00}
			binary.LittleEndian.PutUint32(expected[6:10], serverID)
			if !ok || !equalBytes(full, expected) {
				return nil, fmt.Errorf("GetRandShopServerID static selector mismatch at 0x%08X: got %X", tracer.random.address, full)
			}
			tracer.randomPrepatched = true
			tracer.randomStatic = true
			tracer.random.original = nil
		} else {
			// Legacy per-process compatibility stub. Reuse its built-in
			// record instead of overwriting it while old diagnostics run.
			if len(entry) != 6 || entry[0] != 0xE9 || entry[5] != 0x90 {
				return nil, fmt.Errorf("GetRandShopServerID signature mismatch at 0x%08X: got %X", tracer.random.address, entry)
			}
			target := tracer.random.address + 5 + uintptr(int64(int32(binary.LittleEndian.Uint32(entry[1:5]))))
			stub, ok := readRemote(process, target, 7)
			if !ok || len(stub) != 7 || stub[0] != 0xF0 || stub[1] != 0xFF || stub[2] != 0x05 {
				return nil, fmt.Errorf("GetRandShopServerID existing target at 0x%08X is not the compatibility stub: got %X", target, stub)
			}
			tracer.randomRecord = uintptr(binary.LittleEndian.Uint32(stub[3:7]))
			baseline, ok := readRemote(process, tracer.randomRecord, 4)
			if !ok {
				return nil, fmt.Errorf("read existing shop-server counter at 0x%08X", tracer.randomRecord)
			}
			tracer.randomBaseline = binary.LittleEndian.Uint32(baseline)
			tracer.randomPrepatched = true
			tracer.random.original = nil
		}
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx shop-flow trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page = page
	tracer.record = page + 0x300
	tracer.directory = tracer.record + 0x0C

	if !tracer.randomPrepatched {
		randomStub := buildShopRandomServerStub(tracer.record, tracer.directory, serverID)
		if err := writeRemote(process, page, randomStub); err != nil {
			return nil, fmt.Errorf("write random-server trace stub: %w", err)
		}
	}
	initStubAddress := page + 0x80
	initStub := buildTransparentShopEntryStub(initStubAddress, tracer.init, tracer.record+4)
	if err := writeRemote(process, initStubAddress, initStub); err != nil {
		return nil, fmt.Errorf("write InitializeShop trace stub: %w", err)
	}
	enterStubAddress := page + 0x100
	enterStub := buildTransparentShopEntryStub(enterStubAddress, tracer.enter, tracer.record+8)
	if err := writeRemote(process, enterStubAddress, enterStub); err != nil {
		return nil, fmt.Errorf("write EnterShop trace stub: %w", err)
	}
	if !tracer.randomPrepatched {
		if err := patchShopFlowEntry(process, tracer.random.address, page); err != nil {
			return nil, err
		}
	}
	if err := patchShopFlowEntry(process, tracer.init.address, initStubAddress); err != nil {
		return nil, err
	}
	if err := patchShopFlowEntry(process, tracer.enter.address, enterStubAddress); err != nil {
		return nil, err
	}
	failed = false
	return tracer, nil
}

func buildShopRandomServerStub(counter, directory uintptr, serverID uint32) []byte {
	stub := []byte{0xF0, 0xFF, 0x05} // lock inc dword ptr [counter]
	stub = binary.LittleEndian.AppendUint32(stub, uint32(counter))
	stub = append(stub, 0x8B, 0x44, 0x24, 0x04, 0xA3) // this is the first stack argument
	stub = binary.LittleEndian.AppendUint32(stub, uint32(directory))
	stub = append(stub, 0x8B, 0x44, 0x24, 0x08, 0xC7, 0x00) // second argument is uint32 *outID
	stub = binary.LittleEndian.AppendUint32(stub, serverID)
	stub = append(stub, 0x33, 0xC0, 0xC2, 0x08, 0x00) // status success; callee removes both arguments
	return stub
}

// RepairActiveShopFlowRandomStub upgrades the first diagnostic stub revision
// in place. That revision returned the chosen ID as a status code; the native
// caller correctly interpreted it as failure and discarded the output. This
// helper is narrowly signature-gated so it cannot patch an unrelated page.
func RepairActiveShopFlowRandomStub(pid uint32, stubAddress uintptr, serverID uint32) error {
	if pid == 0 || stubAddress == 0 || stubAddress > 0xFFFFFFFF || serverID == 0 {
		return fmt.Errorf("pid, 32-bit stub address and server ID must be non-zero")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)
	old, ok := readRemote(process, stubAddress, 19)
	if !ok || len(old) != 19 || old[0] != 0xF0 || old[1] != 0xFF || old[2] != 0x05 ||
		old[7] != 0x89 || old[8] != 0x0D || old[13] != 0xB8 || old[18] != 0xC3 {
		return fmt.Errorf("active shop-flow random stub signature mismatch at 0x%08X: got %X", stubAddress, old)
	}
	counter := uintptr(binary.LittleEndian.Uint32(old[3:7]))
	directory := uintptr(binary.LittleEndian.Uint32(old[9:13]))
	stub := buildShopRandomServerStub(counter, directory, serverID)
	if !ensureRemoteBytes(process, stubAddress, stub, pageExecuteReadWrite) {
		return fmt.Errorf("repair active shop-flow random stub at 0x%08X", stubAddress)
	}
	procFlushInstruction.Call(uintptr(process), stubAddress, uintptr(len(stub)))
	return nil
}

func buildTransparentShopEntryStub(stubAddress uintptr, hook shopFlowHook, counter uintptr) []byte {
	stub := []byte{0x9C, 0x60, 0xF0, 0xFF, 0x05} // pushfd; pushad; lock inc [counter]
	stub = binary.LittleEndian.AppendUint32(stub, uint32(counter))
	stub = append(stub, 0x61, 0x9D) // popad; popfd
	stub = append(stub, hook.original...)
	return appendRelativeJump(stub, stubAddress+uintptr(len(stub)), hook.address+uintptr(len(hook.original)))
}

func patchShopFlowEntry(process syscall.Handle, address, stub uintptr) error {
	patch := []byte{0xE9, 0, 0, 0, 0, 0x90}
	binary.LittleEndian.PutUint32(patch[1:5], uint32(stub-(address+5)))
	if !ensureRemoteBytes(process, address, patch, pageExecuteReadWrite) {
		return fmt.Errorf("patch shop-flow entry at 0x%08X", address)
	}
	procFlushInstruction.Call(uintptr(process), address, uintptr(len(patch)))
	return nil
}

func (tracer *ShopFlowTracer) CaptureUntilEnter(duration time.Duration) ShopFlowCapture {
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		record, ok := readRemote(tracer.process, tracer.record, 16)
		if ok && binary.LittleEndian.Uint32(record[8:12]) != 0 {
			return tracer.capture(record)
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	record, _ := readRemote(tracer.process, tracer.record, 16)
	return tracer.capture(record)
}

func (tracer *ShopFlowTracer) capture(record []byte) ShopFlowCapture {
	result := ShopFlowCapture{
		PID: tracer.pid, ServerID: tracer.serverID,
		RandomIDPatch:   fmt.Sprintf("0x%08X", tracer.random.address),
		InitializePatch: fmt.Sprintf("0x%08X", tracer.init.address),
		EnterPatch:      fmt.Sprintf("0x%08X", tracer.enter.address),
		Behavior:        "temporary local shop gate plus transparent InitializeShop/EnterShop call counters; restore all original bytes on close",
	}
	if tracer.randomStatic {
		result.RandomIDSource = "static_prepared_client"
		result.Behavior = "prepared-client static shop selector plus transparent InitializeShop/EnterShop call counters; restore only diagnostic hooks"
	} else if tracer.randomPrepatched {
		result.RandomIDSource = "existing_shop_connect_compatibility_counter"
		compatRecord, ok := readRemote(tracer.process, tracer.randomRecord, 16)
		if ok {
			result.RandomIDCalls = binary.LittleEndian.Uint32(compatRecord[:4]) - tracer.randomBaseline
			result.DirectoryObject = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(compatRecord[12:16]))
		}
	} else {
		result.RandomIDSource = "temporary_trace_counter"
	}
	if len(record) == 16 {
		if !tracer.randomPrepatched {
			result.RandomIDCalls = binary.LittleEndian.Uint32(record[:4])
			result.DirectoryObject = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[12:16]))
		}
		result.InitializeCalls = binary.LittleEndian.Uint32(record[4:8])
		result.EnterCalls = binary.LittleEndian.Uint32(record[8:12])
	}
	return result
}

func (tracer *ShopFlowTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	for _, hook := range []shopFlowHook{tracer.random, tracer.init, tracer.enter} {
		if hook.address != 0 && len(hook.original) != 0 {
			ensureRemoteBytes(tracer.process, hook.address, hook.original, pageExecuteReadWrite)
			procFlushInstruction.Call(uintptr(tracer.process), hook.address, uintptr(len(hook.original)))
		}
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}
