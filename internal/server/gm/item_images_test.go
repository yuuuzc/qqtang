package gm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"qqtang/internal/game/itemcatalog"
)

func TestAppearanceCandidatesStayWithinRegistryCategoryAndSelectedRole(t *testing.T) {
	tests := []struct {
		name     string
		item     itemcatalog.Entry
		role     byte
		wantPath string
		forbid   []string
	}{
		{
			name: "clothing adornment does not borrow full cloth or another role",
			item: itemcatalog.Entry{Index: 167, RegistryCategory: "cladorn", Categories: []string{"cladorn"}}, role: 7,
			wantPath: `object\cladorn\cladorn16707_stand.img`, forbid: []string{`object\cloth\`, `cladorn16701`},
		},
		{
			name: "front pack does not borrow front head adornment",
			item: itemcatalog.Entry{Index: 1, RegistryCategory: "fpack", Categories: []string{"fpack"}}, role: 7,
			wantPath: `object\fpack\fpack10701_stand.img`, forbid: []string{`object\fhadorn\`},
		},
		{
			name: "cap allows shared and selected-role resources",
			item: itemcatalog.Entry{Index: 4, RegistryCategory: "cap", Categories: []string{"cap"}}, role: 7,
			wantPath: `object\cap\cap104_stand.img`, forbid: []string{`cap10104`, `cap10204`},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidates := appearanceCandidates(test.item, test.role)
			paths := make([]string, 0, len(candidates))
			for _, candidate := range candidates {
				paths = append(paths, candidate.path)
			}
			joined := strings.Join(paths, "\n")
			if !strings.Contains(joined, test.wantPath) {
				t.Fatalf("candidates do not contain %q:\n%s", test.wantPath, joined)
			}
			for _, forbidden := range test.forbid {
				if strings.Contains(joined, forbidden) {
					t.Fatalf("candidate crossed resource identity %q:\n%s", forbidden, joined)
				}
			}
		})
	}
}

func TestResolveItemImageUsesExactInventoryIconForXMLMaterial(t *testing.T) {
	root := t.TempDir()
	icon := filepath.Join(root, "res", "uiRes", "icon", "item", "item30002.img")
	if err := os.MkdirAll(filepath.Dir(icon), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(icon, []byte("material-icon"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := &Server{clientRoot: root}
	ref, ok := server.resolveItemImage(itemcatalog.Entry{
		ID: 30002, Name: "煮药的罐子", Kind: "material", Categories: []string{"材料"},
	}, 7)
	if !ok || ref.path != icon || ref.source != "item-icon" || !ref.tightFrame {
		t.Fatalf("material image = %+v, %t", ref, ok)
	}
}

func TestResolveItemImageDoesNotBorrowSceneItemForCosmeticIDCollision(t *testing.T) {
	root := t.TempDir()
	icon := filepath.Join(root, "res", "uiRes", "icon", "item", "item22.img")
	if err := os.MkdirAll(filepath.Dir(icon), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(icon, []byte("scene-item"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := &Server{clientRoot: root}
	ref, ok := server.resolveItemImage(itemcatalog.Entry{
		ID: 22, Index: 4, RegistryCategory: "cap", Kind: "avatar-cosmetic", Categories: []string{"cap"},
	}, 7)
	if ok {
		t.Fatalf("cosmetic borrowed colliding scene icon: %+v", ref)
	}
}

func TestResolveItemAppearanceUsesExactSharedFilePathLayer(t *testing.T) {
	root := t.TempDir()
	icon := filepath.Join(root, "res", "uiRes", "icon", "eye", "eye37.img")
	layer := filepath.Join(root, "object", "eye", "eye37_stand.img")
	for _, path := range []string{icon, layer} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, testDIMG(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{clientRoot: root}
	item := itemcatalog.Entry{ID: 136, Index: 37, RegistryCategory: "eye", Kind: "avatar-cosmetic"}
	category, ok := server.resolveItemImage(item, 7)
	if !ok || category.path != icon || category.source != "category-icon" {
		t.Fatalf("catalog icon = %+v, %t", category, ok)
	}
	appearance, ok := server.resolveItemAppearance(item, 7)
	if !ok || appearance.path != layer || appearance.source != "appearance-layer" || !appearance.tightFrame {
		t.Fatalf("equipped appearance = %+v, %t", appearance, ok)
	}
}

func TestItemImageAliasesResolveCanonicalAndExplicitOriginalResources(t *testing.T) {
	root := t.TempDir()
	petIcon := filepath.Join(root, "res", "uiRes", "icon", "petcard", "petcard1.img")
	eyeLayer := filepath.Join(root, "object", "eye", "eye123.img")
	for _, path := range []string{petIcon, eyeLayer} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, testDIMG(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(t.TempDir(), "aliases.json")
	config := `{"schema_version":1,"aliases":[` +
		`{"item_id":25001,"canonical_item_id":28001,"basis":"same pet card"},` +
		`{"item_id":3767,"relative_path":"object/eye/eye123.img","tight_frame":true,"basis":"official manifest layer"}]}`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	server := &Server{
		clientRoot: root,
		itemByID: map[uint32]itemcatalog.Entry{
			25001: {ID: 25001, Name: "普通的酷比", Kind: "pet-card"},
			28001: {ID: 28001, Index: 1, RegistryCategory: "petcard", Name: "普通的酷比", Kind: "pet-card"},
			3767:  {ID: 3767, Index: 12, RegistryCategory: "eye", Name: "08纪念·篮球", Kind: "avatar-cosmetic"},
		},
	}
	if err := WithItemImageAliases(configPath)(server); err != nil {
		t.Fatal(err)
	}
	pet, ok := server.resolveItemImage(server.itemByID[25001], 7)
	if !ok || pet.path != petIcon || pet.source != "canonical-alias" {
		t.Fatalf("pet alias = %+v, %t", pet, ok)
	}
	eye, ok := server.resolveItemImage(server.itemByID[3767], 7)
	if !ok || eye.path != eyeLayer || eye.source != "explicit-alias" || !eye.tightFrame {
		t.Fatalf("eye alias = %+v, %t", eye, ok)
	}
}

func TestNamecardBoundUsesManifestSpecificIconDirectories(t *testing.T) {
	root := t.TempDir()
	server := &Server{clientRoot: root}
	tests := []struct {
		id        uint32
		index     uint32
		directory string
	}{
		{id: 12501, index: 1, directory: "namecard"},
		{id: 12503, index: 3, directory: "namecardbound"},
	}
	for _, test := range tests {
		icon := filepath.Join(root, "res", "uiRes", "icon", test.directory, fmt.Sprintf("namecardbound%d.img", test.index))
		if err := os.MkdirAll(filepath.Dir(icon), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(icon, testDIMG(), 0o600); err != nil {
			t.Fatal(err)
		}
		ref, ok := server.resolveItemImage(itemcatalog.Entry{
			ID: test.id, Index: test.index, RegistryCategory: "namecardbound", Kind: "profile-decoration",
		}, 7)
		if !ok || ref.path != icon || ref.source != "category-icon" || !ref.tightFrame {
			t.Fatalf("namecard bound %d image = %+v, %t", test.id, ref, ok)
		}
	}
}

func TestValidateItemImagesChecksDecodingNotOnlyFileExistence(t *testing.T) {
	root := t.TempDir()
	icon := filepath.Join(root, "res", "uiRes", "icon", "cap", "cap4.img")
	if err := os.MkdirAll(filepath.Dir(icon), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(icon, []byte("not a DIMG"), 0o600); err != nil {
		t.Fatal(err)
	}
	item := itemcatalog.Entry{ID: 22, Index: 4, RegistryCategory: "cap", Kind: "avatar-cosmetic"}
	server := &Server{clientRoot: root, items: []itemcatalog.Entry{item}, itemByID: map[uint32]itemcatalog.Entry{item.ID: item}}
	if err := server.ValidateItemImages(); err == nil || !strings.Contains(err.Error(), "22:item icon is not a QQF/DIMG resource") {
		t.Fatalf("strict validation error = %v", err)
	}
	if err := os.WriteFile(icon, testDIMG(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := server.ValidateItemImages(); err != nil {
		t.Fatal(err)
	}
}
