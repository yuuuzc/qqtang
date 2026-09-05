package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

// ShopMessageCapture is the minimal, connection-neutral trace used when only
// the CQQTMsgMgr request boundary matters.
type ShopMessageCapture struct {
	PID             uint32                `json:"pid"`
	ElapsedMS       int64                 `json:"elapsed_ms"`
	MessageCommand  uint32                `json:"message_command"`
	MessageRequest  ShopMessageTraceEvent `json:"message_request"`
	MessagePatch    string                `json:"message_request_patch"`
	OriginalMessage string                `json:"original_message_request"`
	Behavior        string                `json:"behavior"`
}

type ShopMessageTracer struct {
	pid      uint32
	command  uint32
	process  syscall.Handle
	started  time.Time
	page     uintptr
	patch    uintptr
	record   uintptr
	original []byte
}

// InstallShopMessageTracer records one selected CQQTMsgMgr command. Unlike the
// broader CDeal diagnostic it does not patch connect, callback, queue, or
// socket functions, so it cannot perturb shop connection lifetime.
func InstallShopMessageTracer(pid, command uint32, timeout time.Duration) (*ShopMessageTracer, error) {
	if pid == 0 || command == 0 {
		return nil, fmt.Errorf("pid and message command must be non-zero")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &ShopMessageTracer{pid: pid, command: command, process: process, started: time.Now()}
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
	tracer.patch = moduleBase + netCenterMessageRequestRVA
	tracer.original, _ = readRemote(process, tracer.patch, len(netCenterMessageRequestSignature))
	signatureOK := len(tracer.original) == len(netCenterMessageRequestSignature) &&
		tracer.original[0] == 0xB8 &&
		binary.LittleEndian.Uint32(tracer.original[1:]) == uint32(moduleBase+0x2C3EC)
	if !signatureOK {
		return nil, fmt.Errorf("CQQTMsgMgr request signature mismatch at 0x%08X: got %X", tracer.patch, tracer.original)
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx shop message trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page = page
	tracer.record = page + 0x400
	stub := buildShopMessageTraceStub(page, tracer.record, tracer.patch, tracer.original, command)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write shop message trace: %w", err)
	}
	if err := patchShopDealTraceEntry(process, tracer.patch, page, len(tracer.original)); err != nil {
		return nil, err
	}
	procFlushInstruction.Call(uintptr(process), page, 0x1000)
	failed = false
	return tracer, nil
}

func (tracer *ShopMessageTracer) Capture(duration, settle time.Duration) ShopMessageCapture {
	if duration <= 0 {
		duration = 2 * time.Minute
	}
	if settle <= 0 {
		settle = 500 * time.Millisecond
	}
	deadline := time.Now().Add(duration)
	var capturedAt time.Time
	for time.Now().Before(deadline) {
		message, _ := readRemote(tracer.process, tracer.record, 4)
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
	record, _ := readRemote(tracer.process, tracer.record, shopMessageTraceRecordSize)
	return ShopMessageCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(), MessageCommand: tracer.command,
		MessageRequest: decodeShopMessageTraceEvent(record), MessagePatch: fmt.Sprintf("0x%08X", tracer.patch),
		OriginalMessage: fmt.Sprintf("%X", tracer.original), Behavior: "records one CQQTMsgMgr request command without patching any connection or socket function",
	}
}

func (tracer *ShopMessageTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	if tracer.patch != 0 && len(tracer.original) != 0 {
		ensureRemoteBytes(tracer.process, tracer.patch, tracer.original, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(tracer.process), tracer.patch, uintptr(len(tracer.original)))
	}
	if tracer.page != 0 {
		procVirtualFreeEx.Call(uintptr(tracer.process), tracer.page, 0, memRelease)
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}
