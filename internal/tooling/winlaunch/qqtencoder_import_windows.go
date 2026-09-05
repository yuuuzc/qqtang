package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

type QQTEncoderImportResult struct {
	PID         uint32 `json:"pid"`
	Interface   string `json:"interface"`
	VTable      string `json:"vtable"`
	Method      string `json:"method"`
	Schema      uint32 `json:"schema"`
	PayloadSize int    `json:"payload_size"`
	SourceSize  int    `json:"source_size"`
	ReturnCode  uint32 `json:"return_code"`
	Source      []byte `json:"-"`
}

// ImportQQTEncoderMessage invokes the already initialized QQTEncoder decode
// method in one explicitly selected 32-bit client process. The payload is
// decoded into a zero-filled native source object and copied back. It does not
// dispatch a message to NetCenter, QQTSection, or the UI.
func ImportQQTEncoderMessage(pid uint32, interfaceAddress uintptr, schema uint32, payload []byte, sourceSize int, timeout time.Duration) (QQTEncoderImportResult, error) {
	result := QQTEncoderImportResult{
		PID:         pid,
		Interface:   fmt.Sprintf("0x%08X", interfaceAddress),
		Schema:      schema,
		PayloadSize: len(payload),
		SourceSize:  sourceSize,
	}
	if pid == 0 {
		return result, fmt.Errorf("pid must be non-zero")
	}
	if interfaceAddress == 0 || interfaceAddress > 0xffffffff {
		return result, fmt.Errorf("interface address 0x%X is not a 32-bit address", interfaceAddress)
	}
	if schema == 0 {
		return result, fmt.Errorf("schema must be non-zero")
	}
	if len(payload) == 0 || len(payload) > qqtEncoderOutputCapacity {
		return result, fmt.Errorf("payload size 0x%X is outside 1..0x%X", len(payload), qqtEncoderOutputCapacity)
	}
	if sourceSize <= 0 || sourceSize > 0x100000 {
		return result, fmt.Errorf("source size 0x%X is outside 1..0x100000", sourceSize)
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	process, err := syscall.OpenProcess(attachedProcessAccess|processCreateThread, false, pid)
	if err != nil {
		return result, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)

	interfaceBytes, ok := readRemote(process, interfaceAddress, 4)
	if !ok || len(interfaceBytes) != 4 {
		return result, fmt.Errorf("read QQTEncoder interface 0x%08X", interfaceAddress)
	}
	vtable := uintptr(binary.LittleEndian.Uint32(interfaceBytes))
	result.VTable = fmt.Sprintf("0x%08X", vtable)
	methodBytes, ok := readRemote(process, vtable+0x14, 4)
	if !ok || len(methodBytes) != 4 {
		return result, fmt.Errorf("read QQTEncoder decode method at vtable 0x%08X", vtable)
	}
	method := uintptr(binary.LittleEndian.Uint32(methodBytes))
	result.Method = fmt.Sprintf("0x%08X", method)
	if method == 0 {
		return result, fmt.Errorf("QQTEncoder decode method is NULL")
	}

	alignedPayload := uintptr((len(payload) + 0xfff) &^ 0xfff)
	alignedSource := uintptr((sourceSize + 0xfff) &^ 0xfff)
	allocationSize := uintptr(0x1000) + alignedPayload + alignedSource + 0x1000
	page, _, allocErr := procVirtualAllocEx.Call(
		uintptr(process), 0, allocationSize, memReserve|memCommit, pageExecuteReadWrite,
	)
	if page == 0 || page > 0xffffffff {
		return result, fmt.Errorf("VirtualAllocEx QQTEncoder import: 0x%X (%v)", page, allocErr)
	}
	freeAllocation := true
	defer func() {
		if freeAllocation {
			procVirtualFreeEx.Call(uintptr(process), page, 0, memRelease)
		}
	}()

	payloadAddress := page + 0x1000
	sourceAddress := payloadAddress + alignedPayload
	stateAddress := sourceAddress + alignedSource
	stub := buildQQTEncoderImportStub(
		interfaceAddress, method, schema, sourceAddress, payloadAddress,
		uintptr(len(payload)), stateAddress,
	)
	if err := writeRemote(process, page, stub); err != nil {
		return result, fmt.Errorf("write QQTEncoder import stub: %w", err)
	}
	if err := writeRemote(process, payloadAddress, payload); err != nil {
		return result, fmt.Errorf("write QQTEncoder payload: %w", err)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))

	freeAllocation = false
	if _, err := remoteCallOne(process, page, 0, timeout); err != nil {
		return result, fmt.Errorf("run QQTEncoder import stub: %w", err)
	}
	freeAllocation = true

	state, ok := readRemote(process, stateAddress, 4)
	if !ok || len(state) != 4 {
		return result, fmt.Errorf("read QQTEncoder import state")
	}
	result.ReturnCode = binary.LittleEndian.Uint32(state)
	source, ok := readRemote(process, sourceAddress, sourceSize)
	if !ok || len(source) != sourceSize {
		return result, fmt.Errorf("read QQTEncoder source object 0x%X bytes", sourceSize)
	}
	result.Source = source
	return result, nil
}

func buildQQTEncoderImportStub(interfaceAddress, method uintptr, schema uint32, destinationAddress, payloadAddress, payloadSize, returnCodeAddress uintptr) []byte {
	stub := []byte{0x6A, 0x01} // protocol version / final argument
	stub = appendPushImmediate(stub, payloadSize)
	stub = appendPushImmediate(stub, payloadAddress)
	stub = appendPushImmediate(stub, destinationAddress)
	stub = appendPushImmediate(stub, uintptr(schema))
	stub = appendPushImmediate(stub, interfaceAddress)
	stub = append(stub, 0xB8)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(method))
	stub = append(stub, 0xFF, 0xD0)
	stub = append(stub, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(returnCodeAddress))
	stub = append(stub, 0x31, 0xC0, 0xC2, 0x04, 0x00)
	return stub
}
