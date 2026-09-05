package probe

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"qqtang/internal/game/itemuse"
	"qqtang/internal/protocol/game"
	"qqtang/internal/server/persistence"
)

// localPlayerPetsRefresh projects the account's complete pet inventory into
// the legacy client's local cache. It must be delivered before a create/enter
// result: the room creator is constructed from GetCurActivePetInfo immediately
// while handling that result, rather than from an ENTER_ROOM member record.
func (server *Server) localPlayerPetsRefresh(session *connectionSession, template []byte) ([]byte, int, error) {
	if session == nil || session.UIN == 0 {
		return nil, 0, fmt.Errorf("pet refresh requires an authenticated session")
	}
	players, err := server.players()
	if err != nil {
		return nil, 0, err
	}
	pets, err := players.ListPets(context.Background(), session.UIN)
	if err != nil {
		return nil, 0, err
	}
	pets = server.projectPetsForClient(pets)
	refresh, err := game.BuildLocalPlayerPetsRefresh(template, pets)
	if err != nil {
		return nil, 0, err
	}
	return refresh, len(pets), nil
}

func (server *Server) requirePetOperationCatalog() error {
	if server == nil || server.petCardsByItem == nil || server.petSkillBooksByItem == nil || server.petFoodsByItem == nil {
		return fmt.Errorf("pet operation catalog is unavailable")
	}
	return nil
}

func (server *Server) handlePetOperationMessage(session *connectionSession, connectionID string, data []byte) loginServiceResult {
	request, err := game.DecodeLocalHandlePetRequest(data)
	if err != nil {
		return loginServiceResult{keepConnection: true}
	}
	result := loginServiceResult{handled: true, keepConnection: true}
	if session.UIN == 0 || request.UIN != session.UIN {
		err = fmt.Errorf("pet operation UIN %d does not match session UIN %d", request.UIN, session.UIN)
	} else if server.playerStore == nil {
		err = fmt.Errorf("player store is unavailable")
	}
	var responsePet *game.PetInfo
	var inventoryChange *game.ItemInfo
	refreshPetList := false
	var refreshedPets []game.PetInfo
	if err == nil {
		var playersErr error
		players, playersErr := server.players()
		if playersErr != nil {
			err = playersErr
		} else {
			switch request.EventID {
			case game.PetEventFeed:
				var itemID uint16
				itemID, err = request.ParameterItemID()
				if err == nil {
					err = server.requirePetOperationCatalog()
				}
				if err == nil {
					food, found := server.petFoodsByItem[itemID]
					if !found {
						err = fmt.Errorf("item %d is not canonical pet food", itemID)
					} else if !food.Complete {
						err = fmt.Errorf("pet food %d has an incomplete original effect", itemID)
					} else {
						_, err = itemuse.Resolve(itemuse.Intent{
							Path: itemuse.PathPetOperation, Context: sessionInventoryItemUseContext(session),
							ItemID: uint32(itemID), Kind: "pet-food",
						})
						if err == nil {
							fed, feedErr := players.FeedPet(context.Background(), session.UIN, request.PetID, food, server.petExperienceTable)
							err = feedErr
							if err == nil {
								responsePet = &fed.Pet
								item := game.NewPermanentItemInfo(fed.ConsumedItemID, fed.RemainingQuantity)
								inventoryChange = &item
							}
						}
					}
				}
			case game.PetEventActivate:
				var pet game.PetInfo
				pet, err = players.SetPetActive(context.Background(), session.UIN, request.PetID, true)
				if err == nil {
					session.Profile.GameInfo.PetID = pet.PetID
					responsePet = &pet
				}
			case game.PetEventDeactivate:
				var pet game.PetInfo
				pet, err = players.SetPetActive(context.Background(), session.UIN, request.PetID, false)
				if err == nil {
					if session.Profile.GameInfo.PetID == pet.PetID {
						session.Profile.GameInfo.PetID = 0
					}
					responsePet = &pet
				}
			case game.PetEventRelease:
				refreshPetList = true
				var pets []game.PetInfo
				pets, err = players.ListPets(context.Background(), session.UIN)
				if err == nil {
					for index := range pets {
						if pets[index].PetID == request.PetID {
							responsePet = &pets[index]
							break
						}
					}
					if responsePet == nil {
						err = fmt.Errorf("pet %d: %w", request.PetID, persistence.ErrPetNotFound)
					}
				}
				if err == nil {
					var removed bool
					removed, err = players.DeletePet(context.Background(), session.UIN, request.PetID)
					if err == nil && !removed {
						err = fmt.Errorf("pet %d: %w", request.PetID, persistence.ErrPetNotFound)
					}
				}
				if err == nil && session.Profile.GameInfo.PetID == request.PetID {
					session.Profile.GameInfo.PetID = 0
				}
			case game.PetEventRename:
				var name string
				name, err = request.ParameterText()
				if err == nil {
					var pet game.PetInfo
					pet, err = players.RenamePet(context.Background(), session.UIN, request.PetID, name)
					responsePet = &pet
				}
			case game.PetEventLearnSkill:
				var itemID uint16
				itemID, err = request.ParameterItemID()
				if err == nil {
					err = server.requirePetOperationCatalog()
				}
				if err == nil {
					book, found := server.petSkillBooksByItem[itemID]
					if !found {
						err = fmt.Errorf("item %d is not a canonical pet skill book", itemID)
					} else {
						_, err = itemuse.Resolve(itemuse.Intent{
							Path: itemuse.PathPetOperation, Context: sessionInventoryItemUseContext(session),
							ItemID: uint32(itemID), Kind: "pet-skill-book",
						})
						if err == nil {
							learned, learnErr := players.LearnPetSkillFromBook(context.Background(), session.UIN, request.PetID, book)
							err = learnErr
							if err == nil {
								responsePet = &learned.Pet
								item := game.NewPermanentItemInfo(learned.ConsumedItemID, learned.RemainingQuantity)
								inventoryChange = &item
							}
						}
					}
				}
			case game.PetEventAdopt:
				refreshPetList = true
				var itemID uint16
				itemID, err = request.ParameterItemID()
				if err == nil {
					err = server.requirePetOperationCatalog()
				}
				if err == nil {
					link, found := server.petCardsByItem[itemID]
					if !found {
						err = fmt.Errorf("item %d is not a canonical pet card", itemID)
					} else {
						_, err = itemuse.Resolve(itemuse.Intent{
							Path: itemuse.PathPetOperation, Context: sessionInventoryItemUseContext(session),
							ItemID: uint32(itemID), Kind: "pet-card",
						})
						if err == nil {
							adopted, adoptErr := players.AdoptPetFromCard(context.Background(), session.UIN, link)
							err = adoptErr
							if err == nil {
								responsePet = &adopted.Pet
								item := game.NewPermanentItemInfo(adopted.ConsumedItemID, adopted.RemainingQuantity)
								inventoryChange = &item
							}
						}
					}
				}
			}
			if err == nil {
				var refreshed game.PlayerProfile
				refreshed, err = players.Load(context.Background(), session.UIN)
				if err == nil {
					session.replaceProfile(refreshed)
					server.syncPrimarySessionProfile(session, session.UIN, session.Profile)
				}
				if err == nil && refreshPetList {
					refreshedPets, err = players.ListPets(context.Background(), session.UIN)
				}
			}
		}
	}

	resultID := game.HandlePetResultSuccess
	reason := ""
	if err != nil {
		resultID = game.HandlePetResultFailed
		reason = petOperationReason(err)
		server.log(logEvent{
			Level: "warn", Event: "pet_operation_rejected", ConnectionID: connectionID,
			AccountID: fmt.Sprint(request.UIN), MessageID: fmt.Sprintf("0x%04X", game.HandlePetCommand),
			Result: fmt.Sprintf("event_%d", request.EventID), ErrorContext: err.Error(),
		})
	}
	if responsePet != nil {
		projected := server.projectPetForClient(*responsePet)
		responsePet = &projected
	}
	result.response, err = game.BuildLocalHandlePetResponse(data, resultID, reason, responsePet)
	if err != nil {
		server.log(logEvent{
			Level: "error", Event: "pet_operation_response_failed", ConnectionID: connectionID,
			AccountID: fmt.Sprint(request.UIN), MessageID: fmt.Sprintf("0x%04X", game.HandlePetCommand),
			Result: fmt.Sprintf("event_%d", request.EventID), ErrorContext: err.Error(),
		})
		result.response = nil
	}
	if resultID == game.HandlePetResultSuccess && inventoryChange != nil {
		result.followUp, err = game.BuildLocalPlayerItemAddNotification(data, game.PlayerItemAddNotification{
			UIN: session.UIN, Time: uint32(time.Now().Unix()), SourceUIN: session.UIN,
			Items: []game.ItemInfo{*inventoryChange},
		})
		if err != nil {
			result.followUp = nil
			server.log(logEvent{
				Level: "warn", Event: "pet_inventory_refresh_failed", ConnectionID: connectionID,
				AccountID: fmt.Sprint(request.UIN), ErrorContext: err.Error(),
			})
		} else {
			result.followUpResult = fmt.Sprintf("pet_item_%d_quantity_%d", inventoryChange.ItemID, inventoryChange.NumOfItem)
		}
	}
	if resultID == game.HandlePetResultSuccess && refreshPetList {
		var refresh []byte
		refreshedPets = server.projectPetsForClient(refreshedPets)
		refresh, err = game.BuildLocalPlayerPetsRefresh(data, refreshedPets)
		if err != nil {
			server.log(logEvent{
				Level: "warn", Event: "pet_list_refresh_failed", ConnectionID: connectionID,
				AccountID: fmt.Sprint(request.UIN), ErrorContext: err.Error(),
			})
		} else if len(result.followUp) == 0 {
			result.followUp = refresh
			result.followUpResult = fmt.Sprintf("pet_list_refresh_%d", len(refreshedPets))
		} else {
			result.followUps = append(result.followUps, tcpFollowUpPacket{
				data: refresh, result: fmt.Sprintf("pet_list_refresh_%d", len(refreshedPets)),
			})
		}
	}
	result.result = fmt.Sprintf("qqt_handle_pet_event_%d_result_%d", request.EventID, resultID)
	return result
}

func petOperationReason(err error) string {
	switch {
	case errors.Is(err, persistence.ErrInventoryItemMissing):
		return "道具数量不足"
	case errors.Is(err, persistence.ErrPetAlreadyOwned):
		return "已经拥有这种宠物"
	case errors.Is(err, persistence.ErrPetCapacityReached):
		return "宠物栏已满"
	case errors.Is(err, persistence.ErrPetNotFound):
		return "没有找到这只宠物"
	case errors.Is(err, persistence.ErrPetSkillKnown):
		return "宠物已经学会这个技能"
	case errors.Is(err, persistence.ErrPetSkillLevelOccupied):
		return "宠物已经拥有同等级技能"
	case strings.Contains(err.Error(), "incomplete original effect"):
		return "该宠物食品的原版数值尚未恢复"
	default:
		return "宠物操作失败"
	}
}
