package wininspect

import (
	"fmt"
	"syscall"
	"unsafe"
)

const duplicateSameAccess = 0x00000002

var (
	ntdll             = syscall.NewLazyDLL("ntdll.dll")
	procNtQueryObject = ntdll.NewProc("NtQueryObject")
	procNtQueryMutant = ntdll.NewProc("NtQueryMutant")
	procGetThreadID   = kernel32.NewProc("GetThreadId")
)

type objectUnicodeString struct {
	Length        uint16
	MaximumLength uint16
	Buffer        *uint16
}

type mutantBasicInformation struct {
	CurrentCount  int32
	OwnedByCaller byte
	Abandoned     byte
	_             [2]byte
}

func InspectHandle(pid, handleValue uint32) (HandleInfo, error) {
	processValue, _, openErr := procOpenProcess.Call(processQueryInformation|processDuplicateHandle, 0, uintptr(pid))
	if processValue == 0 {
		return HandleInfo{}, fmt.Errorf("OpenProcess for handle inspection: %w", openErr)
	}
	process := syscall.Handle(processValue)
	defer syscall.CloseHandle(process)

	currentProcess, _, _ := procGetCurrentProcess.Call()
	var duplicate syscall.Handle
	success, _, duplicateErr := procDuplicateHandle.Call(
		uintptr(process), uintptr(handleValue), currentProcess,
		uintptr(unsafe.Pointer(&duplicate)), 0, 0, duplicateSameAccess,
	)
	if success == 0 {
		return HandleInfo{}, fmt.Errorf("DuplicateHandle(0x%X): %w", handleValue, duplicateErr)
	}
	defer syscall.CloseHandle(duplicate)

	typeName, err := queryObjectString(duplicate, 2)
	if err != nil {
		return HandleInfo{}, fmt.Errorf("query handle 0x%X type: %w", handleValue, err)
	}
	objectName, _ := queryObjectString(duplicate, 1)
	result := HandleInfo{Value: handleValue, TypeName: typeName, ObjectName: objectName}
	if typeName == "Thread" {
		threadID, _, _ := procGetThreadID.Call(uintptr(duplicate))
		result.ThreadID = uint32(threadID)
	}
	if typeName == "Mutant" {
		var basic mutantBasicInformation
		status, _, _ := procNtQueryMutant.Call(
			uintptr(duplicate), 0, uintptr(unsafe.Pointer(&basic)), unsafe.Sizeof(basic), 0,
		)
		if status == 0 {
			result.Mutant = &MutantInfo{
				CurrentCount: basic.CurrentCount, OwnedByCaller: basic.OwnedByCaller != 0, Abandoned: basic.Abandoned != 0,
			}
		}
	}
	return result, nil
}

func queryObjectString(handle syscall.Handle, informationClass uintptr) (string, error) {
	buffer := make([]byte, 0x1000)
	for attempt := 0; attempt < 2; attempt++ {
		var required uint32
		status, _, _ := procNtQueryObject.Call(
			uintptr(handle), informationClass, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)),
			uintptr(unsafe.Pointer(&required)),
		)
		if status == 0 {
			value := (*objectUnicodeString)(unsafe.Pointer(&buffer[0]))
			if value.Buffer == nil || value.Length == 0 {
				return "", nil
			}
			return syscall.UTF16ToString(unsafe.Slice(value.Buffer, int(value.Length/2))), nil
		}
		if required <= uint32(len(buffer)) || required > 1024*1024 {
			return "", fmt.Errorf("NtQueryObject class %d status 0x%X", informationClass, status)
		}
		buffer = make([]byte, required+2)
	}
	return "", fmt.Errorf("NtQueryObject class %d did not converge", informationClass)
}
