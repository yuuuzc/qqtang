package game

import "testing"

func TestCommandRegistryHasNoConflictingIDsOrNames(t *testing.T) {
	definitions := CommandDefinitions()
	if len(definitions) == 0 {
		t.Fatal("command registry is empty")
	}
	byID := make(map[Command]CommandDefinition, len(definitions))
	byName := make(map[string]CommandDefinition, len(definitions))
	for _, definition := range definitions {
		if definition.ID == 0 || definition.Name == "" || definition.Family == "" || definition.Direction == 0 {
			t.Fatalf("incomplete command definition: %+v", definition)
		}
		if previous, exists := byID[definition.ID]; exists {
			t.Fatalf("command ID 0x%04X conflicts between %q and %q", definition.ID, previous.Name, definition.Name)
		}
		if previous, exists := byName[definition.Name]; exists {
			t.Fatalf("command name %q is assigned to both 0x%04X and 0x%04X", definition.Name, previous.ID, definition.ID)
		}
		byID[definition.ID] = definition
		byName[definition.Name] = definition
		resolved, ok := LookupCommand(uint16(definition.ID))
		if !ok || resolved != definition {
			t.Fatalf("lookup 0x%04X = %+v, %v; want %+v", definition.ID, resolved, ok, definition)
		}
	}
	if StartGameResponseCommand != StartGameCommand {
		t.Fatalf("start-game response alias = 0x%04X, want request command 0x%04X", StartGameResponseCommand, StartGameCommand)
	}
	if _, ok := LookupCommand(0xFFFF); ok {
		t.Fatal("unregistered command unexpectedly resolved")
	}
}
