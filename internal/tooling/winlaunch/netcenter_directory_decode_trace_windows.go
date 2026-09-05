package winlaunch

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"strings"
	"syscall"
	"time"
)

const (
	netCenterDirectoryDecodePatchRVA  = uintptr(0x4625)
	netCenterDirectoryDecodeResumeRVA = uintptr(0x4664)
	directoryNativeFrameOffset        = uint32(0x308A0)
	directoryNativePrefixLength       = 0x540
	directoryNativeChannelOffset      = 0x1064
	directoryNativeChannelLength      = 0x80
	directoryDecodeRecordHeader       = 20
)

var netCenterDirectoryDecodeSignature = []byte{0x8B, 0x45, 0x14, 0x8D, 0x95}

type NetCenterDirectoryDecodeCapture struct {
	PID             uint32 `json:"pid"`
	ElapsedMS       int64  `json:"elapsed_ms"`
	Calls           uint32 `json:"calls"`
	ThreadID        uint32 `json:"thread_id,omitempty"`
	DecoderReturn   uint32 `json:"decoder_return"`
	FramePointer    string `json:"frame_pointer"`
	NativeObject    string `json:"native_object"`
	ResultID        uint16 `json:"result_id"`
	Version         uint32 `json:"version"`
	Build           uint32 `json:"build"`
	LocationCount   uint8  `json:"location_count"`
	LocationID      uint16 `json:"location_id"`
	LocationName    string `json:"location_name"`
	ServerCount     uint8  `json:"server_count"`
	ServerID        uint32 `json:"server_id"`
	ServerIP        string `json:"server_ip"`
	ServerPort      uint16 `json:"server_port"`
	ServerUDPPort   uint16 `json:"server_udp_port"`
	ChannelCount    uint8  `json:"channel_count"`
	ChannelName     string `json:"channel_name"`
	ChannelID       uint32 `json:"channel_id"`
	SectionCount    uint16 `json:"section_count"`
	SectionName     string `json:"section_name"`
	SectionID       uint16 `json:"section_id"`
	SectionServerID uint32 `json:"section_server_id"`
	MaxPlayers      uint16 `json:"max_players"`
	CurrentPlayers  uint16 `json:"current_players"`
	PatchAddress    string `json:"patch_address"`
	StubAddress     string `json:"stub_address"`
	OriginalBytes   string `json:"original_bytes"`
	Behavior        string `json:"behavior"`
}

type NetCenterDirectoryDecodeTracer struct {
	pid          uint32
	process      syscall.Handle
	started      time.Time
	patchAddress uintptr
	stubAddress  uintptr
	record       uintptr
	original     []byte
}

func InstallNetCenterDirectoryDecodeTracer(pid uint32, timeout time.Duration) (*NetCenterDirectoryDecodeTracer, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &NetCenterDirectoryDecodeTracer{pid: pid, process: process, started: time.Now()}
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
	tracer.patchAddress = moduleBase + netCenterDirectoryDecodePatchRVA
	tracer.original, _ = readRemote(process, tracer.patchAddress, len(netCenterDirectoryDecodeSignature))
	if len(tracer.original) != len(netCenterDirectoryDecodeSignature) ||
		!equalBytes(tracer.original, netCenterDirectoryDecodeSignature) {
		return nil, fmt.Errorf("NetCenter post-decode signature mismatch at 0x%08X: got %X", tracer.patchAddress, tracer.original)
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x2000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xffffffff {
		return nil, fmt.Errorf("VirtualAllocEx NetCenter post-decode trace: 0x%X (%v)", page, allocErr)
	}
	tracer.stubAddress = page
	tracer.record = page + 0x400
	stub := buildNetCenterDirectoryDecodeTraceStub(page, tracer.record, moduleBase+netCenterDirectoryDecodeResumeRVA)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write NetCenter post-decode trace stub: %w", err)
	}
	patch := []byte{0xE9, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(patch[1:], uint32(page-(tracer.patchAddress+5)))
	if !ensureRemoteBytes(process, tracer.patchAddress, patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch NetCenter post-decode branch at 0x%08X", tracer.patchAddress)
	}
	procFlushInstruction.Call(uintptr(process), tracer.patchAddress, uintptr(len(patch)))
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	failed = false
	return tracer, nil
}

func buildNetCenterDirectoryDecodeTraceStub(page, record, resume uintptr) []byte {
	stub := []byte{0x9C, 0x60} // pushfd; pushad
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record))
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+4))
	stub = append(stub, 0x8B, 0x44, 0x24, 0x1C, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+8)) // decoder EAX
	stub = append(stub, 0x8B, 0x44, 0x24, 0x08, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+12)) // saved EBP
	stub = append(stub, 0x2D)
	stub = binary.LittleEndian.AppendUint32(stub, directoryNativeFrameOffset)
	stub = append(stub, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+16))
	stub = append(stub, 0x89, 0xC6, 0xBF)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+directoryDecodeRecordHeader))
	stub = append(stub, 0xB9)
	stub = binary.LittleEndian.AppendUint32(stub, directoryNativePrefixLength)
	stub = append(stub, 0xF3, 0xA4)
	stub = append(stub, 0x8B, 0x44, 0x24, 0x08, 0x2D)
	stub = binary.LittleEndian.AppendUint32(stub, directoryNativeFrameOffset)
	stub = append(stub, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, directoryNativeChannelOffset)
	stub = append(stub, 0x89, 0xC6, 0xBF)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+directoryDecodeRecordHeader+directoryNativePrefixLength))
	stub = append(stub, 0xB9)
	stub = binary.LittleEndian.AppendUint32(stub, directoryNativeChannelLength)
	stub = append(stub, 0xF3, 0xA4, 0x61, 0x9D)
	return appendRelativeJump(stub, page+uintptr(len(stub)), resume)
}

func (tracer *NetCenterDirectoryDecodeTracer) CaptureUntilFirst(duration time.Duration) NetCenterDirectoryDecodeCapture {
	recordSize := directoryDecodeRecordHeader + directoryNativePrefixLength + directoryNativeChannelLength
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		record, ok := readRemote(tracer.process, tracer.record, recordSize)
		if ok && binary.LittleEndian.Uint32(record[:4]) != 0 {
			return tracer.capture(record)
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	record, _ := readRemote(tracer.process, tracer.record, recordSize)
	return tracer.capture(record)
}

func (tracer *NetCenterDirectoryDecodeTracer) capture(record []byte) NetCenterDirectoryDecodeCapture {
	result := NetCenterDirectoryDecodeCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		PatchAddress:  fmt.Sprintf("0x%08X", tracer.patchAddress),
		StubAddress:   fmt.Sprintf("0x%08X", tracer.stubAddress),
		OriginalBytes: fmt.Sprintf("%X", tracer.original),
		Behavior:      "record the decoded schema-0x0816 native object, skip the QQTDir UI callback, and return handled",
	}
	want := directoryDecodeRecordHeader + directoryNativePrefixLength + directoryNativeChannelLength
	if len(record) != want {
		return result
	}
	result.Calls = binary.LittleEndian.Uint32(record[0:4])
	result.ThreadID = binary.LittleEndian.Uint32(record[4:8])
	result.DecoderReturn = binary.LittleEndian.Uint32(record[8:12])
	result.FramePointer = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[12:16]))
	result.NativeObject = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[16:20]))
	decodeDirectoryNativeCapture(&result, record[20:20+directoryNativePrefixLength], record[20+directoryNativePrefixLength:])
	return result
}

func decodeDirectoryNativeCapture(result *NetCenterDirectoryDecodeCapture, prefix, channel []byte) {
	if len(prefix) < directoryNativePrefixLength || len(channel) < directoryNativeChannelLength {
		return
	}
	result.ResultID = binary.LittleEndian.Uint16(prefix[0:2])
	result.Version = binary.LittleEndian.Uint32(prefix[2:6])
	result.Build = binary.LittleEndian.Uint32(prefix[6:10])
	result.LocationCount = prefix[0x0A]
	result.LocationID = binary.LittleEndian.Uint16(prefix[0x0B:0x0D])
	result.LocationName = countedString(prefix[0x0E:0x25], int(prefix[0x0D]))
	result.ServerCount = prefix[0x523]
	result.ServerID = binary.LittleEndian.Uint32(prefix[0x524:0x528])
	ipBytes := [4]byte(prefix[0x528:0x52C])
	result.ServerIP = netip.AddrFrom4(ipBytes).String()
	result.ServerPort = binary.LittleEndian.Uint16(prefix[0x52C:0x52E])
	result.ServerUDPPort = binary.LittleEndian.Uint16(prefix[0x52E:0x530])
	result.ChannelCount = channel[0]
	result.ChannelName = strings.TrimRight(string(channel[1:0x15]), "\x00")
	result.ChannelID = binary.LittleEndian.Uint32(channel[0x15:0x19])
	result.SectionCount = binary.LittleEndian.Uint16(channel[0x19:0x1B])
	section := channel[0x1B:]
	result.SectionName = countedString(section[1:0x18], int(section[0]))
	result.SectionID = binary.LittleEndian.Uint16(section[0x18:0x1A])
	result.SectionServerID = binary.LittleEndian.Uint32(section[0x1A:0x1E])
	result.MaxPlayers = binary.LittleEndian.Uint16(section[0x1E:0x20])
	result.CurrentPlayers = binary.LittleEndian.Uint16(section[0x20:0x22])
}

func countedString(buffer []byte, length int) string {
	if length < 0 {
		return ""
	}
	if length > len(buffer) {
		length = len(buffer)
	}
	return string(buffer[:length])
}

func (tracer *NetCenterDirectoryDecodeTracer) Close() {
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
