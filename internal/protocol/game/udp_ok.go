package game

import (
	"encoding/binary"
	"fmt"
)

const (
	RequestTransferUDPOKSchema uint16 = 0x0411
	NotifyTransferUDPOKSchema  uint16 = 0x0413

	transferUDPOKFixedSize            = 16
	TransferUDPOKInfoMaximum          = 32
	TransferUDPOKStartNegotiationMode = 0
	transferUDPOKAddressInfoMinimum   = 7 // mode + opaque 32-bit/16-bit transport address pair
	transferUDPOKRoute                = 4
	transferUDPOKSectionID            = 0xFFFF
)

// TransferUDPOKRequest is REQUEST_TRANSFER_UDP_OK from QQTMsgData.bin. Info
// is a variable-length opaque handshake blob; it is not an array of peers.
type TransferUDPOKRequest struct {
	SourceUIN           uint32
	ClientTime          uint32
	DestinationPlayerID uint16
	DestinationUIN      uint32
	Info                []byte
}

// TransferUDPOKNotification is NOTIFY_UDP_OK from QQTMsgData.bin. UIN is the
// notification recipient, while SourceUIN/SourcePlayerID identify the peer
// whose handshake blob is being delivered.
type TransferUDPOKNotification struct {
	RecipientUIN   uint32
	ClientTime     uint32
	SourcePlayerID uint16
	SourceUIN      uint32
	Info           []byte
}

func DecodeLocalTransferUDPOKRequest(packet []byte) (TransferUDPOKRequest, error) {
	decoded, err := decodeLocalPacket(packet)
	if err != nil {
		return TransferUDPOKRequest{}, err
	}
	if decoded.Command != RequestUDPOKCommand {
		return TransferUDPOKRequest{}, fmt.Errorf("REQUEST_TRANSFER_UDP_OK command 0x%04X, want 0x%04X", decoded.Command, RequestUDPOKCommand)
	}
	payload := decoded.Plaintext[localInnerHeaderSize:]
	if len(payload) < transferUDPOKFixedSize {
		return TransferUDPOKRequest{}, fmt.Errorf("REQUEST_TRANSFER_UDP_OK payload length %d is shorter than %d", len(payload), transferUDPOKFixedSize)
	}
	infoLength := int(binary.BigEndian.Uint16(payload[14:16]))
	if infoLength > TransferUDPOKInfoMaximum || len(payload) != transferUDPOKFixedSize+infoLength {
		return TransferUDPOKRequest{}, fmt.Errorf("REQUEST_TRANSFER_UDP_OK info length %d does not match payload length %d", infoLength, len(payload))
	}
	if err := validateTransferUDPOKInfo(payload[16:]); err != nil {
		return TransferUDPOKRequest{}, fmt.Errorf("REQUEST_TRANSFER_UDP_OK: %w", err)
	}
	request := TransferUDPOKRequest{
		SourceUIN:           binary.BigEndian.Uint32(payload[0:4]),
		ClientTime:          binary.BigEndian.Uint32(payload[4:8]),
		DestinationPlayerID: binary.BigEndian.Uint16(payload[8:10]),
		DestinationUIN:      binary.BigEndian.Uint32(payload[10:14]),
		Info:                append([]byte(nil), payload[16:]...),
	}
	if request.SourceUIN == 0 || request.SourceUIN != decoded.EnvelopeUIN {
		return TransferUDPOKRequest{}, fmt.Errorf("REQUEST_TRANSFER_UDP_OK source UIN %d does not match envelope UIN %d", request.SourceUIN, decoded.EnvelopeUIN)
	}
	if request.DestinationUIN == 0 || request.DestinationPlayerID == 0 {
		return TransferUDPOKRequest{}, fmt.Errorf("REQUEST_TRANSFER_UDP_OK destination UIN and player ID must be non-zero")
	}
	return request, nil
}

func (notification TransferUDPOKNotification) MarshalNetworkBinary() ([]byte, error) {
	if notification.RecipientUIN == 0 || notification.SourceUIN == 0 || notification.SourcePlayerID == 0 {
		return nil, fmt.Errorf("NOTIFY_UDP_OK recipient UIN, source UIN, and source player ID must be non-zero")
	}
	if len(notification.Info) > TransferUDPOKInfoMaximum {
		return nil, fmt.Errorf("NOTIFY_UDP_OK info length %d exceeds %d", len(notification.Info), TransferUDPOKInfoMaximum)
	}
	if err := validateTransferUDPOKInfo(notification.Info); err != nil {
		return nil, fmt.Errorf("NOTIFY_UDP_OK: %w", err)
	}
	payload := make([]byte, transferUDPOKFixedSize+len(notification.Info))
	binary.BigEndian.PutUint32(payload[0:4], notification.RecipientUIN)
	binary.BigEndian.PutUint32(payload[4:8], notification.ClientTime)
	binary.BigEndian.PutUint16(payload[8:10], notification.SourcePlayerID)
	binary.BigEndian.PutUint32(payload[10:14], notification.SourceUIN)
	binary.BigEndian.PutUint16(payload[14:16], uint16(len(notification.Info)))
	copy(payload[16:], notification.Info)
	return payload, nil
}

// validateTransferUDPOKInfo mirrors QQTSection's NOTIFY_UDP_OK handler. The
// zero mode invokes QQTPPP's peer-negotiation transition. A non-zero mode is
// followed by a 32-bit/16-bit transport-address pair which the client reads
// unconditionally. Captures show the pair may be a logical UIN/route value,
// so it must not be constrained to a physical IPv4 endpoint here.
func validateTransferUDPOKInfo(info []byte) error {
	if len(info) == 0 {
		return fmt.Errorf("info must contain a mode byte")
	}
	if info[0] != TransferUDPOKStartNegotiationMode && len(info) < transferUDPOKAddressInfoMinimum {
		return fmt.Errorf("transport-address info length %d is shorter than %d", len(info), transferUDPOKAddressInfoMinimum)
	}
	return nil
}

func BuildLocalTransferUDPOKNotificationForRecipient(recipientPacket []byte, notification TransferUDPOKNotification) ([]byte, error) {
	recipient, err := decodeLocalPacket(recipientPacket)
	if err != nil {
		return nil, err
	}
	if recipient.EnvelopeUIN != notification.RecipientUIN {
		return nil, fmt.Errorf("NOTIFY_UDP_OK recipient UIN %d does not match envelope UIN %d", notification.RecipientUIN, recipient.EnvelopeUIN)
	}
	payload, err := notification.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalNotificationFromRequestWithRoute(
		recipientPacket,
		recipient,
		NotifyUDPOKCommand,
		payload,
		localMessageRoute{Route: transferUDPOKRoute, SectionID: transferUDPOKSectionID},
		nil,
	)
}
