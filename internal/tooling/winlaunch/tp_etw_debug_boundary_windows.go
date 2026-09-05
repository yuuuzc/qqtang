package winlaunch

import (
	"strings"
)

var (
	procDebugActiveProcess        = kernel32.NewProc("DebugActiveProcess")
	procDebugActiveProcessStop    = kernel32.NewProc("DebugActiveProcessStop")
	procDebugSetProcessKillOnExit = kernel32.NewProc("DebugSetProcessKillOnExit")
	procWaitForDebugEvent         = kernel32.NewProc("WaitForDebugEvent")
	procContinueDebugEvent        = kernel32.NewProc("ContinueDebugEvent")
)

const (
	debugEventException      = uint32(1)
	debugEventExitProcess    = uint32(5)
	debugContinue            = uintptr(0x00010002)
	debugExceptionNotHandled = uintptr(0x80010001)
	statusAccessViolation    = uint32(0xC0000005)
	statusBreakpoint         = uint32(0x80000003)
	statusSingleStep         = uint32(0x80000004)
	wow64SingleStep          = uint32(0x4000001E)
	wow64ContextInteger      = uint32(0x00010002)
	debugEventBufferSize     = 176
	tpETWProviderPageSize    = uintptr(0x1000)
)

const (
	wow64ContextEDI    = 0x9C
	wow64ContextESI    = 0xA0
	wow64ContextEBX    = 0xA4
	wow64ContextEDX    = 0xA8
	wow64ContextECX    = 0xAC
	wow64ContextEAX    = 0xB0
	wow64ContextEIP    = 0xB8
	wow64ContextEFlags = 0xC0
	wow64ContextESP    = 0xC4
)

type tpETWPointerRegister struct {
	name   string
	offset int
}

var tpETWPointerRegisters = []tpETWPointerRegister{
	{name: "EDI", offset: wow64ContextEDI},
	{name: "ESI", offset: wow64ContextESI},
	{name: "EBX", offset: wow64ContextEBX},
	{name: "EDX", offset: wow64ContextEDX},
	{name: "ECX", offset: wow64ContextECX},
	{name: "EAX", offset: wow64ContextEAX},
}

func tpETWFaultModule(eip uint32, modules []remoteModuleRange) (string, uint32, bool) {
	for _, module := range modules {
		if eip < module.base || eip-module.base >= module.size {
			continue
		}
		allowed := strings.EqualFold(module.name, "KERNEL32.dll") || strings.EqualFold(module.name, "KERNELBASE.dll")
		return module.name, eip - module.base, allowed
	}
	return "", 0, false
}
