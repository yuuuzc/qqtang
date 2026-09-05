package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	ShopBuyRequestSchema  uint32 = 0x040D
	ShopBuyResponseSchema uint32 = 0x07F5

	shopBuyRequestPayloadSize = 26
	shopBuyResultStringSize   = 128
	shopBuyMaximumItems       = 30
)

type ShopBuyRequest struct {
	UIN               uint32
	ClientTime        uint32
	CommodityID       uint32
	DealType          ShopDealType
	PayType           ShopPaymentMethod
	ClientVersion     uint32
	CommodityVersion  uint32
	AgreeMixedPayment uint16
}

func DecodeLocalShopBuyRequest(requestPacket []byte) (ShopBuyRequest, error) {
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return ShopBuyRequest{}, err
	}
	if request.Command != ShopBuyCommand {
		return ShopBuyRequest{}, fmt.Errorf("shop-buy command 0x%04X, want 0x%04X", request.Command, ShopBuyCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) != shopBuyRequestPayloadSize {
		return ShopBuyRequest{}, fmt.Errorf("shop-buy request payload length %d, want %d", len(payload), shopBuyRequestPayloadSize)
	}
	decoded := ShopBuyRequest{
		UIN:               binary.BigEndian.Uint32(payload[0:4]),
		ClientTime:        binary.BigEndian.Uint32(payload[4:8]),
		CommodityID:       binary.BigEndian.Uint32(payload[8:12]),
		DealType:          ShopDealType(binary.BigEndian.Uint16(payload[12:14])),
		PayType:           ShopPaymentMethod(binary.BigEndian.Uint16(payload[14:16])),
		ClientVersion:     binary.BigEndian.Uint32(payload[16:20]),
		CommodityVersion:  binary.BigEndian.Uint32(payload[20:24]),
		AgreeMixedPayment: binary.BigEndian.Uint16(payload[24:26]),
	}
	if decoded.UIN == 0 || decoded.UIN != request.EnvelopeUIN {
		return ShopBuyRequest{}, fmt.Errorf("shop-buy UIN %d does not match envelope UIN %d", decoded.UIN, request.EnvelopeUIN)
	}
	if decoded.CommodityID == 0 {
		return ShopBuyRequest{}, fmt.Errorf("shop-buy commodity ID is zero")
	}
	return decoded, nil
}

type ShopBuyResponse struct {
	ResultID     ShopBuyResult
	ResultString string
	DealType     ShopDealType
	PayType      ShopPaymentMethod
	PayMoney     uint32
	CommodityID  uint32
	ItemIDs      []uint32
	TicketLeft   uint32
}

func (response ShopBuyResponse) payload() ([]byte, error) {
	if len(response.ItemIDs) > shopBuyMaximumItems {
		return nil, fmt.Errorf("shop-buy response item count %d exceeds %d", len(response.ItemIDs), shopBuyMaximumItems)
	}
	resultString, err := encodeLegacyGBKText(response.ResultString, shopBuyResultStringSize-1)
	if err != nil {
		return nil, fmt.Errorf("shop-buy result string: %w", err)
	}
	// QQTMsgData stores ItemID[30] at native offset 144, but it is a dynamic
	// vector controlled by ItemNum. TicketLeft follows the transmitted IDs.
	payload := make([]byte, 148+4*len(response.ItemIDs))
	binary.BigEndian.PutUint16(payload[0:2], uint16(response.ResultID))
	copy(payload[2:2+shopBuyResultStringSize], resultString)
	binary.BigEndian.PutUint16(payload[130:132], uint16(response.DealType))
	binary.BigEndian.PutUint16(payload[132:134], uint16(response.PayType))
	binary.BigEndian.PutUint32(payload[134:138], response.PayMoney)
	binary.BigEndian.PutUint32(payload[138:142], response.CommodityID)
	binary.BigEndian.PutUint16(payload[142:144], uint16(len(response.ItemIDs)))
	offset := 144
	for _, itemID := range response.ItemIDs {
		binary.BigEndian.PutUint32(payload[offset:offset+4], itemID)
		offset += 4
	}
	binary.BigEndian.PutUint32(payload[offset:offset+4], response.TicketLeft)
	return payload, nil
}

func BuildLocalShopBuyResponse(requestPacket []byte, response ShopBuyResponse) ([]byte, error) {
	return buildLocalShopBuyResponse(requestPacket, response, nil)
}

func BuildLocalShopBuyResponseWithReader(requestPacket []byte, response ShopBuyResponse, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalShopBuyResponse(requestPacket, response, entropy)
}

func buildLocalShopBuyResponse(requestPacket []byte, response ShopBuyResponse, entropy io.Reader) ([]byte, error) {
	if _, err := DecodeLocalShopBuyRequest(requestPacket); err != nil {
		return nil, err
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	payload, err := response.payload()
	if err != nil {
		return nil, err
	}
	return buildLocalResponse(requestPacket, request, ShopBuyCommand, payload, entropy)
}
