package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

const (
	shopCatalogCompletionRVA = uintptr(0xC296)
	shopCatalogInstallerRVA  = uintptr(0x2068)
	shopCatalogHookSize      = 5
	shopCatalogRecordSize    = 96
)

type ShopCatalogCallTrace struct {
	Calls      uint32 `json:"calls"`
	ThreadID   uint32 `json:"thread_id"`
	Argument   string `json:"argument"`
	Result     uint32 `json:"result,omitempty"`
	Count      uint32 `json:"count"`
	Records    string `json:"records"`
	FirstBytes string `json:"first_record_hex,omitempty"`
}

type ShopCatalogTraceResult struct {
	PID        uint32               `json:"pid"`
	ModuleBase string               `json:"module_base"`
	Completion ShopCatalogCallTrace `json:"completion"`
	Installer  ShopCatalogCallTrace `json:"installer"`
}

// TraceShopCatalogLoad records the native parse-completion structure and the
// catalog installer arguments. Both hooks replay the displaced instructions
// byte-for-byte and are removed before this function returns.
func TraceShopCatalogLoad(pid uint32, timeout time.Duration) (ShopCatalogTraceResult, error) {
	if pid == 0 {
		return ShopCatalogTraceResult{}, fmt.Errorf("pid must be non-zero")
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return ShopCatalogTraceResult{}, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)
	base, err := waitForModule(process, pid, "QQTShop2ND.dll", timeout)
	if err != nil {
		return ShopCatalogTraceResult{}, err
	}
	completionAddress := base + shopCatalogCompletionRVA
	installerAddress := base + shopCatalogInstallerRVA
	completionOriginal, _ := readRemote(process, completionAddress, shopCatalogHookSize)
	installerOriginal, _ := readRemote(process, installerAddress, shopCatalogHookSize)
	if len(completionOriginal) != shopCatalogHookSize || len(installerOriginal) != shopCatalogHookSize {
		return ShopCatalogTraceResult{}, fmt.Errorf("read shop catalog hook signatures")
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xffffffff {
		return ShopCatalogTraceResult{}, fmt.Errorf("VirtualAllocEx shop catalog trace: 0x%X (%v)", page, allocErr)
	}
	defer procVirtualFreeEx.Call(uintptr(process), page, 0, memRelease)
	completionRecord := page + 0x400
	installerRecord := page + 0x500
	completionStub := buildShopCatalogTraceStub(page, completionRecord, completionAddress, completionOriginal, true)
	installerStub := buildShopCatalogTraceStub(page+0x200, installerRecord, installerAddress, installerOriginal, false)
	if err := writeRemote(process, page, completionStub); err != nil {
		return ShopCatalogTraceResult{}, err
	}
	if err := writeRemote(process, page+0x200, installerStub); err != nil {
		return ShopCatalogTraceResult{}, err
	}
	installedCompletion := false
	installedInstaller := false
	defer func() {
		if installedCompletion {
			ensureRemoteBytes(process, completionAddress, completionOriginal, pageExecuteReadWrite)
			procFlushInstruction.Call(uintptr(process), completionAddress, shopCatalogHookSize)
		}
		if installedInstaller {
			ensureRemoteBytes(process, installerAddress, installerOriginal, pageExecuteReadWrite)
			procFlushInstruction.Call(uintptr(process), installerAddress, shopCatalogHookSize)
		}
	}()
	if err := patchShopDealTraceEntry(process, completionAddress, page, shopCatalogHookSize); err != nil {
		return ShopCatalogTraceResult{}, err
	}
	installedCompletion = true
	if err := patchShopDealTraceEntry(process, installerAddress, page+0x200, shopCatalogHookSize); err != nil {
		return ShopCatalogTraceResult{}, err
	}
	installedInstaller = true
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, _ := readRemote(process, completionRecord, 4)
		if len(data) == 4 && binary.LittleEndian.Uint32(data) != 0 {
			time.Sleep(100 * time.Millisecond)
			return ShopCatalogTraceResult{
				PID: pid, ModuleBase: fmt.Sprintf("0x%08X", base),
				Completion: decodeShopCatalogCallTrace(readRemoteBytes(process, completionRecord, shopCatalogRecordSize), true),
				Installer:  decodeShopCatalogCallTrace(readRemoteBytes(process, installerRecord, shopCatalogRecordSize), false),
			}, nil
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(process), 0)
		if waitResult == waitObject0 {
			return ShopCatalogTraceResult{}, fmt.Errorf("Client.exe exited before the shop catalog loaded")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return ShopCatalogTraceResult{}, fmt.Errorf("shop catalog trace timed out after %s", timeout)
}

func readRemoteBytes(process syscall.Handle, address uintptr, size int) []byte {
	data, _ := readRemote(process, address, size)
	return data
}

func buildShopCatalogTraceStub(stub, record, resume uintptr, original []byte, completion bool) []byte {
	code := []byte{0x9C, 0x60, 0xF0, 0xFF, 0x05} // pushfd; pushad; lock inc [record]
	code = binary.LittleEndian.AppendUint32(code, uint32(record))
	code = append(code, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3) // thread ID
	code = binary.LittleEndian.AppendUint32(code, uint32(record+4))
	code = append(code, 0x8B, 0x54, 0x24, 0x28, 0x89, 0x15) // first argument
	code = binary.LittleEndian.AppendUint32(code, uint32(record+8))
	if completion {
		code = append(code, 0x85, 0xD2, 0x0F, 0x84, 0, 0, 0, 0) // test edx; jz done
		nullJump := len(code) - 4
		for source, target := range map[byte]uintptr{0: 12, 4: 16, 8: 20} {
			code = append(code, 0x8B, 0x42, source, 0xA3)
			code = binary.LittleEndian.AppendUint32(code, uint32(record+target))
		}
		code = appendCopyFirstRecord(code, record)
		binary.LittleEndian.PutUint32(code[nullJump:nullJump+4], uint32(len(code)-(nullJump+4)))
	} else {
		code = append(code, 0x8B, 0x44, 0x24, 0x2C, 0xA3) // records pointer
		code = binary.LittleEndian.AppendUint32(code, uint32(record+20))
		code = append(code, 0x8B, 0x44, 0x24, 0x28, 0xA3) // count
		code = binary.LittleEndian.AppendUint32(code, uint32(record+16))
		code = appendCopyFirstRecord(code, record)
	}
	code = append(code, 0x61, 0x9D) // popad; popfd
	code = append(code, original...)
	return appendRelativeJump(code, stub+uintptr(len(code)), resume+uintptr(len(original)))
}

func appendCopyFirstRecord(code []byte, record uintptr) []byte {
	code = append(code, 0xA1)
	code = binary.LittleEndian.AppendUint32(code, uint32(record+20)) // mov eax,[records]
	code = append(code, 0x85, 0xC0, 0x0F, 0x84, 0, 0, 0, 0)
	nullJump := len(code) - 4
	code = append(code, 0x8B, 0xF0, 0xBF)
	code = binary.LittleEndian.AppendUint32(code, uint32(record+32))
	code = append(code, 0xB9, 0x10, 0, 0, 0, 0xF3, 0xA5) // 64-byte copy
	binary.LittleEndian.PutUint32(code[nullJump:nullJump+4], uint32(len(code)-(nullJump+4)))
	return code
}

func decodeShopCatalogCallTrace(data []byte, completion bool) ShopCatalogCallTrace {
	if len(data) < shopCatalogRecordSize {
		return ShopCatalogCallTrace{}
	}
	value := func(offset int) uint32 { return binary.LittleEndian.Uint32(data[offset : offset+4]) }
	result := ShopCatalogCallTrace{
		Calls: value(0), ThreadID: value(4), Argument: fmt.Sprintf("0x%08X", value(8)),
		Count: value(16), Records: fmt.Sprintf("0x%08X", value(20)), FirstBytes: fmt.Sprintf("%X", data[32:96]),
	}
	if completion {
		result.Result = value(12)
	}
	return result
}
