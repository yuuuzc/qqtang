package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

const (
	netCenterDealSuccessPatchSize    = 12
	netCenterDealFailurePatchSize    = 8
	netCenterSessionSuccessPatchSize = 12
	netCenterSessionFailurePatchSize = 8
)

var (
	netCenterDealSuccessSignature    = []byte{0x57, 0x8D, 0x8E, 0xC8, 0x00, 0x00, 0x00, 0xE8, 0xBC, 0xF2, 0xFE, 0xFF}
	netCenterDealFailureSignature    = []byte{0x85, 0xFF, 0x0F, 0x84, 0x9E, 0x00, 0x00, 0x00}
	netCenterSessionSuccessSignature = []byte{0x57, 0x8D, 0x8E, 0xC8, 0x00, 0x00, 0x00, 0xE8, 0x43, 0xF2, 0xFE, 0xFF}
	netCenterSessionFailureSignature = []byte{0x85, 0xFF, 0x74, 0x08, 0x8B, 0x07, 0x6A, 0x01}
	netCenterSessionFailureContext   = []byte{0x85, 0xFF, 0x74, 0x08, 0x8B, 0x07, 0x6A, 0x01, 0x8B, 0xCF, 0xFF, 0x10, 0x6A, 0x01, 0x58, 0xEB, 0x1F, 0x8D, 0x45, 0xF0}
	netCenterListAddTailContext      = []byte{0x56, 0x8B, 0xF1, 0x6A, 0x00, 0xFF, 0x76, 0x08, 0xE8, 0x23, 0xE9, 0xFE, 0xFF, 0x8B, 0x4C, 0x24, 0x08, 0x89, 0x48, 0x08, 0x8B, 0x4E, 0x08, 0x85, 0xC9, 0x74, 0x04, 0x89, 0x01, 0xEB, 0x03, 0x89, 0x46, 0x04, 0x89, 0x46, 0x08, 0x5E, 0xC2, 0x04, 0x00}
	netCenterListRemoveAtContext     = []byte{0x8B, 0x44, 0x24, 0x04, 0x56, 0x3B, 0x41, 0x04, 0x75, 0x07, 0x8B, 0x10, 0x89, 0x51, 0x04, 0xEB, 0x07, 0x8B, 0x50, 0x04, 0x8B, 0x30, 0x89, 0x32, 0x3B, 0x41, 0x08, 0x75, 0x08, 0x8B, 0x50, 0x04, 0x89, 0x51, 0x08, 0xEB, 0x08, 0x8B, 0x10, 0x8B, 0x70, 0x04, 0x89, 0x72, 0x04}
)

type ShopConnectCompatibilityResult struct {
	PID                 uint32 `json:"pid"`
	ServerID            uint32 `json:"server_id"`
	ModuleBase          string `json:"module_base"`
	ShopServerPatch     string `json:"shop_server_patch"`
	SuccessPatch        string `json:"success_patch"`
	FailurePatch        string `json:"failure_patch"`
	SessionSuccessPatch string `json:"session_success_patch"`
	SessionFailurePatch string `json:"session_failure_patch"`
	StubAddress         string `json:"stub_address"`
	Installed           bool   `json:"installed"`
	Behavior            string `json:"behavior"`
}

// InstallShopConnectCompatibility is an opt-in diagnostic for the legacy shop
// connection callback list. It is deliberately not part of the normal client
// startup path: the original client already owns the type-3/type-2 connection
// lifecycle, and no capture proves that rewriting these completion branches is
// required when the native endpoint and catalog are valid.
//
// EnterShop resolves one QQTDir shop server and gives the resulting endpoint
// to both the native type-3 catalog socket and the native type-2
// config/session/purchase socket. They remain independent TCP objects but use
// the same destination. This compatibility layer does not rewrite either
// object's endpoint. Transport types remain unchanged: type 3 carries the
// 0x035E catalog request while type 2 carries configuration and 0x0259 purchase
// traffic. The completion path scans for the exact object before deciding
// whether the original AddTail is still needed. On failure, the exact matching
// node is removed with the client's own RemoveAt before the original destructor
// runs. Queue-ready pre-registration is deliberately absent: the recorded
// single-variable bisection proved that hook suppresses the native connect.
// Keep this API for controlled A/B captures only; callers must opt in.
func InstallShopConnectCompatibility(pid, serverID uint32, timeout time.Duration) (ShopConnectCompatibilityResult, error) {
	if pid == 0 {
		return ShopConnectCompatibilityResult{}, fmt.Errorf("pid must be non-zero")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return ShopConnectCompatibilityResult{}, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)
	stub, sites, err := installShopConnectOrdering(process, pid, timeout)
	if err != nil {
		return ShopConnectCompatibilityResult{}, err
	}
	return ShopConnectCompatibilityResult{
		PID: pid, ServerID: serverID, ModuleBase: stub.ModuleBase,
		SuccessPatch: sites.dealSuccess, FailurePatch: sites.dealFailure,
		SessionSuccessPatch: sites.sessionSuccess, SessionFailurePatch: sites.sessionFailure,
		StubAddress: stub.StubAddress, Installed: true, Behavior: stub.Behavior,
	}, nil
}

type shopConnectSites struct {
	dealSuccess, dealFailure, sessionSuccess, sessionFailure string
}

func installClientShopConnectOrdering(process syscall.Handle, pid uint32, timeout time.Duration) (ImportStub, error) {
	stub, _, err := installShopConnectOrdering(process, pid, timeout)
	return stub, err
}

func installShopConnectOrdering(process syscall.Handle, pid uint32, timeout time.Duration) (ImportStub, shopConnectSites, error) {
	base, err := waitForModule(process, pid, "NetCenter.dll", timeout)
	if err != nil {
		return ImportStub{}, shopConnectSites{}, err
	}
	deadline := time.Now().Add(timeout)
	locate := func(name string, pattern []byte) (uintptr, uint32, error) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return 0, 0, fmt.Errorf("locate NetCenter %s: timeout", name)
		}
		address, imageSize, locateErr := waitForUniqueExecutableImagePattern(process, base, pattern, remaining)
		if locateErr != nil {
			return 0, 0, fmt.Errorf("locate NetCenter %s: %w", name, locateErr)
		}
		return address, imageSize, nil
	}
	successAddress, imageSize, err := locate("type-3 success", netCenterDealSuccessSignature)
	if err != nil {
		return ImportStub{}, shopConnectSites{}, err
	}
	failureAddress, _, err := locate("type-3 failure", netCenterDealFailureSignature)
	if err != nil {
		return ImportStub{}, shopConnectSites{}, err
	}
	sessionSuccessAddress, _, err := locate("type-2 success", netCenterSessionSuccessSignature)
	if err != nil {
		return ImportStub{}, shopConnectSites{}, err
	}
	sessionFailureAddress, _, err := locate("type-2 failure", netCenterSessionFailureContext)
	if err != nil {
		return ImportStub{}, shopConnectSites{}, err
	}
	addTailAddress, _, err := locate("CPtrList::AddTail", netCenterListAddTailContext)
	if err != nil {
		return ImportStub{}, shopConnectSites{}, err
	}
	// The linked CPtrList implementation places RemoveAt 0x68 bytes after its
	// AddTail. NetCenter contains other byte-identical RemoveAt implementations,
	// so a module-wide scan is intentionally not used here. Derive the helper
	// from the uniquely located AddTail cluster and still verify its complete
	// body before any patch is installed.
	removeAtAddress := addTailAddress + 0x68
	removeAtOriginal, ok := readRemote(process, removeAtAddress, len(netCenterListRemoveAtContext))
	if !ok || !equalBytes(removeAtOriginal, netCenterListRemoveAtContext) {
		return ImportStub{}, shopConnectSites{}, fmt.Errorf(
			"paired CPtrList::RemoveAt changed at 0x%08X: got %X", removeAtAddress, removeAtOriginal,
		)
	}

	dealDispatchResume, err := remoteRelativeTarget(process, successAddress+12, 0xE9, 5)
	if err != nil {
		return ImportStub{}, shopConnectSites{}, fmt.Errorf("decode type-3 dispatch target: %w", err)
	}
	sessionDispatchResume, err := remoteShortJumpTarget(process, sessionFailureAddress+2, 0x74)
	if err != nil {
		return ImportStub{}, shopConnectSites{}, fmt.Errorf("decode type-2 dispatch target: %w", err)
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xffffffff {
		return ImportStub{}, shopConnectSites{}, fmt.Errorf("VirtualAllocEx shop-connect compatibility: 0x%X (%v)", page, allocErr)
	}
	keepPage := false
	defer func() {
		if !keepPage {
			procVirtualFreeEx.Call(uintptr(process), page, 0, memRelease)
		}
	}()
	successStubAddress := page + 0x100
	failureStubAddress := page + 0x200
	sessionSuccessStubAddress := page + 0x400
	sessionFailureStubAddress := page + 0x500
	successStub := buildShopSuccessStub(
		successStubAddress, addTailAddress, dealDispatchResume,
	)
	failureStub := buildShopFailureStub(
		failureStubAddress, removeAtAddress,
		failureAddress+netCenterDealFailurePatchSize, dealDispatchResume,
	)
	sessionSuccessStub := buildShopSuccessStub(
		sessionSuccessStubAddress, addTailAddress,
		sessionSuccessAddress+netCenterSessionSuccessPatchSize,
	)
	sessionFailureStub := buildShopFailureStub(
		sessionFailureStubAddress, removeAtAddress,
		sessionFailureAddress+4, sessionDispatchResume,
	)
	for _, write := range []struct {
		name    string
		address uintptr
		data    []byte
	}{{"success", successStubAddress, successStub}, {"failure", failureStubAddress, failureStub},
		{"session-success", sessionSuccessStubAddress, sessionSuccessStub}, {"session-failure", sessionFailureStubAddress, sessionFailureStub}} {
		if err := writeRemote(process, write.address, write.data); err != nil {
			return ImportStub{}, shopConnectSites{}, fmt.Errorf("write shop-connect %s stub: %w", write.name, err)
		}
	}

	successPatch := relativeJumpPatch(successAddress, successStubAddress, netCenterDealSuccessPatchSize)
	failurePatch := relativeJumpPatch(failureAddress, failureStubAddress, netCenterDealFailurePatchSize)
	sessionSuccessPatch := relativeJumpPatch(sessionSuccessAddress, sessionSuccessStubAddress, netCenterSessionSuccessPatchSize)
	sessionFailurePatch := relativeJumpPatch(sessionFailureAddress, sessionFailureStubAddress, netCenterSessionFailurePatchSize)
	applied := make([]struct {
		address  uintptr
		original []byte
	}, 0, 6)
	patches := []struct {
		name     string
		address  uintptr
		patch    []byte
		original []byte
	}{{"success", successAddress, successPatch, netCenterDealSuccessSignature}, {"failure", failureAddress, failurePatch, netCenterDealFailureSignature},
		{"session-success", sessionSuccessAddress, sessionSuccessPatch, netCenterSessionSuccessSignature}, {"session-failure", sessionFailureAddress, sessionFailurePatch, netCenterSessionFailureSignature}}
	for _, patch := range patches {
		if !ensureRemoteBytes(process, patch.address, patch.patch, pageExecuteReadWrite) {
			for index := len(applied) - 1; index >= 0; index-- {
				ensureRemoteBytes(process, applied[index].address, applied[index].original, pageExecuteReadWrite)
			}
			return ImportStub{}, shopConnectSites{}, fmt.Errorf("patch NetCenter CDeal %s site at 0x%08X", patch.name, patch.address)
		}
		applied = append(applied, struct {
			address  uintptr
			original []byte
		}{patch.address, patch.original})
	}
	procFlushInstruction.Call(uintptr(process), page, 0x800)
	for _, patch := range patches {
		procFlushInstruction.Call(uintptr(process), patch.address, uintptr(len(patch.patch)))
	}
	keepPage = true
	behavior := "local-loopback completion ordering only: preserve the native type-3 catalog and type-2 config/purchase connect path and shared endpoint; deduplicate success registration and remove exact failed nodes"
	sites := shopConnectSites{
		dealSuccess: fmt.Sprintf("0x%08X", successAddress), dealFailure: fmt.Sprintf("0x%08X", failureAddress),
		sessionSuccess: fmt.Sprintf("0x%08X", sessionSuccessAddress), sessionFailure: fmt.Sprintf("0x%08X", sessionFailureAddress),
	}
	return ImportStub{
		Module: "NetCenter.dll", ModuleBase: fmt.Sprintf("0x%08X", base),
		Library: "NetCenter.dll", Symbol: "local shop socket callback ordering",
		StubAddress: fmt.Sprintf("0x%08X", page), TargetAddress: sites.dealSuccess,
		TargetRVA: fmt.Sprintf("0x%X", successAddress-base), TargetSection: "runtime executable image",
		TargetOriginal: fmt.Sprintf("%X", netCenterDealSuccessSignature),
		TargetContext:  fmt.Sprintf("six uniquely located executable contexts in 0x%X-byte image; completion patches type3=%s/%s type2=%s/%s; helpers AddTail=0x%08X RemoveAt=0x%08X", imageSize, sites.dealSuccess, sites.dealFailure, sites.sessionSuccess, sites.sessionFailure, addTailAddress, removeAtAddress),
		Behavior:       behavior,
	}, sites, nil
}

func remoteRelativeTarget(process syscall.Handle, address uintptr, opcode byte, size int) (uintptr, error) {
	code, ok := readRemote(process, address, size)
	if !ok || len(code) != size || size != 5 || code[0] != opcode {
		return 0, fmt.Errorf("instruction at 0x%08X is %X", address, code)
	}
	displacement := int32(binary.LittleEndian.Uint32(code[1:5]))
	return uintptr(int64(address) + int64(size) + int64(displacement)), nil
}

func remoteShortJumpTarget(process syscall.Handle, address uintptr, opcode byte) (uintptr, error) {
	code, ok := readRemote(process, address, 2)
	if !ok || len(code) != 2 || code[0] != opcode {
		return 0, fmt.Errorf("instruction at 0x%08X is %X", address, code)
	}
	return uintptr(int64(address) + 2 + int64(int8(code[1]))), nil
}

func buildShopSuccessStub(stubAddress, addTailAddress, resumeAddress uintptr) []byte {
	stub := []byte{
		0x8D, 0x8E, 0xC8, 0x00, 0x00, 0x00, // lea ecx,[esi+c8]
		0x8B, 0x41, 0x04, // mov eax,[ecx+4] (head)
		0x85, 0xC0, // scan: test eax,eax
		0x74, 0x09, // jz add
		0x39, 0x78, 0x08, // cmp [eax+8],edi
		0x74, 0x0A, // je resume
		0x8B, 0x00, // mov eax,[eax] (next)
		0xEB, 0xF3, // jmp scan
		0x57,             // add: push edi
		0xE8, 0, 0, 0, 0, // call AddTail
	}
	callOffset := 24
	binary.LittleEndian.PutUint32(stub[callOffset:callOffset+4], uint32(addTailAddress-(stubAddress+uintptr(callOffset+4))))
	return appendAbsoluteReturn(stub, resumeAddress)
}

func buildShopFailureStub(stubAddress, removeAtAddress, destructorResumeAddress, dispatchResumeAddress uintptr) []byte {
	stub := []byte{
		0x85, 0xFF, // test edi,edi
		0x0F, 0x84, 0, 0, 0, 0, // jz dispatch
		0x8D, 0x8E, 0xC8, 0x00, 0x00, 0x00, // lea ecx,[esi+c8]
		0x8B, 0x41, 0x04, // mov eax,[ecx+4] (head)
		0x85, 0xC0, // scan: test eax,eax
		0x0F, 0x84, 0, 0, 0, 0, // jz destructor
		0x39, 0x78, 0x08, // cmp [eax+8],edi
		0x74, 0x04, // je found
		0x8B, 0x00, // mov eax,[eax] (next)
		0xEB, 0xEF, // jmp scan
		0x50,             // found: push eax
		0xE8, 0, 0, 0, 0, // call RemoveAt
	}
	destructorOffset := len(stub)
	stub = appendAbsoluteReturn(stub, destructorResumeAddress)
	dispatchOffset := len(stub)
	stub = appendAbsoluteReturn(stub, dispatchResumeAddress)
	binary.LittleEndian.PutUint32(stub[4:8], uint32(dispatchOffset-8))
	binary.LittleEndian.PutUint32(stub[21:25], uint32(destructorOffset-25))
	callOffset := 36
	binary.LittleEndian.PutUint32(stub[callOffset:callOffset+4], uint32(removeAtAddress-(stubAddress+uintptr(callOffset+4))))
	return stub
}

func appendAbsoluteReturn(code []byte, address uintptr) []byte {
	code = append(code, 0x68)
	code = binary.LittleEndian.AppendUint32(code, uint32(address))
	return append(code, 0xC3)
}
