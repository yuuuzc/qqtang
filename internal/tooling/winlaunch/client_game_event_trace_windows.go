package winlaunch

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"syscall"
	"time"
)

const (
	clientGameEventCallbackRVA = uintptr(0x1A23BC)
	clientGameOverConsumeRVA   = uintptr(0x1A3B5F)
	clientGameEventRecordSize  = 0xA0
	clientGameEventPreviewSize = 64
)

var (
	clientGameEventCallbackSignature = []byte{0x55, 0x8B, 0xEC, 0xB8, 0x60, 0x23, 0x00, 0x00}
	clientGameOverConsumeSignature   = []byte{0x55, 0x8B, 0xEC, 0x81, 0xEC, 0x68, 0x01, 0x00, 0x00}
)

type ClientGameEventTraceCapture struct {
	PID                  uint32 `json:"pid"`
	ElapsedMS            int64  `json:"elapsed_ms"`
	Callback             string `json:"callback"`
	Consumer             string `json:"consumer"`
	TotalCallbacks       uint32 `json:"total_callbacks"`
	PlayerDeathCallbacks uint32 `json:"player_death_callbacks"`
	GameOverCallbacks    uint32 `json:"game_over_callbacks"`
	ConsumerCalls        uint32 `json:"consumer_calls"`
	LastCallbackThreadID uint32 `json:"last_callback_thread_id,omitempty"`
	LastCallbackThis     string `json:"last_callback_this,omitempty"`
	LastLength           int32  `json:"last_length,omitempty"`
	LastPayload          string `json:"last_payload,omitempty"`
	LastSchema           string `json:"last_schema,omitempty"`
	LastState            uint32 `json:"last_state,omitempty"`
	LastWorld            string `json:"last_world,omitempty"`
	LastWorldCompleted   uint32 `json:"last_world_completed,omitempty"`
	LastWorldTarget      uint32 `json:"last_world_target,omitempty"`
	ConsumerThreadID     uint32 `json:"consumer_thread_id,omitempty"`
	ConsumerThis         string `json:"consumer_this,omitempty"`
	ConsumerWorld        string `json:"consumer_world,omitempty"`
	ConsumerCompleted    uint32 `json:"consumer_world_completed,omitempty"`
	ConsumerTarget       uint32 `json:"consumer_world_target,omitempty"`
	Behavior             string `json:"behavior"`
}

// ClientGameEventTracer records the original Client.exe server-event callback
// and the separate GAME_OVER consumer. It replays each displaced prologue
// byte-for-byte and restores both sites before freeing its private page.
type ClientGameEventTracer struct {
	pid              uint32
	process          syscall.Handle
	started          time.Time
	callback         uintptr
	consumer         uintptr
	callbackOriginal []byte
	consumerOriginal []byte
	page             uintptr
	record           uintptr
}

func InstallClientGameEventTracer(pid uint32, timeout time.Duration) (*ClientGameEventTracer, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &ClientGameEventTracer{pid: pid, process: process, started: time.Now()}
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
	tracer.callback = moduleBase + clientGameEventCallbackRVA
	tracer.consumer = moduleBase + clientGameOverConsumeRVA
	tracer.callbackOriginal, _ = readRemote(process, tracer.callback, len(clientGameEventCallbackSignature))
	if !equalBytes(tracer.callbackOriginal, clientGameEventCallbackSignature) {
		return nil, fmt.Errorf("Client game-event callback signature mismatch at 0x%08X: got %X", tracer.callback, tracer.callbackOriginal)
	}
	tracer.consumerOriginal, _ = readRemote(process, tracer.consumer, len(clientGameOverConsumeSignature))
	if !equalBytes(tracer.consumerOriginal, clientGameOverConsumeSignature) {
		return nil, fmt.Errorf("Client game-over consumer signature mismatch at 0x%08X: got %X", tracer.consumer, tracer.consumerOriginal)
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xffffffff {
		return nil, fmt.Errorf("VirtualAllocEx Client game-event trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page = page
	tracer.record = page + 0x500
	callbackStub := buildClientGameEventCallbackTraceStub(page, tracer.record, tracer.callback+uintptr(len(tracer.callbackOriginal)), tracer.callbackOriginal)
	consumerStubAddress := page + 0x280
	consumerStub := buildClientGameOverConsumeTraceStub(consumerStubAddress, tracer.record, tracer.consumer+uintptr(len(tracer.consumerOriginal)), tracer.consumerOriginal)
	if len(callbackStub) >= 0x280 {
		return nil, fmt.Errorf("Client game-event callback stub is unexpectedly large: %d", len(callbackStub))
	}
	if err := writeRemote(process, page, callbackStub); err != nil {
		return nil, fmt.Errorf("write Client game-event callback stub: %w", err)
	}
	if err := writeRemote(process, consumerStubAddress, consumerStub); err != nil {
		return nil, fmt.Errorf("write Client game-over consumer stub: %w", err)
	}
	if !ensureRemoteBytes(process, tracer.callback, relativeJumpPatch(tracer.callback, page, len(tracer.callbackOriginal)), pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch Client game-event callback at 0x%08X", tracer.callback)
	}
	if !ensureRemoteBytes(process, tracer.consumer, relativeJumpPatch(tracer.consumer, consumerStubAddress, len(tracer.consumerOriginal)), pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch Client game-over consumer at 0x%08X", tracer.consumer)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(callbackStub)))
	procFlushInstruction.Call(uintptr(process), consumerStubAddress, uintptr(len(consumerStub)))
	procFlushInstruction.Call(uintptr(process), tracer.callback, uintptr(len(tracer.callbackOriginal)))
	procFlushInstruction.Call(uintptr(process), tracer.consumer, uintptr(len(tracer.consumerOriginal)))
	failed = false
	return tracer, nil
}

func relativeJumpPatch(source, target uintptr, size int) []byte {
	patch := make([]byte, size)
	patch[0] = 0xE9
	for index := 5; index < len(patch); index++ {
		patch[index] = 0x90
	}
	displacement := int64(target) - int64(source+5)
	binary.LittleEndian.PutUint32(patch[1:5], uint32(int32(displacement)))
	return patch
}

func buildClientGameEventCallbackTraceStub(stubAddress, record, resume uintptr, original []byte) []byte {
	stub := []byte{0x9C, 0x60} // pushfd; pushad
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record)) // total callbacks
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+12)) // thread ID
	stub = append(stub, 0x8B, 0x44, 0x24, 0x18, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+16)) // saved ECX / this
	stub = append(stub, 0x8B, 0x54, 0x24, 0x28, 0x89, 0x15)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+20)) // length
	stub = append(stub, 0x8B, 0x74, 0x24, 0x2C, 0x89, 0x35)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+24)) // payload pointer
	stub = append(stub, 0x85, 0xF6)                                  // test esi,esi
	noPayloadJump := appendShortJump(&stub, 0x74)                    // jz no-payload
	stub = append(stub, 0x8B, 0x06, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+28)) // schema
	stub = append(stub, 0x3D, 0xA7, 0x0F, 0x00, 0x00)                // cmp eax,0xFA7
	noDeathJump := appendShortJump(&stub, 0x75)
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+4))
	patchShortJump(stub, noDeathJump, len(stub))
	stub = append(stub, 0x3D, 0xBB, 0x0F, 0x00, 0x00) // cmp eax,0xFBB
	noGameOverJump := appendShortJump(&stub, 0x75)
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+8))
	patchShortJump(stub, noGameOverJump, len(stub))
	stub = append(stub, 0x8B, 0x4C, 0x24, 0x28, 0x85, 0xC9) // mov ecx,length; test ecx,ecx
	noCopyJump := appendShortJump(&stub, 0x7E)              // jle no-copy
	stub = append(stub, 0x83, 0xF9, clientGameEventPreviewSize)
	copySizeReadyJump := appendShortJump(&stub, 0x76) // jbe copy-size-ready
	stub = append(stub, 0xB9)
	stub = binary.LittleEndian.AppendUint32(stub, clientGameEventPreviewSize)
	patchShortJump(stub, copySizeReadyJump, len(stub))
	stub = append(stub, 0xBF)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+48))
	stub = append(stub, 0xFC, 0xF3, 0xA4) // cld; rep movsb
	patchShortJump(stub, noCopyJump, len(stub))
	patchShortJump(stub, noPayloadJump, len(stub))

	stub = append(stub, 0x8B, 0x44, 0x24, 0x18) // saved this
	stub = append(stub, 0x8B, 0x90, 0xE4, 0x6E, 0x56, 0x00, 0x89, 0x15)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+32)) // state
	stub = append(stub, 0x8B, 0x90, 0xD8, 0x6E, 0x56, 0x00, 0x89, 0x15)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+36)) // world
	stub = append(stub, 0x85, 0xD2)
	noWorldJump := appendShortJump(&stub, 0x74)
	stub = append(stub, 0x8B, 0x8A, 0x90, 0x38, 0x3B, 0x00, 0x89, 0x0D)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+40))
	stub = append(stub, 0x8B, 0x8A, 0x8C, 0xA0, 0x00, 0x00, 0x89, 0x0D)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+44))
	patchShortJump(stub, noWorldJump, len(stub))
	stub = append(stub, 0x61, 0x9D)
	stub = append(stub, original...)
	return appendRelativeJump(stub, stubAddress+uintptr(len(stub)), resume)
}

func buildClientGameOverConsumeTraceStub(stubAddress, record, resume uintptr, original []byte) []byte {
	stub := []byte{0x9C, 0x60}
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+112))
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+116))
	stub = append(stub, 0x8B, 0x44, 0x24, 0x18, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+120))
	stub = append(stub, 0x8B, 0x90, 0xD8, 0x6E, 0x56, 0x00, 0x89, 0x15)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+124))
	stub = append(stub, 0x85, 0xD2)
	noWorldJump := appendShortJump(&stub, 0x74)
	stub = append(stub, 0x8B, 0x8A, 0x90, 0x38, 0x3B, 0x00, 0x89, 0x0D)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+128))
	stub = append(stub, 0x8B, 0x8A, 0x8C, 0xA0, 0x00, 0x00, 0x89, 0x0D)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+132))
	patchShortJump(stub, noWorldJump, len(stub))
	stub = append(stub, 0x61, 0x9D)
	stub = append(stub, original...)
	return appendRelativeJump(stub, stubAddress+uintptr(len(stub)), resume)
}

func appendShortJump(stub *[]byte, opcode byte) int {
	*stub = append(*stub, opcode, 0)
	return len(*stub) - 1
}

func patchShortJump(stub []byte, displacementIndex, target int) {
	displacement := target - (displacementIndex + 1)
	if displacement < -128 || displacement > 127 {
		panic(fmt.Sprintf("short jump displacement %d is outside int8", displacement))
	}
	stub[displacementIndex] = byte(int8(displacement))
}

func (tracer *ClientGameEventTracer) Capture(duration time.Duration) ClientGameEventTraceCapture {
	if duration <= 0 {
		duration = 3 * time.Minute
	}
	deadline := time.Now().Add(duration)
	var gameOverSeen time.Time
	for time.Now().Before(deadline) {
		record, ok := readRemote(tracer.process, tracer.record, clientGameEventRecordSize)
		if ok {
			if binary.LittleEndian.Uint32(record[112:116]) != 0 {
				time.Sleep(100 * time.Millisecond)
				break
			}
			if binary.LittleEndian.Uint32(record[8:12]) != 0 {
				if gameOverSeen.IsZero() {
					gameOverSeen = time.Now()
				} else if time.Since(gameOverSeen) >= 2*time.Second {
					break
				}
			}
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	return tracer.capture()
}

func (tracer *ClientGameEventTracer) capture() ClientGameEventTraceCapture {
	result := ClientGameEventTraceCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		Callback: fmt.Sprintf("0x%08X", tracer.callback), Consumer: fmt.Sprintf("0x%08X", tracer.consumer),
		Behavior: "transparent Client server-event/settlement trace; both exact prologues restored on close",
	}
	record, ok := readRemote(tracer.process, tracer.record, clientGameEventRecordSize)
	if !ok {
		return result
	}
	result.TotalCallbacks = binary.LittleEndian.Uint32(record[0:4])
	result.PlayerDeathCallbacks = binary.LittleEndian.Uint32(record[4:8])
	result.GameOverCallbacks = binary.LittleEndian.Uint32(record[8:12])
	result.ConsumerCalls = binary.LittleEndian.Uint32(record[112:116])
	result.LastCallbackThreadID = binary.LittleEndian.Uint32(record[12:16])
	result.LastCallbackThis = formatTracePointer(record[16:20])
	result.LastLength = int32(binary.LittleEndian.Uint32(record[20:24]))
	result.LastSchema = fmt.Sprintf("0x%04X", binary.LittleEndian.Uint32(record[28:32]))
	result.LastState = binary.LittleEndian.Uint32(record[32:36])
	result.LastWorld = formatTracePointer(record[36:40])
	result.LastWorldCompleted = binary.LittleEndian.Uint32(record[40:44])
	result.LastWorldTarget = binary.LittleEndian.Uint32(record[44:48])
	previewLength := int(result.LastLength)
	if previewLength > clientGameEventPreviewSize {
		previewLength = clientGameEventPreviewSize
	}
	if previewLength > 0 {
		result.LastPayload = hex.EncodeToString(record[48 : 48+previewLength])
	}
	result.ConsumerThreadID = binary.LittleEndian.Uint32(record[116:120])
	result.ConsumerThis = formatTracePointer(record[120:124])
	result.ConsumerWorld = formatTracePointer(record[124:128])
	result.ConsumerCompleted = binary.LittleEndian.Uint32(record[128:132])
	result.ConsumerTarget = binary.LittleEndian.Uint32(record[132:136])
	return result
}

func formatTracePointer(data []byte) string {
	value := binary.LittleEndian.Uint32(data)
	if value == 0 {
		return ""
	}
	return fmt.Sprintf("0x%08X", value)
}

func (tracer *ClientGameEventTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	if tracer.consumer != 0 && len(tracer.consumerOriginal) != 0 {
		ensureRemoteBytes(tracer.process, tracer.consumer, tracer.consumerOriginal, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(tracer.process), tracer.consumer, uintptr(len(tracer.consumerOriginal)))
	}
	if tracer.callback != 0 && len(tracer.callbackOriginal) != 0 {
		ensureRemoteBytes(tracer.process, tracer.callback, tracer.callbackOriginal, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(tracer.process), tracer.callback, uintptr(len(tracer.callbackOriginal)))
	}
	if tracer.page != 0 {
		procVirtualFreeEx.Call(uintptr(tracer.process), tracer.page, 0, memRelease)
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}
