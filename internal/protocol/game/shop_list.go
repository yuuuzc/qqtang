package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	// ShopListDownloadCurrent is the client's native RESPONSE_SHOPLIST value
	// for "the local Commodity.ini is current; no transfer follows".
	ShopListDownloadCurrent uint16 = 3

	ShopListSchemaRequest  uint32 = 0x1799
	ShopListSchemaResponse uint32 = 0x179A
)

// ShopListRequest is the complete 20-byte REQUEST_SHOPLIST payload recovered
// from QQTMsgData. Version fields are preserved separately because they refer
// to different client components.
type ShopListRequest struct {
	UIN             uint32
	Time            uint32
	ClientVersion   uint32
	CSVersion       uint32
	ShopListVersion uint32
}

// DecodeLocalShopListRequest validates and decodes a request received by the
// dedicated Deal service.
func DecodeLocalShopListRequest(requestPacket []byte) (ShopListRequest, error) {
	var request ShopListRequest
	packet, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return request, err
	}
	if packet.Command != ShopListCommand {
		return request, fmt.Errorf("shop-list command 0x%04X, want 0x%04X", packet.Command, ShopListCommand)
	}
	payload := packet.Plaintext[localInnerHeaderSize:]
	if len(payload) != 20 {
		return request, fmt.Errorf("shop-list request payload length %d, want 20", len(payload))
	}
	request.UIN = binary.BigEndian.Uint32(payload[0:4])
	request.Time = binary.BigEndian.Uint32(payload[4:8])
	request.ClientVersion = binary.BigEndian.Uint32(payload[8:12])
	request.CSVersion = binary.BigEndian.Uint32(payload[12:16])
	request.ShopListVersion = binary.BigEndian.Uint32(payload[16:20])
	if request.UIN == 0 {
		return ShopListRequest{}, fmt.Errorf("shop-list request UIN is zero")
	}
	return request, nil
}

// ShopListResponse models the fixed prefix of RESPONSE_SHOPLIST. FileData is
// one transfer fragment and must not exceed the QQTMsgData capacity of 29000.
type ShopListResponse struct {
	ResultID              uint16
	DownloadResultID      uint16
	LatestShopListVersion uint32
	RealFileLength        uint32
	RealZipFileLength     uint32
	BufferSequence        byte
	FileData              []byte
}

func (response ShopListResponse) payload() ([]byte, error) {
	if len(response.FileData) > 29000 {
		return nil, fmt.Errorf("shop-list transfer fragment length %d exceeds 29000", len(response.FileData))
	}
	payload := make([]byte, 19+len(response.FileData))
	binary.BigEndian.PutUint16(payload[0:2], response.ResultID)
	binary.BigEndian.PutUint16(payload[2:4], response.DownloadResultID)
	binary.BigEndian.PutUint32(payload[4:8], response.LatestShopListVersion)
	binary.BigEndian.PutUint32(payload[8:12], response.RealFileLength)
	binary.BigEndian.PutUint32(payload[12:16], response.RealZipFileLength)
	payload[16] = response.BufferSequence
	binary.BigEndian.PutUint16(payload[17:19], uint16(len(response.FileData)))
	copy(payload[19:], response.FileData)
	return payload, nil
}

func BuildLocalShopListCurrentWithReader(requestPacket []byte, version uint32, entropy io.Reader) ([]byte, error) {
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	if request.Command != ShopListCommand {
		return nil, fmt.Errorf("shop-list command 0x%04X, want 0x%04X", request.Command, ShopListCommand)
	}
	payload, err := (ShopListResponse{
		DownloadResultID:      ShopListDownloadCurrent,
		LatestShopListVersion: version,
	}).payload()
	if err != nil {
		return nil, err
	}
	return buildLocalResponse(requestPacket, request, ShopListCommand, payload, entropy)
}
