package game

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestLocalShopBuyRequestAndDynamicResponse(t *testing.T) {
	payload := make([]byte, shopBuyRequestPayloadSize)
	binary.BigEndian.PutUint32(payload[0:4], 1_000_001)
	binary.BigEndian.PutUint32(payload[4:8], 1234)
	binary.BigEndian.PutUint32(payload[8:12], 912067)
	binary.BigEndian.PutUint16(payload[12:14], uint16(ShopDealTypePurchase))
	binary.BigEndian.PutUint16(payload[14:16], uint16(ShopPayTypeGameMoney))
	binary.BigEndian.PutUint32(payload[16:20], 0x00520001)
	binary.BigEndian.PutUint32(payload[20:24], 848)
	binary.BigEndian.PutUint16(payload[24:26], 1)
	packet := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(ShopBuyCommand), payload...))

	request, err := DecodeLocalShopBuyRequest(packet)
	if err != nil {
		t.Fatal(err)
	}
	if request.UIN != 1_000_001 || request.CommodityID != 912067 || request.PayType != ShopPayTypeGameMoney || request.CommodityVersion != 848 {
		t.Fatalf("request = %+v", request)
	}

	encoded, err := BuildLocalShopBuyResponseWithReader(packet, ShopBuyResponse{
		ResultID: ShopBuyResultSuccess, ResultString: "购买成功", DealType: request.DealType,
		PayType: request.PayType, PayMoney: 1000, CommodityID: request.CommodityID,
		ItemIDs: []uint32{2067, 244}, TicketLeft: 9999000,
	}, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != ShopBuyCommand || len(inspection.Payload) != 156 {
		t.Fatalf("response command/size = 0x%04X/%d", inspection.Command, len(inspection.Payload))
	}
	if got := binary.BigEndian.Uint16(inspection.Payload[142:144]); got != 2 {
		t.Fatalf("item count = %d", got)
	}
	if got := binary.BigEndian.Uint32(inspection.Payload[144:148]); got != 2067 {
		t.Fatalf("first item = %d", got)
	}
	if got := binary.BigEndian.Uint32(inspection.Payload[152:156]); got != 9999000 {
		t.Fatalf("ticket left = %d", got)
	}
}
