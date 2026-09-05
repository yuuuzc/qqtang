package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

const (
	netCenterDealConnectRVA         = uintptr(0x9838)
	netCenterDealConnectResultRVA   = uintptr(0x1C39C)
	netCenterPooledConnectResultRVA = uintptr(0x14EA1)
	netCenterDealRequestRVA         = uintptr(0x1D839)
	netCenterMessageRequestRVA      = uintptr(0x11E12)
	shopDealTraceRecordSize         = 52
	shopMessageTraceRecordSize      = 64
	shopDealTraceConnectRecordOff   = uintptr(0x300)
	shopDealTraceCallbackRecordOff  = uintptr(0x340)
	shopDealTracePooledRecordOff    = uintptr(0x380)
	shopDealTraceRequestRecordOff   = uintptr(0x3C0)
	shopMessageTraceRecordOff       = uintptr(0x400)
)

var (
	netCenterDealConnectSignature         = []byte{0x55, 0x8B, 0xEC, 0x51, 0x51, 0x56}
	netCenterDealConnectResultSignature   = []byte{0x55, 0x8B, 0xEC, 0x83, 0xEC, 0x0C}
	netCenterPooledConnectResultSignature = []byte{0x55, 0x8B, 0xEC, 0x51, 0x51, 0x53}
	netCenterDealRequestSignature         = []byte{0x55, 0x8B, 0xEC, 0xB8, 0xB0, 0x19, 0x00, 0x00}
	netCenterMessageRequestSignature      = []byte{0xB8, 0x00, 0x00, 0x00, 0x00}
)

type ShopDealTraceEvent struct {
	Calls       uint32 `json:"calls"`
	ThreadID    uint32 `json:"thread_id,omitempty"`
	Object      string `json:"object"`
	Argument1   string `json:"argument_1"`
	Argument2   string `json:"argument_2"`
	Argument3   string `json:"argument_3"`
	QueueWrite  uint32 `json:"queue_write"`
	QueueRead   uint32 `json:"queue_read"`
	Connected   uint32 `json:"connected"`
	RemoteIPv4  string `json:"remote_ipv4"`
	RemotePort  uint32 `json:"remote_port"`
	SocketToken uint32 `json:"socket_token"`
	VTable      string `json:"vtable"`
}

type ShopMessageTraceEvent struct {
	Calls          uint32 `json:"calls"`
	ThreadID       uint32 `json:"thread_id,omitempty"`
	Object         string `json:"object"`
	Request        string `json:"request"`
	Length         uint32 `json:"length"`
	Descriptor     string `json:"descriptor"`
	ServerID       uint32 `json:"server_id"`
	ConnectionType uint32 `json:"connection_type"`
	Route          uint32 `json:"route"`
	Schema         uint32 `json:"schema"`
	Command        uint32 `json:"command"`
	Flags          uint32 `json:"flags"`
	ServiceID      uint32 `json:"service_id"`
	IPv4           string `json:"ipv4"`
	PortPair       string `json:"port_pair"`
}

type ShopDealTraceCapture struct {
	PID                    uint32                `json:"pid"`
	ElapsedMS              int64                 `json:"elapsed_ms"`
	Connect                ShopDealTraceEvent    `json:"connect_entry"`
	ConnectResult          ShopDealTraceEvent    `json:"connect_result"`
	PostConnect            ShopDealTraceEvent    `json:"post_connect_object"`
	PooledConnectResult    ShopDealTraceEvent    `json:"pooled_connect_result"`
	DealRequest            ShopDealTraceEvent    `json:"deal_request"`
	MessageCommand         uint32                `json:"message_command"`
	MessageRequest         ShopMessageTraceEvent `json:"message_request"`
	ConnectPatch           string                `json:"connect_patch"`
	ResultPatch            string                `json:"connect_result_patch"`
	PooledResultPatch      string                `json:"pooled_connect_result_patch"`
	DealRequestPatch       string                `json:"deal_request_patch"`
	MessageRequestPatch    string                `json:"message_request_patch"`
	OriginalConnect        string                `json:"original_connect"`
	OriginalResult         string                `json:"original_connect_result"`
	OriginalPooledResult   string                `json:"original_pooled_connect_result"`
	OriginalDealRequest    string                `json:"original_deal_request"`
	OriginalMessageRequest string                `json:"original_message_request"`
	Behavior               string                `json:"behavior"`
}

type ShopDealTracer struct {
	pid                    uint32
	process                syscall.Handle
	started                time.Time
	page                   uintptr
	connectPatch           uintptr
	resultPatch            uintptr
	pooledResultPatch      uintptr
	dealRequestPatch       uintptr
	messageRequestPatch    uintptr
	connectRecord          uintptr
	resultRecord           uintptr
	pooledResultRecord     uintptr
	dealRequestRecord      uintptr
	messageRequestRecord   uintptr
	messageCommand         uint32
	originalConnect        []byte
	originalResult         []byte
	originalPooledResult   []byte
	originalDealRequest    []byte
	originalMessageRequest []byte
}

// InstallShopDealTracerForCommand records the same connection path as
// InstallShopDealTracer and captures the selected CQQTMsgMgr command. Keeping
// the command explicit lets protocol investigations observe purchase and other
// shop requests without changing the target client's behavior.
func InstallShopDealTracerForCommand(pid, command uint32, timeout time.Duration) (*ShopDealTracer, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	if command == 0 {
		return nil, fmt.Errorf("message command must be non-zero")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &ShopDealTracer{pid: pid, process: process, started: time.Now(), messageCommand: command}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()

	moduleBase, err := waitForModule(process, pid, "NetCenter.dll", timeout)
	if err != nil {
		return nil, err
	}
	tracer.connectPatch = moduleBase + netCenterDealConnectRVA
	tracer.resultPatch = moduleBase + netCenterDealConnectResultRVA
	tracer.pooledResultPatch = moduleBase + netCenterPooledConnectResultRVA
	tracer.dealRequestPatch = moduleBase + netCenterDealRequestRVA
	tracer.messageRequestPatch = moduleBase + netCenterMessageRequestRVA
	tracer.originalConnect, _ = readRemote(process, tracer.connectPatch, len(netCenterDealConnectSignature))
	tracer.originalResult, _ = readRemote(process, tracer.resultPatch, len(netCenterDealConnectResultSignature))
	tracer.originalPooledResult, _ = readRemote(process, tracer.pooledResultPatch, len(netCenterPooledConnectResultSignature))
	tracer.originalDealRequest, _ = readRemote(process, tracer.dealRequestPatch, len(netCenterDealRequestSignature))
	tracer.originalMessageRequest, _ = readRemote(process, tracer.messageRequestPatch, len(netCenterMessageRequestSignature))
	if !equalBytes(tracer.originalConnect, netCenterDealConnectSignature) {
		return nil, fmt.Errorf("CDealSocketObject connect signature mismatch at 0x%08X: got %X", tracer.connectPatch, tracer.originalConnect)
	}
	if !equalBytes(tracer.originalResult, netCenterDealConnectResultSignature) {
		return nil, fmt.Errorf("CDealSocketObject connect-result signature mismatch at 0x%08X: got %X", tracer.resultPatch, tracer.originalResult)
	}
	if !equalBytes(tracer.originalPooledResult, netCenterPooledConnectResultSignature) {
		return nil, fmt.Errorf("pooled socket connect-result signature mismatch at 0x%08X: got %X", tracer.pooledResultPatch, tracer.originalPooledResult)
	}
	if !equalBytes(tracer.originalDealRequest, netCenterDealRequestSignature) {
		return nil, fmt.Errorf("CDealSocketObject request signature mismatch at 0x%08X: got %X", tracer.dealRequestPatch, tracer.originalDealRequest)
	}
	messageRequestSignatureOK := len(tracer.originalMessageRequest) == len(netCenterMessageRequestSignature) &&
		tracer.originalMessageRequest[0] == 0xB8 &&
		binary.LittleEndian.Uint32(tracer.originalMessageRequest[1:]) == uint32(moduleBase+0x2C3EC)
	if !messageRequestSignatureOK {
		return nil, fmt.Errorf("CQQTMsgMgr request signature mismatch at 0x%08X: got %X", tracer.messageRequestPatch, tracer.originalMessageRequest)
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx shop deal trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page = page
	tracer.connectRecord = page + shopDealTraceConnectRecordOff
	tracer.resultRecord = page + shopDealTraceCallbackRecordOff
	tracer.pooledResultRecord = page + shopDealTracePooledRecordOff
	tracer.dealRequestRecord = page + shopDealTraceRequestRecordOff
	tracer.messageRequestRecord = page + shopMessageTraceRecordOff
	connectStubAddress := page
	resultStubAddress := page + 0x180
	pooledResultStubAddress := page + 0x500
	dealRequestStubAddress := page + 0x680
	messageRequestStubAddress := page + 0x800
	connectStub := buildShopDealTraceStub(connectStubAddress, tracer.connectRecord, tracer.connectPatch, tracer.originalConnect)
	resultStub := buildShopDealTraceStub(resultStubAddress, tracer.resultRecord, tracer.resultPatch, tracer.originalResult)
	pooledResultStub := buildShopDealTraceStub(pooledResultStubAddress, tracer.pooledResultRecord, tracer.pooledResultPatch, tracer.originalPooledResult)
	dealRequestStub := buildShopDealTraceStub(dealRequestStubAddress, tracer.dealRequestRecord, tracer.dealRequestPatch, tracer.originalDealRequest)
	messageRequestStub := buildShopMessageTraceStub(messageRequestStubAddress, tracer.messageRequestRecord, tracer.messageRequestPatch, tracer.originalMessageRequest, command)
	if err := writeRemote(process, connectStubAddress, connectStub); err != nil {
		return nil, fmt.Errorf("write shop deal connect trace: %w", err)
	}
	if err := writeRemote(process, resultStubAddress, resultStub); err != nil {
		return nil, fmt.Errorf("write shop deal connect-result trace: %w", err)
	}
	if err := writeRemote(process, pooledResultStubAddress, pooledResultStub); err != nil {
		return nil, fmt.Errorf("write pooled connect-result trace: %w", err)
	}
	if err := writeRemote(process, dealRequestStubAddress, dealRequestStub); err != nil {
		return nil, fmt.Errorf("write deal request trace: %w", err)
	}
	if err := writeRemote(process, messageRequestStubAddress, messageRequestStub); err != nil {
		return nil, fmt.Errorf("write message request trace: %w", err)
	}
	if err := patchShopDealTraceEntry(process, tracer.connectPatch, connectStubAddress, len(tracer.originalConnect)); err != nil {
		return nil, err
	}
	if err := patchShopDealTraceEntry(process, tracer.resultPatch, resultStubAddress, len(tracer.originalResult)); err != nil {
		return nil, err
	}
	if err := patchShopDealTraceEntry(process, tracer.pooledResultPatch, pooledResultStubAddress, len(tracer.originalPooledResult)); err != nil {
		return nil, err
	}
	if err := patchShopDealTraceEntry(process, tracer.dealRequestPatch, dealRequestStubAddress, len(tracer.originalDealRequest)); err != nil {
		return nil, err
	}
	if err := patchShopDealTraceEntry(process, tracer.messageRequestPatch, messageRequestStubAddress, len(tracer.originalMessageRequest)); err != nil {
		return nil, err
	}
	procFlushInstruction.Call(uintptr(process), page, 0x1000)
	failed = false
	return tracer, nil
}

func buildShopDealTraceStub(stubAddress, record, patchAddress uintptr, original []byte) []byte {
	stub := []byte{0x9C, 0x60}            // pushfd; pushad
	stub = append(stub, 0xF0, 0xFF, 0x05) // lock inc dword ptr [record]
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record))
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3) // thread ID
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+4))
	stub = append(stub, 0x8B, 0x44, 0x24, 0x18, 0xA3) // saved ECX = object
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+8))
	stub = append(stub, 0x8B, 0x54, 0x24, 0x0C) // saved entry ESP-4
	for sourceOffset, targetOffset := range map[byte]uintptr{0x08: 12, 0x0C: 16, 0x10: 20} {
		stub = append(stub, 0x8B, 0x42, sourceOffset, 0xA3)
		stub = binary.LittleEndian.AppendUint32(stub, uint32(record+targetOffset))
	}
	stub = append(stub, 0x8B, 0x54, 0x24, 0x18) // object again
	// Queue helpers receive &object[0x18], so their +0x2088/+0x208c
	// cursors live at +0x20a0/+0x20a4 in the complete CDeal object.
	for sourceOffset, targetOffset := range map[uint32]uintptr{0x20A0: 24, 0x20A4: 28, 0x20AC: 32, 0x08: 36} {
		stub = append(stub, 0x8B, 0x82)
		stub = binary.LittleEndian.AppendUint32(stub, sourceOffset)
		stub = append(stub, 0xA3)
		stub = binary.LittleEndian.AppendUint32(stub, uint32(record+targetOffset))
	}
	stub = append(stub, 0x0F, 0xB7, 0x42, 0x0C, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+40))
	stub = append(stub, 0x0F, 0xB7, 0x42, 0x0E, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+44))
	stub = append(stub, 0x8B, 0x02, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+48))
	stub = append(stub, 0x61, 0x9D)
	stub = append(stub, original...)
	return appendRelativeJump(stub, stubAddress+uintptr(len(stub)), patchAddress+uintptr(len(original)))
}

func buildShopMessageTraceStub(stubAddress, record, patchAddress uintptr, original []byte, command uint32) []byte {
	stub := []byte{0x9C, 0x60}                  // pushfd; pushad
	stub = append(stub, 0x8B, 0x54, 0x24, 0x0C) // saved entry ESP-4
	stub = append(stub, 0x8B, 0x42, 0x10)       // descriptor (third argument)
	stub = append(stub, 0x85, 0xC0, 0x0F, 0x84, 0, 0, 0, 0)
	skipNull := len(stub) - 4
	stub = append(stub, 0x81, 0x78, 0x10) // cmp dword ptr [descriptor+0x10], command
	stub = binary.LittleEndian.AppendUint32(stub, command)
	stub = append(stub, 0x0F, 0x85, 0, 0, 0, 0)
	skipOther := len(stub) - 4
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record))
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+4))
	stub = append(stub, 0x8B, 0x4C, 0x24, 0x18, 0x89, 0x0D) // saved ECX
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+8))
	stub = append(stub, 0x8B, 0x4A, 0x08, 0x89, 0x0D) // request
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+12))
	stub = append(stub, 0x8B, 0x4A, 0x0C, 0x89, 0x0D) // length
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+16))
	stub = append(stub, 0x8B, 0x42, 0x10) // reload descriptor after the thread-ID read clobbered EAX
	stub = append(stub, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+20))
	for sourceOffset, targetOffset := range map[byte]uintptr{
		0x00: 24, 0x04: 28, 0x08: 32, 0x0C: 36, 0x10: 40,
		0x14: 44, 0x1C: 48, 0x20: 52, 0x24: 56,
	} {
		stub = append(stub, 0x8B, 0x48, sourceOffset, 0x89, 0x0D)
		stub = binary.LittleEndian.AppendUint32(stub, uint32(record+targetOffset))
	}
	skip := len(stub)
	binary.LittleEndian.PutUint32(stub[skipNull:skipNull+4], uint32(skip-(skipNull+4)))
	binary.LittleEndian.PutUint32(stub[skipOther:skipOther+4], uint32(skip-(skipOther+4)))
	stub = append(stub, 0x61, 0x9D)
	stub = append(stub, original...)
	return appendRelativeJump(stub, stubAddress+uintptr(len(stub)), patchAddress+uintptr(len(original)))
}

func patchShopDealTraceEntry(process syscall.Handle, address, stub uintptr, size int) error {
	if size < 5 {
		return fmt.Errorf("shop deal trace patch size %d is below a near jump", size)
	}
	patch := make([]byte, size)
	patch[0] = 0xE9
	for index := 5; index < len(patch); index++ {
		patch[index] = 0x90
	}
	binary.LittleEndian.PutUint32(patch[1:5], uint32(stub-(address+5)))
	if !ensureRemoteBytes(process, address, patch, pageExecuteReadWrite) {
		return fmt.Errorf("patch shop deal trace entry at 0x%08X", address)
	}
	procFlushInstruction.Call(uintptr(process), address, uintptr(len(patch)))
	return nil
}

func (tracer *ShopDealTracer) CaptureAfterConnect(duration, settle time.Duration) ShopDealTraceCapture {
	if duration <= 0 {
		duration = 2 * time.Minute
	}
	if settle <= 0 {
		settle = 5 * time.Second
	}
	deadline := time.Now().Add(duration)
	var connectedAt time.Time
	for time.Now().Before(deadline) {
		connect, _ := readRemote(tracer.process, tracer.connectRecord, 4)
		result, _ := readRemote(tracer.process, tracer.resultRecord, 4)
		pooled, _ := readRemote(tracer.process, tracer.pooledResultRecord, 4)
		if (len(result) == 4 && binary.LittleEndian.Uint32(result) != 0) ||
			(len(pooled) == 4 && binary.LittleEndian.Uint32(pooled) != 0) {
			break
		}
		if len(connect) == 4 && binary.LittleEndian.Uint32(connect) != 0 {
			if connectedAt.IsZero() {
				connectedAt = time.Now()
			}
			if time.Since(connectedAt) >= settle {
				break
			}
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	return tracer.capture()
}

// CaptureAfterMessage waits for the selected CQQTMsgMgr command rather than a
// connection callback. This is useful for requests such as purchases that can
// reuse an already-open shop session.
func (tracer *ShopDealTracer) CaptureAfterMessage(duration, settle time.Duration) ShopDealTraceCapture {
	if duration <= 0 {
		duration = 2 * time.Minute
	}
	if settle <= 0 {
		settle = 500 * time.Millisecond
	}
	deadline := time.Now().Add(duration)
	var capturedAt time.Time
	for time.Now().Before(deadline) {
		message, _ := readRemote(tracer.process, tracer.messageRequestRecord, 4)
		if len(message) == 4 && binary.LittleEndian.Uint32(message) != 0 {
			if capturedAt.IsZero() {
				capturedAt = time.Now()
			}
			if time.Since(capturedAt) >= settle {
				break
			}
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	return tracer.capture()
}

func (tracer *ShopDealTracer) capture() ShopDealTraceCapture {
	result := ShopDealTraceCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		MessageCommand: tracer.messageCommand,
		ConnectPatch:   fmt.Sprintf("0x%08X", tracer.connectPatch), ResultPatch: fmt.Sprintf("0x%08X", tracer.resultPatch),
		PooledResultPatch: fmt.Sprintf("0x%08X", tracer.pooledResultPatch), DealRequestPatch: fmt.Sprintf("0x%08X", tracer.dealRequestPatch),
		MessageRequestPatch: fmt.Sprintf("0x%08X", tracer.messageRequestPatch),
		OriginalConnect:     fmt.Sprintf("%X", tracer.originalConnect), OriginalResult: fmt.Sprintf("%X", tracer.originalResult),
		OriginalPooledResult: fmt.Sprintf("%X", tracer.originalPooledResult), OriginalDealRequest: fmt.Sprintf("%X", tracer.originalDealRequest),
		OriginalMessageRequest: fmt.Sprintf("%X", tracer.originalMessageRequest),
		Behavior:               "transparent CDealSocketObject connect/connect-result trace; records queue cursors and connection state without changing dispatch",
	}
	connect, _ := readRemote(tracer.process, tracer.connectRecord, shopDealTraceRecordSize)
	callback, _ := readRemote(tracer.process, tracer.resultRecord, shopDealTraceRecordSize)
	pooled, _ := readRemote(tracer.process, tracer.pooledResultRecord, shopDealTraceRecordSize)
	dealRequest, _ := readRemote(tracer.process, tracer.dealRequestRecord, shopDealTraceRecordSize)
	messageRequest, _ := readRemote(tracer.process, tracer.messageRequestRecord, shopMessageTraceRecordSize)
	callbackObject := uint32(0)
	if len(callback) == shopDealTraceRecordSize {
		callbackObject = binary.LittleEndian.Uint32(callback[8:12])
	}
	result.Connect = decodeShopDealTraceEvent(connect)
	result.ConnectResult = decodeShopDealTraceEvent(callback)
	result.PostConnect = snapshotShopDealObject(tracer.process, callbackObject)
	result.PooledConnectResult = decodeShopDealTraceEvent(pooled)
	result.DealRequest = decodeShopDealTraceEvent(dealRequest)
	result.MessageRequest = decodeShopMessageTraceEvent(messageRequest)
	return result
}

// snapshotShopDealObject reads the concrete socket after its asynchronous
// connect-result method has returned. Entry traces necessarily see Connected=0
// and QueueRead=0 because the native callback sets/flushes them later in the
// same method; this post-state distinguishes a missed callback from a failed
// queue drain without changing target behavior.
func snapshotShopDealObject(process syscall.Handle, address uint32) ShopDealTraceEvent {
	if process == 0 || address == 0 {
		return ShopDealTraceEvent{}
	}
	object, ok := readRemote(process, uintptr(address), 0x20B0)
	if !ok || len(object) < 0x20B0 {
		return ShopDealTraceEvent{Object: fmt.Sprintf("0x%08X", address)}
	}
	value := func(offset int) uint32 { return binary.LittleEndian.Uint32(object[offset : offset+4]) }
	return ShopDealTraceEvent{
		Object: fmt.Sprintf("0x%08X", address), QueueWrite: value(0x20A0), QueueRead: value(0x20A4),
		Connected: value(0x20AC), RemoteIPv4: fmt.Sprintf("0x%08X", value(0x08)),
		RemotePort:  uint32(binary.LittleEndian.Uint16(object[0x0C:0x0E])),
		SocketToken: uint32(binary.LittleEndian.Uint16(object[0x0E:0x10])),
		VTable:      fmt.Sprintf("0x%08X", value(0)),
	}
}

func decodeShopDealTraceEvent(record []byte) ShopDealTraceEvent {
	if len(record) != shopDealTraceRecordSize {
		return ShopDealTraceEvent{}
	}
	value := func(offset int) uint32 { return binary.LittleEndian.Uint32(record[offset : offset+4]) }
	return ShopDealTraceEvent{
		Calls: value(0), ThreadID: value(4), Object: fmt.Sprintf("0x%08X", value(8)),
		Argument1: fmt.Sprintf("0x%08X", value(12)), Argument2: fmt.Sprintf("0x%08X", value(16)), Argument3: fmt.Sprintf("0x%08X", value(20)),
		QueueWrite: value(24), QueueRead: value(28), Connected: value(32), RemoteIPv4: fmt.Sprintf("0x%08X", value(36)),
		RemotePort: value(40), SocketToken: value(44),
		VTable: fmt.Sprintf("0x%08X", value(48)),
	}
}

func decodeShopMessageTraceEvent(record []byte) ShopMessageTraceEvent {
	if len(record) != shopMessageTraceRecordSize {
		return ShopMessageTraceEvent{}
	}
	value := func(offset int) uint32 { return binary.LittleEndian.Uint32(record[offset : offset+4]) }
	return ShopMessageTraceEvent{
		Calls: value(0), ThreadID: value(4), Object: fmt.Sprintf("0x%08X", value(8)),
		Request: fmt.Sprintf("0x%08X", value(12)), Length: value(16), Descriptor: fmt.Sprintf("0x%08X", value(20)),
		ServerID: value(24), ConnectionType: value(28), Route: value(32), Schema: value(36), Command: value(40),
		Flags: value(44), ServiceID: value(48), IPv4: fmt.Sprintf("0x%08X", value(52)), PortPair: fmt.Sprintf("0x%08X", value(56)),
	}
}

func (tracer *ShopDealTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	if tracer.connectPatch != 0 && len(tracer.originalConnect) != 0 {
		ensureRemoteBytes(tracer.process, tracer.connectPatch, tracer.originalConnect, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(tracer.process), tracer.connectPatch, uintptr(len(tracer.originalConnect)))
	}
	if tracer.resultPatch != 0 && len(tracer.originalResult) != 0 {
		ensureRemoteBytes(tracer.process, tracer.resultPatch, tracer.originalResult, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(tracer.process), tracer.resultPatch, uintptr(len(tracer.originalResult)))
	}
	if tracer.pooledResultPatch != 0 && len(tracer.originalPooledResult) != 0 {
		ensureRemoteBytes(tracer.process, tracer.pooledResultPatch, tracer.originalPooledResult, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(tracer.process), tracer.pooledResultPatch, uintptr(len(tracer.originalPooledResult)))
	}
	if tracer.dealRequestPatch != 0 && len(tracer.originalDealRequest) != 0 {
		ensureRemoteBytes(tracer.process, tracer.dealRequestPatch, tracer.originalDealRequest, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(tracer.process), tracer.dealRequestPatch, uintptr(len(tracer.originalDealRequest)))
	}
	if tracer.messageRequestPatch != 0 && len(tracer.originalMessageRequest) != 0 {
		ensureRemoteBytes(tracer.process, tracer.messageRequestPatch, tracer.originalMessageRequest, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(tracer.process), tracer.messageRequestPatch, uintptr(len(tracer.originalMessageRequest)))
	}
	if tracer.page != 0 {
		procVirtualFreeEx.Call(uintptr(tracer.process), tracer.page, 0, memRelease)
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}
