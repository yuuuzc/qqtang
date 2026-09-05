package game

// Command is a top-level QQTang game-service transport command. Command IDs
// live in this file so protocol handlers, codecs, and documentation cannot
// silently introduce conflicting numeric values in unrelated source files.
type Command uint16

// CommandFamily identifies the application area that owns a transport
// command. It is intentionally coarser than an individual packet schema: one
// request and its response usually share a command while using different
// schemas.
type CommandFamily string

const (
	CommandFamilySession   CommandFamily = "session"
	CommandFamilyLobby     CommandFamily = "lobby"
	CommandFamilyRoom      CommandFamily = "room"
	CommandFamilyMatch     CommandFamily = "match"
	CommandFamilyInventory CommandFamily = "inventory"
	CommandFamilyShop      CommandFamily = "shop"
	CommandFamilyPet       CommandFamily = "pet"
	CommandFamilyCraft     CommandFamily = "craft"
	CommandFamilyChat      CommandFamily = "chat"
	CommandFamilyKin       CommandFamily = "kin"
	CommandFamilyMarriage  CommandFamily = "marriage"
	CommandFamilyFriend    CommandFamily = "friend"
	CommandFamilyConfig    CommandFamily = "config"
)

// CommandDirection describes which side may originate a command. Many legacy
// request/response pairs use the same numeric command and are therefore
// represented as bidirectional.
type CommandDirection byte

const (
	CommandBidirectional CommandDirection = iota + 1
	CommandClientToServer
	CommandServerToClient
)

// All confirmed top-level transport commands. Packet-local schema IDs and
// payload sizes remain next to their codecs; they are not command IDs.
const (
	LoginCommand                   = 0x0064
	LogoutCommand                  = 0x0065
	RoomListCommand                = 0x0066
	FindFriendCommand              = 0x0067
	EnterRoomCommand               = 0x0068
	LeaveRoomCommand               = 0x0069
	PlayerListCommand              = 0x006A
	CreateRoomCommand              = 0x006B
	ChatAcrossSectionCommand       = 0x006C
	RoomChatCommand                = 0x006D
	SectionChatCommand             = 0x006E
	SectionChatNotifyCommand       = 0x0092
	RoomChatNotifyCommand          = 0x0093
	AcrossSectionChatNotifyCommand = 0x0095
	KickOffPlayerCommand           = 0x006F
	ChangeTeamCommand              = 0x0070
	ChangeRoleCommand              = 0x0071
	ReadyCommand                   = 0x0072
	CancelReadyCommand             = 0x0073
	KickOffRoomNotifyCommand       = 0x0074
	ModifyRoomInfoCommand          = 0x0078
	ModifyRoomInfoNotifyCommand    = 0x0079
	ChangeTeamNotifyCommand        = 0x007A
	ChangeRoleNotifyCommand        = 0x007B
	ReadyStateNotifyCommand        = 0x007C
	UseItemInRoomCommand           = 0x007D
	UseItemInRoomNotifyCommand     = 0x007E
	SetSeatStatusCommand           = 0x007F
	SetSeatStatusNotifyCommand     = 0x0080
	StartGameCommand               = 0x0082
	GameEventRequestCommand        = 0x0083
	GameEventNotifyCommand         = 0x0084
	ItemStatusChangeCommand        = 0x0085
	RequestUDPOKCommand            = 0x0086
	NotifyUDPOKCommand             = 0x0087
	HelloCommand                   = 0x0088
	GameBeginNotifyCommand         = 0x0089
	JoinRoomCommand                = 0x008A
	GetGameMoneyCommand            = 0x008B
	RoomPushCommand                = 0x008C
	EnterRoomNotifyCommand         = 0x008D
	LeaveRoomNotifyCommand         = 0x008E
	ModifyRoomCommand              = 0x0098
	ModifyRoomNotifyCommand        = 0x0099
	PointsRefreshNotifyCommand     = 0x009A
	GameMoneyNotifyCommand         = 0x0094
	TCPTransferRequestCommand      = 0x009C
	TCPTransferNotifyCommand       = 0x009D
	PlayerStatusNotifyCommand      = 0x00A0
	FriendListCommand              = 0x00A2
	AddFriendCommand               = 0x00A3
	RemoveFriendCommand            = 0x00A4
	FriendUpdateNotifyCommand      = 0x00A5
	AnswerFriendCommand            = 0x00A6
	EnterTreasureCommand           = 0x00AB
	GetTreasureItemCommand         = 0x00AC
	LeaveTreasureCommand           = 0x00AD
	TreasureTimeoutCommand         = 0x00AE
	CreateKinCommand               = 0x00B5
	FetchKinBaseCommand            = 0x00B6
	FetchKinMemberListCommand      = 0x00B7
	UpdateKinTitleCommand          = 0x00B8
	UpdateKinFlagCommand           = 0x00B9
	OperateKinCommand              = 0x00BA
	UpdateKinFlagNotifyCommand     = 0x00BB
	ServerOperateKinCommand        = 0x00BC
	ServerOperateKinNotifyCommand  = 0x00BD
	KinChatCommand                 = 0x00BE
	KinChatNotifyCommand           = 0x00BF
	KickKinMemberCommand           = 0x00C0
	ExitKinCommand                 = 0x00C1
	DismissKinCommand              = 0x00C2
	KinEventNotifyCommand          = 0x00C4
	AssignKinAuthorityCommand      = 0x00C5
	SetKinAuthorityCommand         = 0x00C6
	SetKinBadgeCommand             = 0x00C7
	// The v848/5.2 client sends and receives these two operations through
	// 0x0140/0x0141. 0x00C9/0x00CA are retained as legacy request aliases for
	// older captures; they are not the commands emitted by the shipped client.
	LegacySetKinDeclarationCommand    = 0x00C9
	LegacySetKinNotificationCommand   = 0x00CA
	FetchKinTopCommand                = 0x00CB
	SetKinDeclarationCommand          = 0x0140
	SetKinNotificationCommand         = 0x0141
	SetKinDeclarationResponseCommand  = SetKinDeclarationCommand
	SetKinNotificationResponseCommand = SetKinNotificationCommand
	RequestSparkCommand               = 0x0104
	NotifySparkCommand                = 0x0105
	AnswerSparkCommand                = 0x0106
	MarriageInfoCommand               = 0x0107
	ModifyLoveWordCommand             = 0x0108
	DivorceCommand                    = 0x0109
	StartWeddingCommand               = 0x010A
	WeddingConfirmNotifyCommand       = 0x010B
	AnswerWeddingCommand              = 0x010C
	ChangeWeddingModeCommand          = 0x010D
	ChangeWeddingModeNotifyCommand    = 0x010E
	ChangeRoomTypeCommand             = 0x00D0
	ChangeRoomTypeNotifyCommand       = 0x00D1
	PlayerPetsCommand                 = 0x00D4
	HandlePetCommand                  = 0x00D5
	PlayerItemAddNotifyCommand        = 0x00D9
	CombineForgeCommand               = 0x00CF
	ShopBuyCommand                    = 0x0259
	GetConfigFileCommand              = 0x0266
	ShopListCommand                   = 0x035E
	ForgeCommand                      = 0x0353
	BreakEggCommand                   = 0x00FE
	BackgroundListCommand             = 0x00FF
	UseBackgroundCommand              = 0x0100
	UseBackgroundNotifyCommand        = 0x0101

	// Start-game response deliberately aliases the request command. Naming the
	// alias here makes the shared wire ID explicit instead of looking like an
	// accidental collision.
	StartGameResponseCommand = StartGameCommand
	// The client has one bidirectional transport command for breaking eggs.
	// REQUEST_BREAK_EGG (0x0BDB) and RESPONSE_BREAK_EGG (0x0BDC) are payload
	// schema IDs from QQTSection, not top-level transport commands.
	BreakEggRequestCommand  = BreakEggCommand
	BreakEggResponseCommand = BreakEggCommand
	// GameMoneyCommand is the historical name used by the request/response
	// codec. Keep it as an explicit alias; unsolicited money changes use the
	// distinct server command GameMoneyNotifyCommand (0x0094).
	GameMoneyCommand = GetGameMoneyCommand
)

// CommandDefinition is the machine-readable command registry used by the
// dispatcher and consistency tests.
type CommandDefinition struct {
	ID        Command
	Name      string
	Family    CommandFamily
	Direction CommandDirection
}

var commandDefinitions = [...]CommandDefinition{
	{LoginCommand, "login", CommandFamilySession, CommandBidirectional},
	{LogoutCommand, "logout", CommandFamilySession, CommandBidirectional},
	{RoomListCommand, "room_list", CommandFamilyLobby, CommandBidirectional},
	{FindFriendCommand, "find_friend", CommandFamilyLobby, CommandBidirectional},
	{EnterRoomCommand, "enter_room", CommandFamilyRoom, CommandBidirectional},
	{LeaveRoomCommand, "leave_room", CommandFamilyRoom, CommandBidirectional},
	{PlayerListCommand, "player_list", CommandFamilyLobby, CommandBidirectional},
	{CreateRoomCommand, "create_room", CommandFamilyRoom, CommandBidirectional},
	{ChatAcrossSectionCommand, "chat_across_section", CommandFamilyChat, CommandBidirectional},
	{RoomChatCommand, "room_chat", CommandFamilyChat, CommandBidirectional},
	{SectionChatCommand, "section_chat", CommandFamilyChat, CommandBidirectional},
	{SectionChatNotifyCommand, "section_chat_notify", CommandFamilyChat, CommandServerToClient},
	{RoomChatNotifyCommand, "room_chat_notify", CommandFamilyChat, CommandServerToClient},
	{AcrossSectionChatNotifyCommand, "across_section_chat_notify", CommandFamilyChat, CommandServerToClient},
	{KickOffPlayerCommand, "kick_off_player", CommandFamilyRoom, CommandBidirectional},
	{ChangeTeamCommand, "change_team", CommandFamilyRoom, CommandBidirectional},
	{ChangeRoleCommand, "change_role", CommandFamilyRoom, CommandBidirectional},
	{ReadyCommand, "ready", CommandFamilyRoom, CommandBidirectional},
	{CancelReadyCommand, "cancel_ready", CommandFamilyRoom, CommandBidirectional},
	{KickOffRoomNotifyCommand, "kick_off_room_notify", CommandFamilyRoom, CommandServerToClient},
	{ModifyRoomInfoCommand, "modify_room_info", CommandFamilyRoom, CommandBidirectional},
	{ModifyRoomInfoNotifyCommand, "modify_room_info_notify", CommandFamilyRoom, CommandServerToClient},
	{ChangeTeamNotifyCommand, "change_team_notify", CommandFamilyRoom, CommandServerToClient},
	{ChangeRoleNotifyCommand, "change_role_notify", CommandFamilyRoom, CommandServerToClient},
	{ReadyStateNotifyCommand, "ready_state_notify", CommandFamilyRoom, CommandServerToClient},
	{UseItemInRoomCommand, "use_item_in_room", CommandFamilyInventory, CommandBidirectional},
	{UseItemInRoomNotifyCommand, "use_item_in_room_notify", CommandFamilyInventory, CommandServerToClient},
	{SetSeatStatusCommand, "set_seat_status", CommandFamilyRoom, CommandBidirectional},
	{SetSeatStatusNotifyCommand, "set_seat_status_notify", CommandFamilyRoom, CommandServerToClient},
	{StartGameCommand, "start_game", CommandFamilyMatch, CommandBidirectional},
	{GameEventRequestCommand, "game_event", CommandFamilyMatch, CommandBidirectional},
	{GameEventNotifyCommand, "game_event_notify", CommandFamilyMatch, CommandServerToClient},
	{ItemStatusChangeCommand, "item_status_change", CommandFamilyInventory, CommandBidirectional},
	{RequestUDPOKCommand, "request_udp_ok", CommandFamilyMatch, CommandClientToServer},
	{NotifyUDPOKCommand, "notify_udp_ok", CommandFamilyMatch, CommandServerToClient},
	{HelloCommand, "hello", CommandFamilySession, CommandBidirectional},
	{GameBeginNotifyCommand, "game_begin_notify", CommandFamilyMatch, CommandServerToClient},
	{JoinRoomCommand, "join_room", CommandFamilyRoom, CommandBidirectional},
	{GetGameMoneyCommand, "get_game_money", CommandFamilyInventory, CommandBidirectional},
	{RoomPushCommand, "room_push", CommandFamilyLobby, CommandServerToClient},
	{EnterRoomNotifyCommand, "enter_room_notify", CommandFamilyRoom, CommandServerToClient},
	{LeaveRoomNotifyCommand, "leave_room_notify", CommandFamilyRoom, CommandServerToClient},
	{ModifyRoomCommand, "modify_room", CommandFamilyRoom, CommandBidirectional},
	{ModifyRoomNotifyCommand, "modify_room_notify", CommandFamilyRoom, CommandServerToClient},
	{PointsRefreshNotifyCommand, "points_refresh_notify", CommandFamilyInventory, CommandServerToClient},
	{GameMoneyNotifyCommand, "game_money_notify", CommandFamilyInventory, CommandServerToClient},
	{TCPTransferRequestCommand, "tcp_transfer_request", CommandFamilyMatch, CommandClientToServer},
	{TCPTransferNotifyCommand, "tcp_transfer_notify", CommandFamilyMatch, CommandServerToClient},
	{PlayerStatusNotifyCommand, "player_status_notify", CommandFamilyLobby, CommandServerToClient},
	{FriendListCommand, "friend_list", CommandFamilyFriend, CommandBidirectional},
	{AddFriendCommand, "add_friend", CommandFamilyFriend, CommandBidirectional},
	{RemoveFriendCommand, "remove_friend", CommandFamilyFriend, CommandBidirectional},
	{FriendUpdateNotifyCommand, "friend_update_notify", CommandFamilyFriend, CommandServerToClient},
	{AnswerFriendCommand, "answer_friend", CommandFamilyFriend, CommandBidirectional},
	{EnterTreasureCommand, "enter_treasure", CommandFamilyMatch, CommandBidirectional},
	{GetTreasureItemCommand, "get_treasure_item", CommandFamilyMatch, CommandBidirectional},
	{LeaveTreasureCommand, "leave_treasure", CommandFamilyMatch, CommandClientToServer},
	{TreasureTimeoutCommand, "treasure_timeout", CommandFamilyMatch, CommandServerToClient},
	{CreateKinCommand, "create_kin", CommandFamilyKin, CommandBidirectional},
	{FetchKinBaseCommand, "fetch_kin_base", CommandFamilyKin, CommandBidirectional},
	{FetchKinMemberListCommand, "fetch_kin_member_list", CommandFamilyKin, CommandBidirectional},
	{UpdateKinTitleCommand, "update_kin_title", CommandFamilyKin, CommandBidirectional},
	{UpdateKinFlagCommand, "update_kin_flag", CommandFamilyKin, CommandBidirectional},
	{OperateKinCommand, "operate_kin", CommandFamilyKin, CommandBidirectional},
	{UpdateKinFlagNotifyCommand, "update_kin_flag_notify", CommandFamilyKin, CommandServerToClient},
	{ServerOperateKinCommand, "server_operate_kin", CommandFamilyKin, CommandBidirectional},
	{ServerOperateKinNotifyCommand, "server_operate_kin_notify", CommandFamilyKin, CommandServerToClient},
	{KinChatCommand, "kin_chat", CommandFamilyKin, CommandBidirectional},
	{KinChatNotifyCommand, "kin_chat_notify", CommandFamilyKin, CommandServerToClient},
	{KickKinMemberCommand, "kick_kin_member", CommandFamilyKin, CommandBidirectional},
	{ExitKinCommand, "exit_kin", CommandFamilyKin, CommandBidirectional},
	{DismissKinCommand, "dismiss_kin", CommandFamilyKin, CommandBidirectional},
	{KinEventNotifyCommand, "kin_event_notify", CommandFamilyKin, CommandServerToClient},
	{AssignKinAuthorityCommand, "assign_kin_authority", CommandFamilyKin, CommandBidirectional},
	{SetKinAuthorityCommand, "set_kin_authority", CommandFamilyKin, CommandBidirectional},
	{SetKinBadgeCommand, "set_kin_badge", CommandFamilyKin, CommandBidirectional},
	{LegacySetKinDeclarationCommand, "set_kin_declaration_legacy", CommandFamilyKin, CommandBidirectional},
	{LegacySetKinNotificationCommand, "set_kin_notification_legacy", CommandFamilyKin, CommandBidirectional},
	{FetchKinTopCommand, "fetch_kin_top", CommandFamilyKin, CommandBidirectional},
	{SetKinDeclarationCommand, "set_kin_declaration", CommandFamilyKin, CommandBidirectional},
	{SetKinNotificationCommand, "set_kin_notification", CommandFamilyKin, CommandBidirectional},
	{RequestSparkCommand, "request_spark", CommandFamilyMarriage, CommandBidirectional},
	{NotifySparkCommand, "notify_spark", CommandFamilyMarriage, CommandServerToClient},
	{AnswerSparkCommand, "answer_spark", CommandFamilyMarriage, CommandBidirectional},
	{MarriageInfoCommand, "marriage_info", CommandFamilyMarriage, CommandBidirectional},
	{ModifyLoveWordCommand, "modify_love_word", CommandFamilyMarriage, CommandBidirectional},
	{DivorceCommand, "divorce", CommandFamilyMarriage, CommandBidirectional},
	{StartWeddingCommand, "start_wedding", CommandFamilyMarriage, CommandBidirectional},
	{WeddingConfirmNotifyCommand, "wedding_confirm_notify", CommandFamilyMarriage, CommandServerToClient},
	{AnswerWeddingCommand, "answer_wedding", CommandFamilyMarriage, CommandBidirectional},
	{ChangeWeddingModeCommand, "change_wedding_mode", CommandFamilyMarriage, CommandBidirectional},
	{ChangeWeddingModeNotifyCommand, "change_wedding_mode_notify", CommandFamilyMarriage, CommandServerToClient},
	{ChangeRoomTypeCommand, "change_room_type", CommandFamilyRoom, CommandBidirectional},
	{ChangeRoomTypeNotifyCommand, "change_room_type_notify", CommandFamilyRoom, CommandServerToClient},
	{PlayerPetsCommand, "player_pets", CommandFamilyPet, CommandBidirectional},
	{HandlePetCommand, "handle_pet", CommandFamilyPet, CommandBidirectional},
	{PlayerItemAddNotifyCommand, "player_item_add_notify", CommandFamilyInventory, CommandServerToClient},
	{CombineForgeCommand, "combine_forge", CommandFamilyCraft, CommandBidirectional},
	{ShopBuyCommand, "shop_buy", CommandFamilyShop, CommandBidirectional},
	{GetConfigFileCommand, "get_config_file", CommandFamilyConfig, CommandBidirectional},
	{ShopListCommand, "shop_list", CommandFamilyShop, CommandBidirectional},
	{ForgeCommand, "forge", CommandFamilyCraft, CommandBidirectional},
	{BreakEggCommand, "break_egg", CommandFamilyInventory, CommandBidirectional},
	{BackgroundListCommand, "background_list", CommandFamilyRoom, CommandBidirectional},
	{UseBackgroundCommand, "use_background", CommandFamilyRoom, CommandBidirectional},
	{UseBackgroundNotifyCommand, "use_background_notify", CommandFamilyRoom, CommandServerToClient},
}

// CommandDefinitions returns an isolated copy so callers cannot mutate the
// authoritative registry.
func CommandDefinitions() []CommandDefinition {
	result := make([]CommandDefinition, len(commandDefinitions))
	copy(result, commandDefinitions[:])
	return result
}

// LookupCommand resolves a raw transport ID to its registered definition.
func LookupCommand(id uint16) (CommandDefinition, bool) {
	for _, definition := range commandDefinitions {
		if uint16(definition.ID) == id {
			return definition, true
		}
	}
	return CommandDefinition{}, false
}
