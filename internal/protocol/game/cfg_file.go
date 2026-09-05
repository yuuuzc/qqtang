package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	MaxConfigFileReports = 3
	ConfigFileHashSize   = 16

	configFileRequestHeaderSize = 12
	configFileReportSize        = 24
)

// ConfigFileReport mirrors REPORTFILEINFO in QQTMsgData.bin. The client sends
// one row for every local configuration file whose freshness it wants the
// server to evaluate.
type ConfigFileReport struct {
	FileID      uint32
	FileVersion uint32
	FileHash    [ConfigFileHashSize]byte
}

// ConfigFileRequest mirrors REQUEST_GETCFGFILE (schema 0x081E).
type ConfigFileRequest struct {
	UIN        uint32
	ClientTime uint32
	Files      []ConfigFileReport
}

func DecodeLocalConfigFileRequest(packet []byte) (ConfigFileRequest, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return ConfigFileRequest{}, err
	}
	if request.Command != GetConfigFileCommand {
		return ConfigFileRequest{}, fmt.Errorf("config-file command 0x%04X, want 0x%04X", request.Command, GetConfigFileCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) < configFileRequestHeaderSize {
		return ConfigFileRequest{}, fmt.Errorf("config-file payload length %d is shorter than %d", len(payload), configFileRequestHeaderSize)
	}
	decoded := ConfigFileRequest{
		UIN:        binary.BigEndian.Uint32(payload[0:4]),
		ClientTime: binary.BigEndian.Uint32(payload[4:8]),
	}
	if decoded.UIN != request.EnvelopeUIN {
		return ConfigFileRequest{}, fmt.Errorf("config-file UIN %d does not match envelope UIN %d", decoded.UIN, request.EnvelopeUIN)
	}
	count := int(binary.BigEndian.Uint32(payload[8:12]))
	if count > MaxConfigFileReports {
		return ConfigFileRequest{}, fmt.Errorf("config-file report count %d exceeds %d", count, MaxConfigFileReports)
	}
	wantLength := configFileRequestHeaderSize + count*configFileReportSize
	if len(payload) != wantLength {
		return ConfigFileRequest{}, fmt.Errorf("config-file payload length %d, want %d for %d reports", len(payload), wantLength, count)
	}
	decoded.Files = make([]ConfigFileReport, 0, count)
	for index := 0; index < count; index++ {
		offset := configFileRequestHeaderSize + index*configFileReportSize
		var report ConfigFileReport
		report.FileID = binary.BigEndian.Uint32(payload[offset : offset+4])
		report.FileVersion = binary.BigEndian.Uint32(payload[offset+4 : offset+8])
		copy(report.FileHash[:], payload[offset+8:offset+configFileReportSize])
		decoded.Files = append(decoded.Files, report)
	}
	return decoded, nil
}

// BuildLocalConfigFilesCurrent tells the client that none of the reported
// local files needs replacement. RESPONSE_GETCFGFILE (schema 0x081F) begins
// with a DWORD count; a zero count is the canonical empty response and avoids
// fabricating remote file contents already present in the restored client.
func BuildLocalConfigFilesCurrent(requestPacket []byte) ([]byte, error) {
	return buildLocalConfigFilesCurrent(requestPacket, nil)
}

func BuildLocalConfigFilesCurrentWithReader(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalConfigFilesCurrent(requestPacket, entropy)
}

func buildLocalConfigFilesCurrent(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	if _, err := DecodeLocalConfigFileRequest(requestPacket); err != nil {
		return nil, err
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	return buildLocalResponse(requestPacket, request, GetConfigFileCommand, make([]byte, 4), entropy)
}
