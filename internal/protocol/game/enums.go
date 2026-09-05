package game

// RoomFastEventID identifies a compact real-time event transported by the
// waiting-room fast path. These are native object-message IDs, not top-level
// game-service commands.
type RoomFastEventID uint16

const (
	RoomPlayerMoveInfoEvent RoomFastEventID = 0x0BB8
	RoomPlayerPutBombEvent  RoomFastEventID = 0x0BB9
)

// LegacyUDPMessageType is the QQTPPP datagram type stored in the common
// 18-byte header. It is independent from the TCP game-service command space.
type LegacyUDPMessageType uint16

const (
	LegacyUDPPresenceType  LegacyUDPMessageType = 1
	LegacyUDPMulticastType LegacyUDPMessageType = 2
	LegacyUDPRoomPeerType  LegacyUDPMessageType = 3
)

// This file is the authoritative home for protocol enum families and bit
// sets. Numeric payload sizes and message-local schema IDs stay beside their
// codecs; values shared by handlers and application services live here.

type PlayerIdentity uint32

const (
	IdentityOrdinary      PlayerIdentity = 0
	IdentityQQMember      PlayerIdentity = 1 << 5
	IdentityPurpleDiamond PlayerIdentity = 1 << 13
)

type FindFriendStatus byte

const (
	FindFriendStatusOffline FindFriendStatus = 0
	FindFriendStatusOnline  FindFriendStatus = 1
)

type RoomListOperation byte

const (
	// RoomListOperationRefresh is emitted by the original client every five
	// seconds after a login response advertises a non-zero room pool.
	RoomListOperationRefresh RoomListOperation = 2
)

type RoomListGameModeFilter byte

const (
	RoomListFilterAllRooms      RoomListGameModeFilter = 0
	RoomListFilterLegacyAllMaps RoomListGameModeFilter = 1
	RoomListFilterAllMaps       RoomListGameModeFilter = 2
	RoomListFilterNormal        RoomListGameModeFilter = 3
)

func (filter RoomListGameModeFilter) CompetitiveMode() (byte, bool) {
	if filter < RoomListFilterNormal {
		return 0, false
	}
	return byte(filter - 2), true
}

type RoomListFlag byte

const (
	// QQTSection consumes four independent ROOM_INFO bits. Bit 0 says that
	// the room is preparing; the client derives joinable/full from the packed
	// current/capacity nibbles and renders a cleared bit as an in-match card.
	// Bits 1..3 are password, free rule and VIP.
	RoomListFlagInMatch   RoomListFlag = 0
	RoomListFlagPreparing RoomListFlag = 1 << 0
	RoomListFlagPassword  RoomListFlag = 1 << 1
	RoomListFlagFreeRule  RoomListFlag = 1 << 2
	RoomListFlagVIP       RoomListFlag = 1 << 3
	roomListKnownFlags                 = RoomListFlagPreparing | RoomListFlagPassword | RoomListFlagFreeRule | RoomListFlagVIP
)

func (flags RoomListFlag) Valid() bool { return flags&^roomListKnownFlags == 0 }

// RoomPropertyFlag is the two-bit rule value passed by CreateRoom and the
// room-property UI. It is deliberately distinct from RoomListFlag: lobby
// ROOM_INFO places the password/free properties at bits 1 and 2 after its
// preparing marker.
type RoomPropertyFlag byte

const (
	RoomPropertyStandard   RoomPropertyFlag = 0
	RoomPropertyPassword   RoomPropertyFlag = 1 << 0
	RoomPropertyFreeRule   RoomPropertyFlag = 1 << 1
	roomPropertyKnownFlags                  = RoomPropertyPassword | RoomPropertyFreeRule
)

func (flags RoomPropertyFlag) Valid() bool {
	return flags&^roomPropertyKnownFlags == 0
}

func (flags RoomPropertyFlag) HasPassword() bool {
	return flags&RoomPropertyPassword != 0
}

func (flags RoomPropertyFlag) UsesFreeRule() bool {
	return flags&RoomPropertyFreeRule != 0
}

// EnterRoomResultID is the native RESPONSE_ENTER_ROOM_OLD result family.
// Values below are backed by QQTSection's result switch and its GBK messages.
type EnterRoomResultID uint16

const (
	EnterRoomResultSuccess           EnterRoomResultID = 0
	EnterRoomResultRoomUnavailable   EnterRoomResultID = 9
	EnterRoomResultRoomFull          EnterRoomResultID = 13
	EnterRoomResultPasswordIncorrect EnterRoomResultID = 14
	EnterRoomResultGameInProgress    EnterRoomResultID = 19
)

// ModifyRoomFlags is QQTSection's native change mask. It describes which
// name/password inputs changed; it does not carry the standard/free rule.
// That explicit two-bit value belongs to REQUEST_MODIFY_ROOM.RoomFlag.
type ModifyRoomFlags uint32

const (
	ModifyRoomNameChanged     ModifyRoomFlags = 1 << 0
	ModifyRoomPasswordPresent ModifyRoomFlags = 1 << 1
	ModifyRoomPasswordChanged ModifyRoomFlags = 1 << 2
	ModifyRoomPasswordEnabled ModifyRoomFlags = 1 << 3
	modifyRoomKnownFlags                      = ModifyRoomNameChanged | ModifyRoomPasswordPresent | ModifyRoomPasswordChanged | ModifyRoomPasswordEnabled
)

type RoomSeatStatus byte

const (
	RoomSeatStatusOpen   RoomSeatStatus = 1
	RoomSeatStatusLocked RoomSeatStatus = 2
)

// ClientRuleMode is the internal mode stored on the client's active gameplay
// rule object. It is not a wire-level room or settlement enum.
type ClientRuleMode byte

const (
	ClientRuleModeStandard  ClientRuleMode = 0x00
	ClientRuleModeAdventure ClientRuleMode = 0x0C
)

// SettlementGameMode is the final byte of NOTIFY_GAME_OVER. It deliberately
// does not share ClientRuleMode values. A controlled legacy-client probe proves
// that value 2 is accepted for an adventure result, while the active client
// rule object uses 12. It does not prove that this byte suppresses the client's
// own competitive-record projection; the 5.2 client increments that transient
// UI copy for both tested values 2 and 12.
type SettlementGameMode byte

const (
	SettlementGameModeCompetitive SettlementGameMode = 0
	SettlementGameModeAdventure   SettlementGameMode = 2
)

// PatternGameMode is the independent discriminator nested in PATTERN_POINT.
// Its concrete values remain unnamed until client consumers are proven.
type PatternGameMode byte

type GameResultCode byte

const (
	GameResultLoss GameResultCode = 0
	GameResultDraw GameResultCode = 1
	GameResultWin  GameResultCode = 2
)

type ShopBuyResult uint16

const (
	ShopBuyResultSuccess ShopBuyResult = 0
	ShopBuyResultFailed  ShopBuyResult = 1
)

type ShopDealType uint16

const (
	// ShopDealTypePurchase is the normal self-purchase path used by uiShop.py.
	ShopDealTypePurchase ShopDealType = 2
)

type ShopPaymentMethod uint16

const (
	ShopPayTypeQCoin     ShopPaymentMethod = 1
	ShopPayTypeGameMoney ShopPaymentMethod = 3
	ShopPayTypeVNet      ShopPaymentMethod = 4
	ShopPayTypeKubiGem   ShopPaymentMethod = 6
	ShopPayTypeQPoint    ShopPaymentMethod = 7
)

// PetEventID is REQUEST_HANDLE_PET.EventID. These values are shared by the
// native QQTSection send wrappers and the response dispatcher, so they belong
// in the central protocol enum registry rather than in a packet handler.
type PetEventID uint32

const (
	PetEventFeed       PetEventID = 1
	PetEventActivate   PetEventID = 2
	PetEventDeactivate PetEventID = 3
	PetEventRelease    PetEventID = 4
	PetEventRename     PetEventID = 5
	PetEventLearnSkill PetEventID = 6
	PetEventAdopt      PetEventID = 7
)

func (event PetEventID) Valid() bool {
	return event >= PetEventFeed && event <= PetEventAdopt
}

type HandlePetResult uint16

const (
	HandlePetResultSuccess HandlePetResult = 0
	HandlePetResultFailed  HandlePetResult = 1
)
