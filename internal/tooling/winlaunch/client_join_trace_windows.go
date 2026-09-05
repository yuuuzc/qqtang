package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

const (
	clientJoinRequestRVA         = uintptr(0x12D570)
	clientJoinRequestResumeRVA   = uintptr(0x12D579)
	clientJoinRecordSize         = 64
	clientJoinStateRVA           = uintptr(0x12D642)
	clientJoinStateResumeRVA     = uintptr(0x12D648)
	clientJoinOutputRVA          = uintptr(0x12D66E)
	clientJoinOutputResumeRVA    = uintptr(0x12D675)
	clientJoinOutputFrameSize    = 0x240
	clientJoinOutputRecordSize   = 64 + clientJoinOutputFrameSize
	qqtModulesStageCNetworkRVA   = uintptr(0x9427)
	qqtModulesStageCResumeRVA    = uintptr(0x942C)
	qqtModulesSocketCounterRVA   = uintptr(0x7F744)
	qqtSectionServicesRVA        = uintptr(0x1991E)
	qqtSectionServicesResumeRVA  = uintptr(0x19923)
	qqtSectionSendRVA            = uintptr(0x19C8D)
	qqtSectionSendResumeRVA      = uintptr(0x19C92)
	qqtSectionLoginPacketSize    = 0x1C5
	qqtSectionSendRecordSize     = 64 + qqtSectionLoginPacketSize
	qqtSectionTransportRVA       = uintptr(0x1AF72)
	qqtSectionTransportResumeRVA = uintptr(0x1AF78)
)

var clientJoinRequestSignature = []byte{0x55, 0x8B, 0xEC, 0x81, 0xEC, 0x7C, 0x02, 0x00, 0x00}
var clientJoinStateSignature = []byte{0x83, 0x7D, 0xFC, 0x00, 0x74, 0x42}
var clientJoinOutputSignature = []byte{0x66, 0x8B, 0x8D, 0xD8, 0xFD, 0xFF, 0xFF}
var qqtModulesStageCNetworkSignature = []byte{0x8B, 0x45, 0xFC, 0x3B, 0xC3}
var qqtSectionServicesSignature = []byte{0x66, 0x8B, 0x7D, 0x10, 0x59}
var qqtSectionSendSignature = []byte{0x8B, 0x06, 0x83, 0xC4, 0x20}
var qqtSectionTransportSignature = []byte{0x8B, 0x45, 0xF0, 0x8B, 0x7D, 0x0C}

// ClientJoinCapture records one native request_LoginSection invocation. The
// five arguments are deliberately left scalar because their meaning is still
// being established from runtime evidence.
type ClientJoinCapture struct {
	PID            uint32   `json:"pid"`
	ElapsedMS      int64    `json:"elapsed_ms"`
	Calls          uint32   `json:"calls"`
	ThreadID       uint32   `json:"thread_id,omitempty"`
	Caller         string   `json:"caller"`
	Arguments      []string `json:"arguments,omitempty"`
	ServiceLocator string   `json:"service_locator"`
	PatchAddress   string   `json:"patch_address"`
	StubAddress    string   `json:"stub_address"`
	OriginalBytes  string   `json:"original_bytes"`
	Behavior       string   `json:"behavior"`
}

type ClientJoinTracer struct {
	pid          uint32
	process      syscall.Handle
	started      time.Time
	patchAddress uintptr
	stubAddress  uintptr
	record       uintptr
	original     []byte
}

type ClientJoinStateCapture struct {
	PID               uint32   `json:"pid"`
	ElapsedMS         int64    `json:"elapsed_ms"`
	Calls             uint32   `json:"calls"`
	ThreadID          uint32   `json:"thread_id,omitempty"`
	FramePointer      string   `json:"frame_pointer"`
	InterfaceA        string   `json:"interface_a"`
	InterfaceAVTable  string   `json:"interface_a_vtable"`
	InterfaceAMethods []string `json:"interface_a_methods,omitempty"`
	InterfaceB        string   `json:"interface_b"`
	InterfaceBVTable  string   `json:"interface_b_vtable"`
	InterfaceBMethods []string `json:"interface_b_methods,omitempty"`
	UIScript          string   `json:"ui_script"`
	UIScriptVTable    string   `json:"ui_script_vtable"`
	UIScriptMethods   []string `json:"ui_script_methods,omitempty"`
	GuideValues       []string `json:"guide_values,omitempty"`
	Arguments         []string `json:"arguments,omitempty"`
	PatchAddress      string   `json:"patch_address"`
	StubAddress       string   `json:"stub_address"`
	OriginalBytes     string   `json:"original_bytes"`
	Behavior          string   `json:"behavior"`
}

type ClientJoinStateTracer struct {
	pid          uint32
	process      syscall.Handle
	started      time.Time
	patchAddress uintptr
	stubAddress  uintptr
	record       uintptr
	original     []byte
}

// ClientJoinOutputCapture records the local selection object immediately
// after QQTDir's vtable+0x4C method fills it. SelectionValueA and
// SelectionValueB are the two WORDs that Client.exe passes to the next
// QQTModules interface call.
type ClientJoinOutputCapture struct {
	PID                uint32 `json:"pid"`
	ElapsedMS          int64  `json:"elapsed_ms"`
	Calls              uint32 `json:"calls"`
	ThreadID           uint32 `json:"thread_id,omitempty"`
	FramePointer       string `json:"frame_pointer"`
	FrameStart         string `json:"frame_start"`
	MethodResult       string `json:"method_result"`
	InterfaceA         string `json:"interface_a"`
	InterfaceB         string `json:"interface_b"`
	SelectionValueA    uint16 `json:"selection_value_a"`
	SelectionValueB    uint16 `json:"selection_value_b"`
	SelectionValueAHex string `json:"selection_value_a_hex"`
	SelectionValueBHex string `json:"selection_value_b_hex"`
	FrameHex           string `json:"frame_hex"`
	PatchAddress       string `json:"patch_address"`
	StubAddress        string `json:"stub_address"`
	OriginalBytes      string `json:"original_bytes"`
	Behavior           string `json:"behavior"`
}

type ClientJoinOutputTracer struct {
	pid          uint32
	process      syscall.Handle
	started      time.Time
	patchAddress uintptr
	stubAddress  uintptr
	record       uintptr
	original     []byte
}

type QQTModulesStageCNetworkCapture struct {
	PID              uint32   `json:"pid"`
	ElapsedMS        int64    `json:"elapsed_ms"`
	Calls            uint32   `json:"calls"`
	ThreadID         uint32   `json:"thread_id,omitempty"`
	FramePointer     string   `json:"frame_pointer"`
	LoginObject      string   `json:"login_object"`
	LoginInterface   string   `json:"login_interface"`
	InterfaceVTable  string   `json:"interface_vtable"`
	InterfaceMethods []string `json:"interface_methods,omitempty"`
	ServiceLocator   string   `json:"service_locator"`
	ArgumentA        uint32   `json:"argument_a"`
	ArgumentB        uint32   `json:"argument_b"`
	SocketCounter    uint32   `json:"socket_counter"`
	LastSwitchTick   uint32   `json:"last_switch_tick"`
	SwitchIntervalMS uint32   `json:"switch_interval_ms"`
	LoginConfig      string   `json:"login_config"`
	PatchAddress     string   `json:"patch_address"`
	StubAddress      string   `json:"stub_address"`
	OriginalBytes    string   `json:"original_bytes"`
	Behavior         string   `json:"behavior"`
}

type QQTModulesStageCNetworkTracer struct {
	pid          uint32
	process      syscall.Handle
	started      time.Time
	patchAddress uintptr
	stubAddress  uintptr
	record       uintptr
	original     []byte
}

type QQTSectionResolvedService struct {
	Name    string   `json:"name"`
	Object  string   `json:"object"`
	VTable  string   `json:"vtable"`
	Methods []string `json:"methods,omitempty"`
}

type QQTSectionServicesCapture struct {
	PID           uint32                      `json:"pid"`
	ElapsedMS     int64                       `json:"elapsed_ms"`
	Calls         uint32                      `json:"calls"`
	ThreadID      uint32                      `json:"thread_id,omitempty"`
	FramePointer  string                      `json:"frame_pointer"`
	SectionObject string                      `json:"section_object"`
	Services      []QQTSectionResolvedService `json:"services"`
	ArgumentA     uint32                      `json:"argument_a"`
	ArgumentB     uint32                      `json:"argument_b"`
	SocketCounter uint32                      `json:"socket_counter"`
	PacketAddress string                      `json:"packet_address,omitempty"`
	SessionWord   uint16                      `json:"session_word,omitempty"`
	ServerID      uint32                      `json:"server_id,omitempty"`
	ServerIP      string                      `json:"server_ip,omitempty"`
	ServerPort    uint16                      `json:"server_port,omitempty"`
	CreateResult  string                      `json:"create_result,omitempty"`
	Connection    string                      `json:"connection,omitempty"`
	IDBefore      uint16                      `json:"connection_id_before,omitempty"`
	IDAfter       uint16                      `json:"connection_id_after,omitempty"`
	GateCalls     uint32                      `json:"gate_calls"`
	ReloadArmed   bool                        `json:"reload_armed"`
	ArmError      string                      `json:"arm_error,omitempty"`
	GateAddress   string                      `json:"gate_address"`
	PatchAddress  string                      `json:"patch_address"`
	StubAddress   string                      `json:"stub_address"`
	OriginalBytes string                      `json:"original_bytes"`
	Behavior      string                      `json:"behavior"`
}

type QQTSectionServicesTracer struct {
	pid              uint32
	process          syscall.Handle
	started          time.Time
	patchAddress     uintptr
	stubAddress      uintptr
	record           uintptr
	original         []byte
	gatePatchAddress uintptr
	gateStubAddress  uintptr
	gateRecord       uintptr
	gateOriginal     []byte
	reloadArmed      bool
	armError         string
	transportMode    bool
	preconnectMode   bool
	serverID         uint32
	serverIP         uint32
	serverPort       uint16
}

type QQTSectionSendCapture struct {
	PID           uint32   `json:"pid"`
	ElapsedMS     int64    `json:"elapsed_ms"`
	Calls         uint32   `json:"calls"`
	ThreadID      uint32   `json:"thread_id,omitempty"`
	FramePointer  string   `json:"frame_pointer"`
	SectionObject string   `json:"section_object"`
	SectionVTable string   `json:"section_vtable"`
	SendMethod    string   `json:"send_method"`
	PacketAddress string   `json:"packet_address"`
	PacketHex     string   `json:"packet_hex,omitempty"`
	GateCalls     uint32   `json:"gate_calls"`
	ReloadArmed   bool     `json:"reload_armed"`
	ArmError      string   `json:"arm_error,omitempty"`
	GateAddress   string   `json:"gate_address"`
	PatchAddress  string   `json:"patch_address"`
	StubAddress   string   `json:"stub_address"`
	OriginalBytes string   `json:"original_bytes"`
	Behavior      string   `json:"behavior"`
	VTableMethods []string `json:"vtable_methods,omitempty"`
}

type QQTSectionSendTracer struct {
	pid              uint32
	process          syscall.Handle
	started          time.Time
	patchAddress     uintptr
	stubAddress      uintptr
	record           uintptr
	original         []byte
	gatePatchAddress uintptr
	gateStubAddress  uintptr
	gateRecord       uintptr
	gateOriginal     []byte
	reloadArmed      bool
	armError         string
}

// InstallClientJoinTracer transparently wraps the unpacked Client.exe native
// request_LoginSection function. It records the entry arguments and replays the
// entire displaced prologue before continuing at the original resume address.
func InstallClientJoinTracer(pid uint32, timeout time.Duration) (*ClientJoinTracer, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &ClientJoinTracer{pid: pid, process: process, started: time.Now()}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()

	moduleBase, err := waitForModule(process, pid, "Client.exe", timeout)
	if err != nil {
		return nil, err
	}
	tracer.patchAddress = moduleBase + clientJoinRequestRVA
	tracer.original, _ = readRemote(process, tracer.patchAddress, len(clientJoinRequestSignature))
	if len(tracer.original) != len(clientJoinRequestSignature) ||
		!equalBytes(tracer.original, clientJoinRequestSignature) {
		return nil, fmt.Errorf("Client request_LoginSection signature mismatch at 0x%08X: got %X", tracer.patchAddress, tracer.original)
	}

	page, _, allocErr := procVirtualAllocEx.Call(
		uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite,
	)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx Client join trace: 0x%X (%v)", page, allocErr)
	}
	tracer.stubAddress = page
	tracer.record = page + 0x300
	stub := buildClientJoinTraceStub(
		page, tracer.record, moduleBase+clientJoinRequestResumeRVA,
		moduleBase+0x3FAF04, tracer.original,
	)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write Client join trace stub: %w", err)
	}
	patch := []byte{0xE9, 0, 0, 0, 0, 0x90, 0x90, 0x90, 0x90}
	binary.LittleEndian.PutUint32(patch[1:5], uint32(page-(tracer.patchAddress+5)))
	if !ensureRemoteBytes(process, tracer.patchAddress, patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch Client request_LoginSection at 0x%08X", tracer.patchAddress)
	}
	procFlushInstruction.Call(uintptr(process), tracer.patchAddress, uintptr(len(patch)))
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	failed = false
	return tracer, nil
}

func buildClientJoinTraceStub(page, record, resume, serviceLocatorAddress uintptr, original []byte) []byte {
	stub := []byte{0x9C, 0x60} // pushfd; pushad
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record)) // calls
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+4)) // thread id
	for _, field := range []struct {
		stackOffset byte
		record      uintptr
	}{{0x24, record + 8}, {0x28, record + 12}, {0x2C, record + 16}, {0x30, record + 20}, {0x34, record + 24}, {0x38, record + 28}} {
		stub = append(stub, 0x8B, 0x44, 0x24, field.stackOffset, 0xA3)
		stub = binary.LittleEndian.AppendUint32(stub, uint32(field.record))
	}
	stub = append(stub, 0xA1)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(serviceLocatorAddress))
	stub = append(stub, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+32))
	stub = append(stub, 0x61, 0x9D)
	stub = append(stub, original...)
	return appendRelativeJump(stub, page+uintptr(len(stub)), resume)
}

// InstallClientJoinStateTracer records the request_LoginSection state after
// its three service-locator queries and before either required-interface null
// check. The six displaced bytes are replayed unchanged.
func InstallClientJoinStateTracer(pid uint32, timeout time.Duration) (*ClientJoinStateTracer, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &ClientJoinStateTracer{pid: pid, process: process, started: time.Now()}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()
	moduleBase, err := waitForModule(process, pid, "Client.exe", timeout)
	if err != nil {
		return nil, err
	}
	tracer.patchAddress = moduleBase + clientJoinStateRVA
	tracer.original, _ = readRemote(process, tracer.patchAddress, len(clientJoinStateSignature))
	if len(tracer.original) != len(clientJoinStateSignature) ||
		!equalBytes(tracer.original, clientJoinStateSignature) {
		return nil, fmt.Errorf("Client join-state signature mismatch at 0x%08X: got %X", tracer.patchAddress, tracer.original)
	}
	page, _, allocErr := procVirtualAllocEx.Call(
		uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite,
	)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx Client join-state trace: 0x%X (%v)", page, allocErr)
	}
	tracer.stubAddress = page
	tracer.record = page + 0x300
	stub := buildClientJoinStateTraceStub(page, tracer.record, moduleBase+clientJoinStateResumeRVA, tracer.original)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write Client join-state trace stub: %w", err)
	}
	patch := []byte{0xE9, 0, 0, 0, 0, 0x90}
	binary.LittleEndian.PutUint32(patch[1:5], uint32(page-(tracer.patchAddress+5)))
	if !ensureRemoteBytes(process, tracer.patchAddress, patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch Client join state at 0x%08X", tracer.patchAddress)
	}
	procFlushInstruction.Call(uintptr(process), tracer.patchAddress, uintptr(len(patch)))
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	failed = false
	return tracer, nil
}

func buildClientJoinStateTraceStub(page, record, resume uintptr, original []byte) []byte {
	stub := []byte{0x9C, 0x60}
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record))
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+4))
	stub = append(stub, 0x8B, 0x44, 0x24, 0x08, 0xA3) // saved EBP
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+8))
	for _, field := range []struct {
		ebpOffset byte
		record    uintptr
	}{{0xFC, record + 12}, {0xF8, record + 16}, {0xEC, record + 20}, {0xF4, record + 24}, {0xF0, record + 28},
		{0x08, record + 32}, {0x0C, record + 36}, {0x10, record + 40}, {0x14, record + 44}, {0x18, record + 48}} {
		stub = append(stub, 0x8B, 0x50, field.ebpOffset, 0x89, 0x15)
		stub = binary.LittleEndian.AppendUint32(stub, uint32(field.record))
	}
	stub = append(stub, 0x61, 0x9D)
	stub = append(stub, original...)
	return appendRelativeJump(stub, page+uintptr(len(stub)), resume)
}

// InstallClientJoinOutputTracer records QQTDir's selected-section output and
// then replays the displaced MOV CX,[EBP-0x228] instruction unchanged.
func InstallClientJoinOutputTracer(pid uint32, timeout time.Duration) (*ClientJoinOutputTracer, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &ClientJoinOutputTracer{pid: pid, process: process, started: time.Now()}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()
	moduleBase, err := waitForModule(process, pid, "Client.exe", timeout)
	if err != nil {
		return nil, err
	}
	tracer.patchAddress = moduleBase + clientJoinOutputRVA
	tracer.original, _ = readRemote(process, tracer.patchAddress, len(clientJoinOutputSignature))
	if len(tracer.original) != len(clientJoinOutputSignature) ||
		!equalBytes(tracer.original, clientJoinOutputSignature) {
		return nil, fmt.Errorf("Client join-output signature mismatch at 0x%08X: got %X", tracer.patchAddress, tracer.original)
	}
	page, _, allocErr := procVirtualAllocEx.Call(
		uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite,
	)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx Client join-output trace: 0x%X (%v)", page, allocErr)
	}
	tracer.stubAddress = page
	tracer.record = page + 0x300
	stub := buildClientJoinOutputTraceStub(page, tracer.record, moduleBase+clientJoinOutputResumeRVA, tracer.original)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write Client join-output trace stub: %w", err)
	}
	patch := []byte{0xE9, 0, 0, 0, 0, 0x90, 0x90}
	binary.LittleEndian.PutUint32(patch[1:5], uint32(page-(tracer.patchAddress+5)))
	if !ensureRemoteBytes(process, tracer.patchAddress, patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch Client join output at 0x%08X", tracer.patchAddress)
	}
	procFlushInstruction.Call(uintptr(process), tracer.patchAddress, uintptr(len(patch)))
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	failed = false
	return tracer, nil
}

func buildClientJoinOutputTraceStub(page, record, resume uintptr, original []byte) []byte {
	stub := []byte{0x9C, 0x60} // pushfd; pushad
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record))
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+4))
	stub = append(stub, 0x8B, 0x44, 0x24, 0x08) // saved EBP
	stub = append(stub, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+8))
	stub = append(stub, 0x89, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+12)) // method EAX
	for _, field := range []struct {
		ebpOffset byte
		record    uintptr
	}{{0xFC, record + 16}, {0xF8, record + 20}} {
		stub = append(stub, 0x8B, 0x50, field.ebpOffset, 0x89, 0x15)
		stub = binary.LittleEndian.AppendUint32(stub, uint32(field.record))
	}
	stub = append(stub, 0x8D, 0xB0, 0xC4, 0xFD, 0xFF, 0xFF) // ESI=EBP-0x23C
	stub = append(stub, 0x89, 0x35)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+24))
	stub = append(stub, 0xBF)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+64))
	stub = append(stub, 0xB9)
	stub = binary.LittleEndian.AppendUint32(stub, clientJoinOutputFrameSize/4)
	stub = append(stub, 0xFC, 0xF3, 0xA5) // cld; rep movsd
	stub = append(stub, 0x61, 0x9D)
	stub = append(stub, original...)
	return appendRelativeJump(stub, page+uintptr(len(stub)), resume)
}

// InstallQQTModulesStageCNetworkTracer records the login interface immediately
// after QQTModules resolves it and before the null check that gates the game
// server login calls.
func InstallQQTModulesStageCNetworkTracer(pid uint32, timeout time.Duration) (*QQTModulesStageCNetworkTracer, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &QQTModulesStageCNetworkTracer{pid: pid, process: process, started: time.Now()}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()
	moduleBase, err := waitForModule(process, pid, "QQTModules.dll", timeout)
	if err != nil {
		return nil, err
	}
	tracer.patchAddress = moduleBase + qqtModulesStageCNetworkRVA
	tracer.original, _ = readRemote(process, tracer.patchAddress, len(qqtModulesStageCNetworkSignature))
	if len(tracer.original) != len(qqtModulesStageCNetworkSignature) ||
		!equalBytes(tracer.original, qqtModulesStageCNetworkSignature) {
		return nil, fmt.Errorf("QQTModules Stage C signature mismatch at 0x%08X: got %X", tracer.patchAddress, tracer.original)
	}
	page, _, allocErr := procVirtualAllocEx.Call(
		uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite,
	)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx QQTModules Stage C trace: 0x%X (%v)", page, allocErr)
	}
	tracer.stubAddress = page
	tracer.record = page + 0x300
	stub := buildQQTModulesStageCNetworkTraceStub(
		page, tracer.record, moduleBase+qqtModulesStageCResumeRVA,
		moduleBase+qqtModulesSocketCounterRVA, tracer.original,
	)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write QQTModules Stage C trace stub: %w", err)
	}
	patch := []byte{0xE9, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(patch[1:5], uint32(page-(tracer.patchAddress+5)))
	if !ensureRemoteBytes(process, tracer.patchAddress, patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch QQTModules Stage C at 0x%08X", tracer.patchAddress)
	}
	procFlushInstruction.Call(uintptr(process), tracer.patchAddress, uintptr(len(patch)))
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	failed = false
	return tracer, nil
}

func buildQQTModulesStageCNetworkTraceStub(page, record, resume, socketCounter uintptr, original []byte) []byte {
	stub := []byte{0x9C, 0x60}
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record))
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+4))
	stub = append(stub, 0x8B, 0x44, 0x24, 0x08, 0xA3) // saved EBP
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+8))
	for _, field := range []struct {
		ebpOffset byte
		record    uintptr
	}{{0xFC, record + 16}, {0x0C, record + 24}, {0x10, record + 28}} {
		stub = append(stub, 0x8B, 0x50, field.ebpOffset, 0x89, 0x15)
		stub = binary.LittleEndian.AppendUint32(stub, uint32(field.record))
	}
	stub = append(stub, 0x8B, 0x44, 0x24, 0x04, 0xA3) // saved ESI/login object
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+12))
	for _, field := range []struct {
		objectOffset byte
		record       uintptr
	}{{0x24, record + 20}, {0x44, record + 36}, {0x48, record + 40}, {0x30, record + 44}} {
		stub = append(stub, 0x8B, 0x50, field.objectOffset, 0x89, 0x15)
		stub = binary.LittleEndian.AppendUint32(stub, uint32(field.record))
	}
	stub = append(stub, 0xA1)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(socketCounter))
	stub = append(stub, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+32))
	stub = append(stub, 0x61, 0x9D)
	stub = append(stub, original...)
	return appendRelativeJump(stub, page+uintptr(len(stub)), resume)
}

// InstallQQTSectionServicesTracer first holds the stable QQTModules call site.
// QQTSection is unloaded and reloaded for each attempt, so CaptureUntilFirst
// arms the inner trace only after that reload and then releases the caller.
func InstallQQTSectionServicesTracer(pid uint32, timeout time.Duration) (*QQTSectionServicesTracer, error) {
	return installQQTSectionReloadTracer(pid, timeout, false, false, 0, 0, 0)
}

// InstallQQTSectionTransportTracer records the two services resolved by the
// vtable +0x2C send method: its packet-routing helper and actual transport.
func InstallQQTSectionTransportTracer(pid uint32, timeout time.Duration) (*QQTSectionServicesTracer, error) {
	return installQQTSectionReloadTracer(pid, timeout, true, false, 0, 0, 0)
}

// InstallQQTSectionPreconnectTracer performs a reversible, reload-safe Stage C
// diagnostic. On the original UI thread it asks NetCenter to create the game
// server connection, binds the newly appended connection object to serverID,
// and then resumes QQTSection's unmodified login-packet send path.
//
// serverIP uses the in_addr representation expected by Winsock on x86 (for
// example 127.0.0.1 is 0x0100007F).
func InstallQQTSectionPreconnectTracer(pid uint32, timeout time.Duration, serverID, serverIP uint32, serverPort uint16) (*QQTSectionServicesTracer, error) {
	return installQQTSectionReloadTracer(pid, timeout, true, true, serverID, serverIP, serverPort)
}

func installQQTSectionReloadTracer(pid uint32, timeout time.Duration, transportMode, preconnectMode bool, serverID, serverIP uint32, serverPort uint16) (*QQTSectionServicesTracer, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &QQTSectionServicesTracer{
		pid: pid, process: process, started: time.Now(), transportMode: transportMode,
		preconnectMode: preconnectMode, serverID: serverID, serverIP: serverIP, serverPort: serverPort,
	}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()
	moduleBase, err := waitForModule(process, pid, "QQTModules.dll", timeout)
	if err != nil {
		return nil, err
	}
	tracer.gatePatchAddress = moduleBase + qqtModulesStageCNetworkRVA
	tracer.gateOriginal, _ = readRemote(process, tracer.gatePatchAddress, len(qqtModulesStageCNetworkSignature))
	if len(tracer.gateOriginal) != len(qqtModulesStageCNetworkSignature) ||
		!equalBytes(tracer.gateOriginal, qqtModulesStageCNetworkSignature) {
		return nil, fmt.Errorf("QQTModules reload gate signature mismatch at 0x%08X: got %X", tracer.gatePatchAddress, tracer.gateOriginal)
	}
	page, _, allocErr := procVirtualAllocEx.Call(
		uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite,
	)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx QQTSection reload gate: 0x%X (%v)", page, allocErr)
	}
	tracer.gateStubAddress = page
	tracer.gateRecord = page + 0x300
	stub := buildQQTSectionReloadGateStub(
		page, tracer.gateRecord, moduleBase+qqtModulesStageCResumeRVA, tracer.gateOriginal,
	)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write QQTSection reload gate stub: %w", err)
	}
	patch := []byte{0xE9, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(patch[1:5], uint32(page-(tracer.gatePatchAddress+5)))
	if !ensureRemoteBytes(process, tracer.gatePatchAddress, patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch QQTSection reload gate at 0x%08X", tracer.gatePatchAddress)
	}
	procFlushInstruction.Call(uintptr(process), tracer.gatePatchAddress, uintptr(len(patch)))
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	failed = false
	return tracer, nil
}

func buildQQTSectionReloadGateStub(page, record, resume uintptr, original []byte) []byte {
	stub := []byte{0x9C, 0x60}
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record))
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+4))
	stub = append(stub, 0xF3, 0x90, 0x83, 0x3D) // pause; cmp dword ptr [release],0
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+60))
	stub = append(stub, 0x00, 0x74, 0xF5) // loop back through PAUSE while zero
	stub = append(stub, 0x61, 0x9D)
	stub = append(stub, original...)
	return appendRelativeJump(stub, page+uintptr(len(stub)), resume)
}

func (tracer *QQTSectionServicesTracer) armReloadedSection() error {
	moduleBase, err := waitForModule(tracer.process, tracer.pid, "QQTSection.dll", 5*time.Second)
	if err != nil {
		return err
	}
	patchRVA := qqtSectionServicesRVA
	resumeRVA := qqtSectionServicesResumeRVA
	signature := qqtSectionServicesSignature
	if tracer.transportMode {
		patchRVA = qqtSectionTransportRVA
		resumeRVA = qqtSectionTransportResumeRVA
		signature = qqtSectionTransportSignature
	}
	tracer.patchAddress = moduleBase + patchRVA
	tracer.original, _ = readRemote(tracer.process, tracer.patchAddress, len(signature))
	if len(tracer.original) != len(signature) ||
		!equalBytes(tracer.original, signature) {
		return fmt.Errorf("QQTSection reloaded signature mismatch at 0x%08X: got %X", tracer.patchAddress, tracer.original)
	}
	page, _, allocErr := procVirtualAllocEx.Call(
		uintptr(tracer.process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite,
	)
	if page == 0 || page > 0xFFFFFFFF {
		return fmt.Errorf("VirtualAllocEx reloaded QQTSection trace: 0x%X (%v)", page, allocErr)
	}
	tracer.stubAddress = page
	tracer.record = page + 0x300
	var stub []byte
	if tracer.preconnectMode {
		stub = buildQQTSectionPreconnectStub(
			page, tracer.record, moduleBase+resumeRVA, tracer.original,
			tracer.serverID, tracer.serverIP, tracer.serverPort,
		)
	} else if tracer.transportMode {
		stub = buildQQTSectionTransportTraceStub(page, tracer.record, moduleBase+resumeRVA, tracer.original)
	} else {
		stub = buildQQTSectionServicesTraceStub(page, tracer.record, moduleBase+resumeRVA, tracer.original)
	}
	if err := writeRemote(tracer.process, page, stub); err != nil {
		return fmt.Errorf("write reloaded QQTSection services trace stub: %w", err)
	}
	patch := []byte{0xE9, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(patch[1:5], uint32(page-(tracer.patchAddress+5)))
	if !ensureRemoteBytes(tracer.process, tracer.patchAddress, patch, pageExecuteReadWrite) {
		return fmt.Errorf("patch reloaded QQTSection services at 0x%08X", tracer.patchAddress)
	}
	procFlushInstruction.Call(uintptr(tracer.process), tracer.patchAddress, uintptr(len(patch)))
	procFlushInstruction.Call(uintptr(tracer.process), page, uintptr(len(stub)))
	tracer.reloadArmed = true
	return nil
}

func buildQQTSectionServicesTraceStub(page, record, resume uintptr, original []byte) []byte {
	stub := []byte{0x9C, 0x60}
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record))
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+4))
	stub = append(stub, 0x8B, 0x44, 0x24, 0x08, 0xA3) // saved EBP
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+8))
	for _, field := range []struct {
		ebpOffset byte
		record    uintptr
	}{{0xE4, record + 16}, {0xD0, record + 20}, {0xD8, record + 24}, {0xD4, record + 28},
		{0x0C, record + 32}, {0x10, record + 36}, {0x14, record + 40}} {
		stub = append(stub, 0x8B, 0x50, field.ebpOffset, 0x89, 0x15)
		stub = binary.LittleEndian.AppendUint32(stub, uint32(field.record))
	}
	stub = append(stub, 0x8B, 0x44, 0x24, 0x04, 0xA3) // saved ESI/section object
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+12))
	stub = append(stub, 0x61, 0x9D)
	stub = append(stub, original...)
	return appendRelativeJump(stub, page+uintptr(len(stub)), resume)
}

func buildQQTSectionTransportTraceStub(page, record, resume uintptr, original []byte) []byte {
	stub := []byte{0x9C, 0x60}
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record))
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+4))
	stub = append(stub, 0x8B, 0x44, 0x24, 0x08, 0xA3) // saved EBP
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+8))
	stub = append(stub, 0x8B, 0x4C, 0x24, 0x04, 0x89, 0x0D) // saved ESI/internal base
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+12))
	for _, field := range []struct {
		ebpOffset byte
		record    uintptr
	}{{0xF0, record + 16}, {0xEC, record + 20}, {0x0C, record + 24}} {
		stub = append(stub, 0x8B, 0x50, field.ebpOffset, 0x89, 0x15)
		stub = binary.LittleEndian.AppendUint32(stub, uint32(field.record))
	}
	stub = append(stub, 0x0F, 0xB7, 0x91, 0x92, 0x01, 0x00, 0x00, 0x89, 0x15)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+28))
	stub = append(stub, 0x61, 0x9D)
	stub = append(stub, original...)
	return appendRelativeJump(stub, page+uintptr(len(stub)), resume)
}

func buildQQTSectionPreconnectStub(page, record, resume uintptr, original []byte, serverID, serverIP uint32, serverPort uint16) []byte {
	stub := []byte{0x9C, 0x60} // pushfd; pushad
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record)) // calls
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+4)) // thread id
	stub = append(stub, 0x89, 0x2D)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+8)) // frame pointer
	stub = append(stub, 0x8B, 0x75, 0xEC, 0x89, 0x35)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+16)) // transport interface
	stub = append(stub, 0x85, 0xF6, 0x0F, 0x84)
	noTransportJump := len(stub)
	stub = append(stub, 0, 0, 0, 0)
	stub = append(stub, 0x8B, 0x06) // eax=[transport vtable]
	stub = append(stub, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(serverPort))
	stub = append(stub, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, serverIP)
	stub = append(stub, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, serverID)
	stub = append(stub, 0x56, 0xFF, 0x90, 0x58, 0x01, 0x00, 0x00) // push esi; call [eax+158h]
	stub = append(stub, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+32)) // HRESULT

	// transport is an exported interface at core+0x24. The connection list is
	// an MFC list at core+0xC8; +0xD0 is its tail node and node+8 is the object.
	// The create method has just AddTail'd this connection, so binding the tail
	// avoids touching an older directory or login connection.
	stub = append(stub, 0x8D, 0x56, 0xDC, 0x8B, 0x92, 0xD0, 0x00, 0x00, 0x00) // edx=transport-24h; edx=[edx+D0h]
	stub = append(stub, 0x85, 0xD2, 0x0F, 0x84)
	noNodeJump := len(stub)
	stub = append(stub, 0, 0, 0, 0)
	stub = append(stub, 0x8B, 0x52, 0x08, 0x89, 0x15)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+36)) // connection object
	stub = append(stub, 0x85, 0xD2, 0x0F, 0x84)
	noObjectJump := len(stub)
	stub = append(stub, 0, 0, 0, 0)
	stub = append(stub, 0x0F, 0xB7, 0x42, 0x10, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+40)) // old connection ID
	stub = append(stub, 0x66, 0xC7, 0x42, 0x10)
	stub = binary.LittleEndian.AppendUint16(stub, uint16(serverID))
	stub = append(stub, 0x0F, 0xB7, 0x42, 0x10, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+44)) // new connection ID

	done := len(stub)
	for _, immediate := range []int{noTransportJump, noNodeJump, noObjectJump} {
		binary.LittleEndian.PutUint32(stub[immediate:immediate+4], uint32(int32(done-(immediate+4))))
	}
	stub = append(stub, 0xC7, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+48))
	stub = binary.LittleEndian.AppendUint32(stub, 1) // completed after the potentially asynchronous connect call returns
	stub = append(stub, 0x61, 0x9D)
	stub = append(stub, original...)
	return appendRelativeJump(stub, page+uintptr(len(stub)), resume)
}

// InstallQQTSectionSendTracer holds QQTModules across the per-attempt
// QQTSection reload, then records the fully assembled login packet immediately
// before QQTSection dispatches it through its vtable +0x2C transport method.
func InstallQQTSectionSendTracer(pid uint32, timeout time.Duration) (*QQTSectionSendTracer, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &QQTSectionSendTracer{pid: pid, process: process, started: time.Now()}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()
	moduleBase, err := waitForModule(process, pid, "QQTModules.dll", timeout)
	if err != nil {
		return nil, err
	}
	tracer.gatePatchAddress = moduleBase + qqtModulesStageCNetworkRVA
	tracer.gateOriginal, _ = readRemote(process, tracer.gatePatchAddress, len(qqtModulesStageCNetworkSignature))
	if len(tracer.gateOriginal) != len(qqtModulesStageCNetworkSignature) ||
		!equalBytes(tracer.gateOriginal, qqtModulesStageCNetworkSignature) {
		return nil, fmt.Errorf("QQTModules send reload gate signature mismatch at 0x%08X: got %X", tracer.gatePatchAddress, tracer.gateOriginal)
	}
	page, _, allocErr := procVirtualAllocEx.Call(
		uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite,
	)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx QQTSection send reload gate: 0x%X (%v)", page, allocErr)
	}
	tracer.gateStubAddress = page
	tracer.gateRecord = page + 0x300
	stub := buildQQTSectionReloadGateStub(
		page, tracer.gateRecord, moduleBase+qqtModulesStageCResumeRVA, tracer.gateOriginal,
	)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write QQTSection send reload gate stub: %w", err)
	}
	patch := []byte{0xE9, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(patch[1:5], uint32(page-(tracer.gatePatchAddress+5)))
	if !ensureRemoteBytes(process, tracer.gatePatchAddress, patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch QQTSection send reload gate at 0x%08X", tracer.gatePatchAddress)
	}
	procFlushInstruction.Call(uintptr(process), tracer.gatePatchAddress, uintptr(len(patch)))
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	failed = false
	return tracer, nil
}

func (tracer *QQTSectionSendTracer) armReloadedSection() error {
	moduleBase, err := waitForModule(tracer.process, tracer.pid, "QQTSection.dll", 5*time.Second)
	if err != nil {
		return err
	}
	tracer.patchAddress = moduleBase + qqtSectionSendRVA
	tracer.original, _ = readRemote(tracer.process, tracer.patchAddress, len(qqtSectionSendSignature))
	if len(tracer.original) != len(qqtSectionSendSignature) ||
		!equalBytes(tracer.original, qqtSectionSendSignature) {
		return fmt.Errorf("QQTSection reloaded send signature mismatch at 0x%08X: got %X", tracer.patchAddress, tracer.original)
	}
	page, _, allocErr := procVirtualAllocEx.Call(
		uintptr(tracer.process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite,
	)
	if page == 0 || page > 0xFFFFFFFF {
		return fmt.Errorf("VirtualAllocEx reloaded QQTSection send trace: 0x%X (%v)", page, allocErr)
	}
	tracer.stubAddress = page
	tracer.record = page + 0x300
	stub := buildQQTSectionSendTraceStub(page, tracer.record, moduleBase+qqtSectionSendResumeRVA, tracer.original)
	if err := writeRemote(tracer.process, page, stub); err != nil {
		return fmt.Errorf("write reloaded QQTSection send trace stub: %w", err)
	}
	patch := []byte{0xE9, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(patch[1:5], uint32(page-(tracer.patchAddress+5)))
	if !ensureRemoteBytes(tracer.process, tracer.patchAddress, patch, pageExecuteReadWrite) {
		return fmt.Errorf("patch reloaded QQTSection send at 0x%08X", tracer.patchAddress)
	}
	procFlushInstruction.Call(uintptr(tracer.process), tracer.patchAddress, uintptr(len(patch)))
	procFlushInstruction.Call(uintptr(tracer.process), page, uintptr(len(stub)))
	tracer.reloadArmed = true
	return nil
}

func buildQQTSectionSendTraceStub(page, record, resume uintptr, original []byte) []byte {
	stub := []byte{0x9C, 0x60}
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record))
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+4))
	stub = append(stub, 0x8B, 0x44, 0x24, 0x08, 0xA3) // saved EBP
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+8))
	stub = append(stub, 0x8B, 0x74, 0x24, 0x04, 0x89, 0x35) // saved ESI/section object
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+12))
	stub = append(stub, 0x8B, 0x0E, 0x89, 0x0D) // section vtable
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+16))
	stub = append(stub, 0x8B, 0x51, 0x2C, 0x89, 0x15) // vtable +0x2C method
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+20))
	stub = append(stub, 0x8B, 0x44, 0x24, 0x08, 0x2D, 0x10, 0x02, 0x00, 0x00, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+24)) // EBP-0x210 packet
	stub = append(stub, 0x89, 0xC6, 0xBF)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+64))
	stub = append(stub, 0xB9)
	stub = binary.LittleEndian.AppendUint32(stub, qqtSectionLoginPacketSize/4)
	stub = append(stub, 0xFC, 0xF3, 0xA5, 0xA4) // cld; rep movsd; movsb
	stub = append(stub, 0x61, 0x9D)
	stub = append(stub, original...)
	return appendRelativeJump(stub, page+uintptr(len(stub)), resume)
}

func (tracer *ClientJoinTracer) CaptureUntilFirst(duration time.Duration) ClientJoinCapture {
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		record, ok := readRemote(tracer.process, tracer.record, clientJoinRecordSize)
		if ok && binary.LittleEndian.Uint32(record[:4]) != 0 {
			return tracer.capture(record)
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	record, _ := readRemote(tracer.process, tracer.record, clientJoinRecordSize)
	return tracer.capture(record)
}

func (tracer *ClientJoinTracer) capture(record []byte) ClientJoinCapture {
	result := ClientJoinCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		PatchAddress:  fmt.Sprintf("0x%08X", tracer.patchAddress),
		StubAddress:   fmt.Sprintf("0x%08X", tracer.stubAddress),
		OriginalBytes: fmt.Sprintf("%X", tracer.original),
		Behavior:      "transparent Client request_LoginSection entry: record caller, five scalar arguments, and service locator",
	}
	if len(record) != clientJoinRecordSize {
		return result
	}
	result.Calls = binary.LittleEndian.Uint32(record[0:4])
	result.ThreadID = binary.LittleEndian.Uint32(record[4:8])
	result.Caller = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[8:12]))
	for offset := 12; offset < 32; offset += 4 {
		result.Arguments = append(result.Arguments, fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[offset:offset+4])))
	}
	result.ServiceLocator = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[32:36]))
	return result
}

func (tracer *ClientJoinTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	if tracer.patchAddress != 0 && len(tracer.original) != 0 {
		ensureRemoteBytes(tracer.process, tracer.patchAddress, tracer.original, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(tracer.process), tracer.patchAddress, uintptr(len(tracer.original)))
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}

func (tracer *ClientJoinStateTracer) CaptureUntilFirst(duration time.Duration) ClientJoinStateCapture {
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		record, ok := readRemote(tracer.process, tracer.record, clientJoinRecordSize)
		if ok && binary.LittleEndian.Uint32(record[:4]) != 0 {
			return tracer.capture(record)
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	record, _ := readRemote(tracer.process, tracer.record, clientJoinRecordSize)
	return tracer.capture(record)
}

func (tracer *ClientJoinStateTracer) capture(record []byte) ClientJoinStateCapture {
	result := ClientJoinStateCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		PatchAddress:  fmt.Sprintf("0x%08X", tracer.patchAddress),
		StubAddress:   fmt.Sprintf("0x%08X", tracer.stubAddress),
		OriginalBytes: fmt.Sprintf("%X", tracer.original),
		Behavior:      "transparent request_LoginSection post-lookup state: record required interfaces, UI script, guide values, and arguments",
	}
	if len(record) != clientJoinRecordSize {
		return result
	}
	result.Calls = binary.LittleEndian.Uint32(record[0:4])
	result.ThreadID = binary.LittleEndian.Uint32(record[4:8])
	result.FramePointer = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[8:12]))
	interfaceA := binary.LittleEndian.Uint32(record[12:16])
	interfaceB := binary.LittleEndian.Uint32(record[16:20])
	uiScript := binary.LittleEndian.Uint32(record[20:24])
	result.InterfaceA = fmt.Sprintf("0x%08X", interfaceA)
	result.InterfaceB = fmt.Sprintf("0x%08X", interfaceB)
	result.UIScript = fmt.Sprintf("0x%08X", uiScript)
	result.InterfaceAVTable, result.InterfaceAMethods = tracer.readInterface(interfaceA, 24)
	result.InterfaceBVTable, result.InterfaceBMethods = tracer.readInterface(interfaceB, 24)
	result.UIScriptVTable, result.UIScriptMethods = tracer.readInterface(uiScript, 8)
	for offset := 24; offset < 32; offset += 4 {
		result.GuideValues = append(result.GuideValues, fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[offset:offset+4])))
	}
	for offset := 32; offset < 52; offset += 4 {
		result.Arguments = append(result.Arguments, fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[offset:offset+4])))
	}
	return result
}

func (tracer *ClientJoinStateTracer) readInterface(object uint32, count int) (string, []string) {
	if object == 0 {
		return "0x00000000", nil
	}
	pointer, ok := readRemote(tracer.process, uintptr(object), 4)
	if !ok {
		return "0x00000000", nil
	}
	vtable := binary.LittleEndian.Uint32(pointer)
	methods, ok := readRemote(tracer.process, uintptr(vtable), count*4)
	if !ok {
		return fmt.Sprintf("0x%08X", vtable), nil
	}
	values := make([]string, 0, count)
	for offset := 0; offset < len(methods); offset += 4 {
		values = append(values, fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(methods[offset:offset+4])))
	}
	return fmt.Sprintf("0x%08X", vtable), values
}

func (tracer *ClientJoinStateTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	if tracer.patchAddress != 0 && len(tracer.original) != 0 {
		ensureRemoteBytes(tracer.process, tracer.patchAddress, tracer.original, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(tracer.process), tracer.patchAddress, uintptr(len(tracer.original)))
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}

func (tracer *ClientJoinOutputTracer) CaptureUntilFirst(duration time.Duration) ClientJoinOutputCapture {
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		record, ok := readRemote(tracer.process, tracer.record, clientJoinOutputRecordSize)
		if ok && binary.LittleEndian.Uint32(record[:4]) != 0 {
			return tracer.capture(record)
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	record, _ := readRemote(tracer.process, tracer.record, clientJoinOutputRecordSize)
	return tracer.capture(record)
}

func (tracer *ClientJoinOutputTracer) capture(record []byte) ClientJoinOutputCapture {
	result := ClientJoinOutputCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		PatchAddress:  fmt.Sprintf("0x%08X", tracer.patchAddress),
		StubAddress:   fmt.Sprintf("0x%08X", tracer.stubAddress),
		OriginalBytes: fmt.Sprintf("%X", tracer.original),
		Behavior:      "transparent request_LoginSection post-QQTDir output: copy the local selection frame and replay the displaced instruction",
	}
	if len(record) != clientJoinOutputRecordSize {
		return result
	}
	result.Calls = binary.LittleEndian.Uint32(record[0:4])
	result.ThreadID = binary.LittleEndian.Uint32(record[4:8])
	result.FramePointer = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[8:12]))
	result.MethodResult = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[12:16]))
	result.InterfaceA = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[16:20]))
	result.InterfaceB = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[20:24]))
	result.FrameStart = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[24:28]))
	frame := record[64:]
	result.SelectionValueA = binary.LittleEndian.Uint16(frame[0x14:0x16])
	result.SelectionValueB = binary.LittleEndian.Uint16(frame[0x18:0x1A])
	result.SelectionValueAHex = fmt.Sprintf("0x%04X", result.SelectionValueA)
	result.SelectionValueBHex = fmt.Sprintf("0x%04X", result.SelectionValueB)
	result.FrameHex = fmt.Sprintf("%X", frame)
	return result
}

func (tracer *ClientJoinOutputTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	if tracer.patchAddress != 0 && len(tracer.original) != 0 {
		ensureRemoteBytes(tracer.process, tracer.patchAddress, tracer.original, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(tracer.process), tracer.patchAddress, uintptr(len(tracer.original)))
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}

func (tracer *QQTModulesStageCNetworkTracer) CaptureUntilFirst(duration time.Duration) QQTModulesStageCNetworkCapture {
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		record, ok := readRemote(tracer.process, tracer.record, clientJoinRecordSize)
		if ok && binary.LittleEndian.Uint32(record[:4]) != 0 {
			return tracer.capture(record)
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	record, _ := readRemote(tracer.process, tracer.record, clientJoinRecordSize)
	return tracer.capture(record)
}

func (tracer *QQTModulesStageCNetworkTracer) capture(record []byte) QQTModulesStageCNetworkCapture {
	result := QQTModulesStageCNetworkCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		PatchAddress:  fmt.Sprintf("0x%08X", tracer.patchAddress),
		StubAddress:   fmt.Sprintf("0x%08X", tracer.stubAddress),
		OriginalBytes: fmt.Sprintf("%X", tracer.original),
		Behavior:      "transparent QQTModules Stage C login-interface gate: record resolved interface and original section/server arguments",
	}
	if len(record) != clientJoinRecordSize {
		return result
	}
	result.Calls = binary.LittleEndian.Uint32(record[0:4])
	result.ThreadID = binary.LittleEndian.Uint32(record[4:8])
	result.FramePointer = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[8:12]))
	result.LoginObject = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[12:16]))
	loginInterface := binary.LittleEndian.Uint32(record[16:20])
	result.LoginInterface = fmt.Sprintf("0x%08X", loginInterface)
	result.ServiceLocator = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[20:24]))
	result.ArgumentA = binary.LittleEndian.Uint32(record[24:28])
	result.ArgumentB = binary.LittleEndian.Uint32(record[28:32])
	result.SocketCounter = binary.LittleEndian.Uint32(record[32:36])
	result.LastSwitchTick = binary.LittleEndian.Uint32(record[36:40])
	result.SwitchIntervalMS = binary.LittleEndian.Uint32(record[40:44])
	result.LoginConfig = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[44:48]))
	result.InterfaceVTable, result.InterfaceMethods = readRemoteInterface(tracer.process, loginInterface, 16)
	return result
}

func readRemoteInterface(process syscall.Handle, object uint32, count int) (string, []string) {
	if object == 0 {
		return "0x00000000", nil
	}
	pointer, ok := readRemote(process, uintptr(object), 4)
	if !ok {
		return "0x00000000", nil
	}
	vtable := binary.LittleEndian.Uint32(pointer)
	methods, ok := readRemote(process, uintptr(vtable), count*4)
	if !ok {
		return fmt.Sprintf("0x%08X", vtable), nil
	}
	values := make([]string, 0, count)
	for offset := 0; offset < len(methods); offset += 4 {
		values = append(values, fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(methods[offset:offset+4])))
	}
	return fmt.Sprintf("0x%08X", vtable), values
}

func (tracer *QQTModulesStageCNetworkTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	if tracer.patchAddress != 0 && len(tracer.original) != 0 {
		ensureRemoteBytes(tracer.process, tracer.patchAddress, tracer.original, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(tracer.process), tracer.patchAddress, uintptr(len(tracer.original)))
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}

func (tracer *QQTSectionServicesTracer) CaptureUntilFirst(duration time.Duration) QQTSectionServicesCapture {
	deadline := time.Now().Add(duration)
	gateCalls := uint32(0)
	for time.Now().Before(deadline) {
		record, ok := readRemote(tracer.process, tracer.gateRecord, clientJoinRecordSize)
		if ok {
			gateCalls = binary.LittleEndian.Uint32(record[:4])
			if gateCalls != 0 {
				break
			}
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if gateCalls == 0 {
		return tracer.capture(nil, 0)
	}
	if err := tracer.armReloadedSection(); err != nil {
		tracer.armError = err.Error()
	}
	release := make([]byte, 4)
	binary.LittleEndian.PutUint32(release, 1)
	_ = writeRemote(tracer.process, tracer.gateRecord+60, release)
	if tracer.armError != "" {
		return tracer.capture(nil, gateCalls)
	}
	for time.Now().Before(deadline) {
		record, ok := readRemote(tracer.process, tracer.record, clientJoinRecordSize)
		complete := ok && binary.LittleEndian.Uint32(record[:4]) != 0
		if tracer.preconnectMode {
			complete = ok && binary.LittleEndian.Uint32(record[48:52]) != 0
		}
		if complete {
			return tracer.capture(record, gateCalls)
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	record, _ := readRemote(tracer.process, tracer.record, clientJoinRecordSize)
	return tracer.capture(record, gateCalls)
}

func (tracer *QQTSectionServicesTracer) capture(record []byte, gateCalls uint32) QQTSectionServicesCapture {
	behavior := "reload-safe QQTSection Stage C dependency gate: hold QQTModules after LoadLibrary, arm the reloaded module, then record all four services"
	if tracer.transportMode {
		behavior = "reload-safe QQTSection Stage C transport gate: record packet-routing and transport services before the final network call"
	}
	if tracer.preconnectMode {
		behavior = "reload-safe QQTSection Stage C preconnect: create the configured NetCenter connection, bind its route ID, then resume the original send path"
	}
	result := QQTSectionServicesCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		GateCalls:     gateCalls,
		ReloadArmed:   tracer.reloadArmed,
		ArmError:      tracer.armError,
		GateAddress:   fmt.Sprintf("0x%08X", tracer.gatePatchAddress),
		PatchAddress:  fmt.Sprintf("0x%08X", tracer.patchAddress),
		StubAddress:   fmt.Sprintf("0x%08X", tracer.stubAddress),
		OriginalBytes: fmt.Sprintf("%X", tracer.original),
		Behavior:      behavior,
	}
	if len(record) != clientJoinRecordSize {
		return result
	}
	result.Calls = binary.LittleEndian.Uint32(record[0:4])
	result.ThreadID = binary.LittleEndian.Uint32(record[4:8])
	result.FramePointer = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[8:12]))
	internalBase := binary.LittleEndian.Uint32(record[12:16])
	if tracer.preconnectMode {
		result.ServerID = tracer.serverID
		result.ServerIP = fmt.Sprintf("%d.%d.%d.%d", byte(tracer.serverIP), byte(tracer.serverIP>>8), byte(tracer.serverIP>>16), byte(tracer.serverIP>>24))
		result.ServerPort = tracer.serverPort
		result.CreateResult = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[32:36]))
		result.Connection = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[36:40]))
		result.IDBefore = binary.LittleEndian.Uint16(record[40:42])
		result.IDAfter = binary.LittleEndian.Uint16(record[44:46])
		transport := binary.LittleEndian.Uint32(record[16:20])
		vtable, methods := readRemoteInterface(tracer.process, transport, 88)
		result.Services = append(result.Services, QQTSectionResolvedService{
			Name: "transport_service", Object: fmt.Sprintf("0x%08X", transport), VTable: vtable, Methods: methods,
		})
		return result
	}
	if tracer.transportMode {
		result.SectionObject = fmt.Sprintf("0x%08X", internalBase+0xBF30)
		for index, name := range []string{"packet_routing_service", "transport_service"} {
			object := binary.LittleEndian.Uint32(record[16+index*4 : 20+index*4])
			vtable, methods := readRemoteInterface(tracer.process, object, 20)
			result.Services = append(result.Services, QQTSectionResolvedService{
				Name: name, Object: fmt.Sprintf("0x%08X", object), VTable: vtable, Methods: methods,
			})
		}
		result.PacketAddress = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[24:28]))
		result.SessionWord = binary.LittleEndian.Uint16(record[28:30])
		return result
	}
	result.SectionObject = fmt.Sprintf("0x%08X", internalBase)
	for index, name := range []string{"service_a", "service_b", "service_c", "service_d"} {
		object := binary.LittleEndian.Uint32(record[16+index*4 : 20+index*4])
		vtable, methods := readRemoteInterface(tracer.process, object, 12)
		result.Services = append(result.Services, QQTSectionResolvedService{
			Name: name, Object: fmt.Sprintf("0x%08X", object), VTable: vtable, Methods: methods,
		})
	}
	result.ArgumentA = binary.LittleEndian.Uint32(record[32:36])
	result.ArgumentB = binary.LittleEndian.Uint32(record[36:40])
	result.SocketCounter = binary.LittleEndian.Uint32(record[40:44])
	return result
}

func (tracer *QQTSectionServicesTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	if tracer.gateRecord != 0 {
		release := make([]byte, 4)
		binary.LittleEndian.PutUint32(release, 1)
		_ = writeRemote(tracer.process, tracer.gateRecord+60, release)
	}
	if tracer.patchAddress != 0 && len(tracer.original) != 0 {
		ensureRemoteBytes(tracer.process, tracer.patchAddress, tracer.original, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(tracer.process), tracer.patchAddress, uintptr(len(tracer.original)))
	}
	if tracer.gatePatchAddress != 0 && len(tracer.gateOriginal) != 0 {
		ensureRemoteBytes(tracer.process, tracer.gatePatchAddress, tracer.gateOriginal, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(tracer.process), tracer.gatePatchAddress, uintptr(len(tracer.gateOriginal)))
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}

func (tracer *QQTSectionSendTracer) CaptureUntilFirst(duration time.Duration) QQTSectionSendCapture {
	deadline := time.Now().Add(duration)
	gateCalls := uint32(0)
	for time.Now().Before(deadline) {
		record, ok := readRemote(tracer.process, tracer.gateRecord, clientJoinRecordSize)
		if ok {
			gateCalls = binary.LittleEndian.Uint32(record[:4])
			if gateCalls != 0 {
				break
			}
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if gateCalls == 0 {
		return tracer.capture(nil, 0)
	}
	if err := tracer.armReloadedSection(); err != nil {
		tracer.armError = err.Error()
	}
	release := make([]byte, 4)
	binary.LittleEndian.PutUint32(release, 1)
	_ = writeRemote(tracer.process, tracer.gateRecord+60, release)
	if tracer.armError != "" {
		return tracer.capture(nil, gateCalls)
	}
	for time.Now().Before(deadline) {
		record, ok := readRemote(tracer.process, tracer.record, qqtSectionSendRecordSize)
		if ok && binary.LittleEndian.Uint32(record[:4]) != 0 {
			return tracer.capture(record, gateCalls)
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	record, _ := readRemote(tracer.process, tracer.record, qqtSectionSendRecordSize)
	return tracer.capture(record, gateCalls)
}

func (tracer *QQTSectionSendTracer) capture(record []byte, gateCalls uint32) QQTSectionSendCapture {
	result := QQTSectionSendCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		GateCalls:     gateCalls,
		ReloadArmed:   tracer.reloadArmed,
		ArmError:      tracer.armError,
		GateAddress:   fmt.Sprintf("0x%08X", tracer.gatePatchAddress),
		PatchAddress:  fmt.Sprintf("0x%08X", tracer.patchAddress),
		StubAddress:   fmt.Sprintf("0x%08X", tracer.stubAddress),
		OriginalBytes: fmt.Sprintf("%X", tracer.original),
		Behavior:      "reload-safe QQTSection Stage C send trace: capture the assembled login packet and vtable +0x2C transport target before dispatch",
	}
	if len(record) != qqtSectionSendRecordSize {
		return result
	}
	result.Calls = binary.LittleEndian.Uint32(record[0:4])
	result.ThreadID = binary.LittleEndian.Uint32(record[4:8])
	result.FramePointer = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[8:12]))
	sectionObject := binary.LittleEndian.Uint32(record[12:16])
	result.SectionObject = fmt.Sprintf("0x%08X", sectionObject)
	result.SectionVTable = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[16:20]))
	result.SendMethod = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[20:24]))
	result.PacketAddress = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[24:28]))
	result.PacketHex = fmt.Sprintf("%X", record[64:])
	_, result.VTableMethods = readRemoteInterface(tracer.process, sectionObject, 16)
	return result
}

func (tracer *QQTSectionSendTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	if tracer.gateRecord != 0 {
		release := make([]byte, 4)
		binary.LittleEndian.PutUint32(release, 1)
		_ = writeRemote(tracer.process, tracer.gateRecord+60, release)
	}
	if tracer.patchAddress != 0 && len(tracer.original) != 0 {
		ensureRemoteBytes(tracer.process, tracer.patchAddress, tracer.original, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(tracer.process), tracer.patchAddress, uintptr(len(tracer.original)))
	}
	if tracer.gatePatchAddress != 0 && len(tracer.gateOriginal) != 0 {
		ensureRemoteBytes(tracer.process, tracer.gatePatchAddress, tracer.gateOriginal, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(tracer.process), tracer.gatePatchAddress, uintptr(len(tracer.gateOriginal)))
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}
