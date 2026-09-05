package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

const (
	processCreateThread      = 0x0002
	qqtEncoderOutputCapacity = 0x40000
	memRelease               = 0x8000
)

var procVirtualFreeEx = kernel32.NewProc("VirtualFreeEx")

type QQTEncoderExportResult struct {
	PID          uint32 `json:"pid"`
	Interface    string `json:"interface"`
	VTable       string `json:"vtable"`
	Method       string `json:"method"`
	Schema       uint32 `json:"schema"`
	SourceSize   int    `json:"source_size"`
	ReturnCode   uint32 `json:"return_code"`
	OutputLength uint32 `json:"output_length"`
	Payload      []byte `json:"-"`
}

// ExportQQTEncoderMessage invokes the already initialized QQTEncoder COM
// interface in one explicitly selected 32-bit client process. The source
// object is zero-filled and the encoded bytes are copied back to this process.
// It neither patches a module nor delivers the result to NetCenter/UI.
func ExportQQTEncoderMessage(pid uint32, interfaceAddress uintptr, schema uint32, sourceSize int, timeout time.Duration) (QQTEncoderExportResult, error) {
	if sourceSize <= 0 || sourceSize > 0x100000 {
		return QQTEncoderExportResult{}, fmt.Errorf("source size 0x%X is outside 1..0x100000", sourceSize)
	}
	return ExportQQTEncoderMessageSource(pid, interfaceAddress, schema, make([]byte, sourceSize), timeout)
}

func ExportQQTEncoderMessageSource(pid uint32, interfaceAddress uintptr, schema uint32, source []byte, timeout time.Duration) (QQTEncoderExportResult, error) {
	sourceSize := len(source)
	result := QQTEncoderExportResult{
		PID:        pid,
		Interface:  fmt.Sprintf("0x%08X", interfaceAddress),
		Schema:     schema,
		SourceSize: sourceSize,
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
	methodBytes, ok := readRemote(process, vtable+0x10, 4)
	if !ok || len(methodBytes) != 4 {
		return result, fmt.Errorf("read QQTEncoder encode method at vtable 0x%08X", vtable)
	}
	method := uintptr(binary.LittleEndian.Uint32(methodBytes))
	result.Method = fmt.Sprintf("0x%08X", method)
	if method == 0 {
		return result, fmt.Errorf("QQTEncoder encode method is NULL")
	}

	alignedSource := uintptr((sourceSize + 0xfff) &^ 0xfff)
	allocationSize := uintptr(0x1000) + alignedSource + qqtEncoderOutputCapacity + 0x1000
	page, _, allocErr := procVirtualAllocEx.Call(
		uintptr(process), 0, allocationSize, memReserve|memCommit, pageExecuteReadWrite,
	)
	if page == 0 || page > 0xffffffff {
		return result, fmt.Errorf("VirtualAllocEx QQTEncoder export: 0x%X (%v)", page, allocErr)
	}
	freeAllocation := true
	defer func() {
		if freeAllocation {
			procVirtualFreeEx.Call(uintptr(process), page, 0, memRelease)
		}
	}()

	sourceAddress := page + 0x1000
	outputAddress := sourceAddress + alignedSource
	stateAddress := outputAddress + qqtEncoderOutputCapacity
	stub := buildQQTEncoderExportStub(
		interfaceAddress, method, schema, sourceAddress, outputAddress,
		stateAddress, stateAddress+4,
	)
	if err := writeRemote(process, page, stub); err != nil {
		return result, fmt.Errorf("write QQTEncoder export stub: %w", err)
	}
	if err := writeRemote(process, sourceAddress, source); err != nil {
		return result, fmt.Errorf("write QQTEncoder source object: %w", err)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))

	// Once the thread starts, keep the allocation on an error/timeout because
	// freeing code that might still be executing would be unsafe.
	freeAllocation = false
	if _, err := remoteCallOne(process, page, 0, timeout); err != nil {
		return result, fmt.Errorf("run QQTEncoder export stub: %w", err)
	}
	freeAllocation = true

	state, ok := readRemote(process, stateAddress, 8)
	if !ok || len(state) != 8 {
		return result, fmt.Errorf("read QQTEncoder export state")
	}
	result.OutputLength = binary.LittleEndian.Uint32(state)
	result.ReturnCode = binary.LittleEndian.Uint32(state[4:])
	if result.OutputLength > qqtEncoderOutputCapacity {
		return result, fmt.Errorf("QQTEncoder output length 0x%X exceeds capacity", result.OutputLength)
	}
	if result.OutputLength == 0 {
		return result, nil
	}
	payload, ok := readRemote(process, outputAddress, int(result.OutputLength))
	if !ok || len(payload) != int(result.OutputLength) {
		return result, fmt.Errorf("read QQTEncoder payload 0x%X bytes", result.OutputLength)
	}
	result.Payload = payload
	return result, nil
}

func buildQQTEncoderExportStub(interfaceAddress, method uintptr, schema uint32, sourceAddress, outputAddress, outputLengthAddress, returnCodeAddress uintptr) []byte {
	stub := []byte{0x6A, 0x01} // protocol version / final argument
	stub = appendPushImmediate(stub, sourceAddress)
	stub = appendPushImmediate(stub, outputLengthAddress)
	stub = appendPushImmediate(stub, outputAddress)
	stub = appendPushImmediate(stub, uintptr(schema))
	stub = appendPushImmediate(stub, interfaceAddress)
	stub = append(stub, 0xB8)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(method))
	stub = append(stub, 0xFF, 0xD0) // call eax; callee clears six arguments
	stub = append(stub, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(returnCodeAddress))
	stub = append(stub, 0x31, 0xC0, 0xC2, 0x04, 0x00) // thread exit 0; ret 4
	return stub
}

func appendPushImmediate(code []byte, value uintptr) []byte {
	code = append(code, 0x68)
	return binary.LittleEndian.AppendUint32(code, uint32(value))
}
