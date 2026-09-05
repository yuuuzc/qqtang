package craftcatalog

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// CombineCatalog is the machine-readable projection of the original
// combineforge.ini v110 table. Recipes are keyed by the canonical product
// item ID; book and material IDs remain canonical itemCFG IDs.
type CombineCatalog struct {
	Version uint32          `json:"version"`
	Recipes []CombineRecipe `json:"recipes"`

	byProduct map[uint16]CombineRecipe
	byBook    map[uint16]CombineRecipe
}

type CombineRecipe struct {
	Category      string            `json:"category"`
	Name          string            `json:"name"`
	BookItemID    uint16            `json:"book_item_id"`
	ProductItemID uint16            `json:"product_item_id"`
	Description   string            `json:"description"`
	Materials     []CombineMaterial `json:"materials"`
}

type CombineMaterial struct {
	ItemID   uint16 `json:"item_id"`
	Name     string `json:"name"`
	Quantity uint32 `json:"quantity"`
}

func LoadCombineJSON(path string) (*CombineCatalog, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read combine catalog: %w", err)
	}
	var catalog CombineCatalog
	if err := json.Unmarshal(contents, &catalog); err != nil {
		return nil, fmt.Errorf("decode combine catalog: %w", err)
	}
	if err := catalog.prepare(); err != nil {
		return nil, err
	}
	return &catalog, nil
}

func (catalog *CombineCatalog) SaveJSON(path string) error {
	if catalog == nil {
		return fmt.Errorf("combine catalog is nil")
	}
	if err := catalog.prepare(); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return fmt.Errorf("encode combine catalog: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return fmt.Errorf("write combine catalog: %w", err)
	}
	return nil
}

func (catalog *CombineCatalog) Lookup(productItemID uint16) (CombineRecipe, bool) {
	if catalog == nil {
		return CombineRecipe{}, false
	}
	if catalog.byProduct == nil {
		if err := catalog.prepare(); err != nil {
			return CombineRecipe{}, false
		}
	}
	recipe, found := catalog.byProduct[productItemID]
	return recipe, found
}

func (catalog *CombineCatalog) LookupBook(bookItemID uint16) (CombineRecipe, bool) {
	if catalog == nil {
		return CombineRecipe{}, false
	}
	if catalog.byBook == nil {
		if err := catalog.prepare(); err != nil {
			return CombineRecipe{}, false
		}
	}
	recipe, found := catalog.byBook[bookItemID]
	return recipe, found
}

func (catalog *CombineCatalog) prepare() error {
	if catalog.Version == 0 || len(catalog.Recipes) == 0 {
		return fmt.Errorf("combine catalog has no version or recipes")
	}
	catalog.byProduct = make(map[uint16]CombineRecipe, len(catalog.Recipes))
	catalog.byBook = make(map[uint16]CombineRecipe, len(catalog.Recipes))
	for index := range catalog.Recipes {
		recipe := &catalog.Recipes[index]
		recipe.Category = strings.TrimSpace(recipe.Category)
		recipe.Name = strings.TrimSpace(recipe.Name)
		recipe.Description = strings.TrimSpace(recipe.Description)
		if recipe.Category == "" || recipe.Name == "" || recipe.BookItemID == 0 || recipe.ProductItemID == 0 || len(recipe.Materials) == 0 {
			return fmt.Errorf("combine recipe %d is incomplete", recipe.ProductItemID)
		}
		if _, exists := catalog.byProduct[recipe.ProductItemID]; exists {
			return fmt.Errorf("duplicate combine product item %d", recipe.ProductItemID)
		}
		if _, exists := catalog.byBook[recipe.BookItemID]; exists {
			return fmt.Errorf("duplicate combine book item %d", recipe.BookItemID)
		}
		seen := make(map[uint16]struct{}, len(recipe.Materials))
		for materialIndex := range recipe.Materials {
			material := &recipe.Materials[materialIndex]
			material.Name = strings.TrimSpace(material.Name)
			if material.ItemID == 0 || material.Quantity == 0 || material.Name == "" {
				return fmt.Errorf("combine recipe %d has incomplete material", recipe.ProductItemID)
			}
			if _, exists := seen[material.ItemID]; exists {
				return fmt.Errorf("combine recipe %d repeats material %d", recipe.ProductItemID, material.ItemID)
			}
			seen[material.ItemID] = struct{}{}
		}
		catalog.byProduct[recipe.ProductItemID] = *recipe
		catalog.byBook[recipe.BookItemID] = *recipe
	}
	return nil
}

// ParseCombineXML decodes the original client table after QQTSection has
// authenticated and expanded combineforge.ini. The decoded buffer is GB2312
// XML and may have zero-filled capacity after </ComForgeFile>.
func ParseCombineXML(reader io.Reader) (*CombineCatalog, error) {
	contents, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read decoded combine XML: %w", err)
	}
	end := bytes.Index(contents, []byte("</ComForgeFile>"))
	if end < 0 {
		return nil, fmt.Errorf("decoded combine XML has no closing root element")
	}
	contents = contents[:end+len("</ComForgeFile>")]
	decoder := xml.NewDecoder(bytes.NewReader(contents))
	decoder.CharsetReader = func(label string, input io.Reader) (io.Reader, error) {
		switch strings.ToLower(strings.TrimSpace(label)) {
		case "gb2312", "gbk", "gb18030":
			return transform.NewReader(input, simplifiedchinese.GBK.NewDecoder()), nil
		default:
			return input, nil
		}
	}

	catalog := &CombineCatalog{}
	depth := 0
	inCombine := false
	category := ""
	var recipe *CombineRecipe
	for {
		token, decodeErr := decoder.Token()
		if decodeErr == io.EOF {
			break
		}
		if decodeErr != nil {
			return nil, fmt.Errorf("decode combine XML: %w", decodeErr)
		}
		switch value := token.(type) {
		case xml.StartElement:
			depth++
			switch depth {
			case 1:
				if value.Name.Local != "ComForgeFile" {
					return nil, fmt.Errorf("combine XML root is %q", value.Name.Local)
				}
				version, parseErr := xmlUint(value.Attr, "version", 32)
				if parseErr != nil {
					return nil, parseErr
				}
				catalog.Version = uint32(version)
			case 2:
				inCombine = value.Name.Local == "合成"
			case 3:
				if inCombine {
					category = value.Name.Local
				}
			case 4:
				if inCombine {
					bookID, bookErr := xmlUint(value.Attr, "bookid", 16)
					productID, productErr := xmlUint(value.Attr, "propid", 16)
					if bookErr != nil || productErr != nil {
						return nil, fmt.Errorf("parse combine recipe %q: book=%v product=%v", value.Name.Local, bookErr, productErr)
					}
					recipe = &CombineRecipe{
						Category: category, Name: value.Name.Local, BookItemID: uint16(bookID), ProductItemID: uint16(productID),
						Description: xmlAttribute(value.Attr, "description"),
					}
				}
			case 5:
				if inCombine && recipe != nil {
					itemID, itemErr := xmlUint(value.Attr, "matid", 16)
					quantity, quantityErr := xmlUint(value.Attr, "matnum", 32)
					if itemErr != nil || quantityErr != nil {
						return nil, fmt.Errorf("parse material for recipe %d: item=%v quantity=%v", recipe.ProductItemID, itemErr, quantityErr)
					}
					recipe.Materials = append(recipe.Materials, CombineMaterial{
						ItemID: uint16(itemID), Name: xmlAttribute(value.Attr, "matname"), Quantity: uint32(quantity),
					})
				}
			}
		case xml.EndElement:
			if depth == 4 && inCombine && recipe != nil {
				catalog.Recipes = append(catalog.Recipes, *recipe)
				recipe = nil
			}
			if depth == 3 && inCombine {
				category = ""
			}
			if depth == 2 && inCombine {
				inCombine = false
			}
			depth--
		}
	}
	sort.SliceStable(catalog.Recipes, func(i, j int) bool { return catalog.Recipes[i].ProductItemID < catalog.Recipes[j].ProductItemID })
	if err := catalog.prepare(); err != nil {
		return nil, err
	}
	return catalog, nil
}

func xmlAttribute(attributes []xml.Attr, name string) string {
	for _, attribute := range attributes {
		if attribute.Name.Local == name {
			return attribute.Value
		}
	}
	return ""
}

func xmlUint(attributes []xml.Attr, name string, bits int) (uint64, error) {
	value := xmlAttribute(attributes, name)
	parsed, err := strconv.ParseUint(value, 10, bits)
	if err != nil {
		return 0, fmt.Errorf("parse XML attribute %s=%q: %w", name, value, err)
	}
	return parsed, nil
}
