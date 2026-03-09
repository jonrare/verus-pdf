// Package forms provides PDF interactive form (AcroForm) services.
//
// Reads and fills PDF form fields (text fields, checkboxes, dropdowns)
// using pdfcpu's low-level xref table access.
//
// Spec: PDF 32000-1:2008 §12.7 (Interactive Forms)
package forms

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type Service struct{}

func New() *Service { return &Service{} }

type Result struct {
	OutputPath string `json:"outputPath"`
	Error      string `json:"error,omitempty"`
}

// FormField describes a single interactive form field.
type FormField struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Value    string   `json:"value"`
	Default  string   `json:"default"`
	Options  []string `json:"options"`
	PageNum  int      `json:"pageNum"`
	X        float64  `json:"x"`
	Y        float64  `json:"y"`
	Width    float64  `json:"width"`
	Height   float64  `json:"height"`
	ReadOnly bool     `json:"readOnly"`
	MaxLen   int      `json:"maxLen"`
	FontSize float64  `json:"fontSize"` // from /DA, 0 = auto-size to fit field
	Comb     bool     `json:"comb"`     // §12.7.4.3 bit 25: divide field into MaxLen equal cells
	Multiline bool    `json:"multiline"` // §12.7.4.3 bit 13: multi-line text field
	OnValue  string   `json:"onValue"`  // for checkboxes: the "on" appearance name (e.g. "1", "Yes")
}

// GetFormFields returns all interactive form fields in the PDF.
func (s *Service) GetFormFields(inputPath string) ([]FormField, error) {
	f, err := os.Open(inputPath)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	conf := model.NewDefaultConfiguration()
	ctx, err := api.ReadValidateAndOptimize(f, conf)
	if err != nil {
		return nil, fmt.Errorf("read PDF: %w", err)
	}

	// Get the document root
	rootDict, err := catalogDict(ctx)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}

	// Look for /AcroForm (may be absent — scan page annotations as fallback)
	var fieldsArr types.Array
	afObj, found := rootDict.Find("AcroForm")
	if found {
		afDict, _ := ctx.DereferenceDict(afObj)
		if afDict != nil {
			fieldsObj, found := afDict.Find("Fields")
			if found {
				fieldsDeref, err := ctx.Dereference(fieldsObj)
				if err == nil {
					fieldsArr, _ = fieldsDeref.(types.Array)
				}
			}
		}
	}

	// Build page lookup: annotation object number → page number
	pageMap := buildPageAnnotMap(ctx)

	// Walk the field tree from /AcroForm /Fields
	var fields []FormField
	seenIDs := make(map[string]bool)
	for _, fieldRef := range fieldsArr {
		walkField(ctx, fieldRef, "", pageMap, &fields)
	}
	for _, f := range fields {
		seenIDs[f.ID] = true
	}

	// Also scan page annotations for widget annotations that have /FT
	// directly on them (common in XFA-converted forms like the W-9).
	// These widgets are on the page's /Annots array but not in /Fields.
	for pageNum := 1; pageNum <= ctx.PageCount; pageNum++ {
		pageDict, _, _, err := ctx.PageDict(pageNum, false)
		if err != nil || pageDict == nil {
			continue
		}
		annotsObj, found := pageDict.Find("Annots")
		if !found {
			continue
		}
		annotsDeref, err := ctx.Dereference(annotsObj)
		if err != nil {
			continue
		}
		annotsArr, ok := annotsDeref.(types.Array)
		if !ok {
			continue
		}
		for _, annotRef := range annotsArr {
			// Get object number to check if already seen
			id := ""
			if ir, ok := annotRef.(types.IndirectRef); ok {
				id = fmt.Sprintf("%d", ir.ObjectNumber.Value())
				if seenIDs[id] {
					continue
				}
			}

			annotDeref, err := ctx.Dereference(annotRef)
			if err != nil {
				continue
			}
			annotDict, ok := annotDeref.(types.Dict)
			if !ok {
				continue
			}

			// Must be a Widget annotation
			subtype, _ := annotDict.Find("Subtype")
			if st, ok := subtype.(types.Name); !ok || st.Value() != "Widget" {
				continue
			}

			// Must have /FT (field type) — either directly or via /Parent
			ft := inheritedName(ctx, annotDict, "FT")
			if ft == "" {
				continue
			}

			ff := extractField(ctx, annotDict, "", pageMap, annotRef)
			if ff == nil {
				continue
			}

			// Build the name from /T
			if t, found := annotDict.Find("T"); found {
				ff.Name = stringVal(ctx, t)
			}
			if ff.Name == "" && id != "" {
				ff.Name = "field_" + id
			}
			ff.PageNum = pageNum
			ff.ID = id

			if !seenIDs[ff.ID] {
				seenIDs[ff.ID] = true
				fields = append(fields, *ff)
			}
		}
	}

	return fields, nil
}

// FillFormFields sets form field values and writes the result.
func (s *Service) FillFormFields(inputPath, outputPath string, values map[string]string) Result {
	// Preflight: verify input file exists and isn't empty
	info, err := os.Stat(inputPath)
	if err != nil {
		return Result{Error: "open: " + err.Error()}
	}
	if info.Size() == 0 {
		return Result{Error: "The file appears to be empty. Try saving the document first."}
	}

	f, err := os.Open(inputPath)
	if err != nil {
		return Result{Error: "open: " + err.Error()}
	}
	conf := model.NewDefaultConfiguration()
	ctx, err := api.ReadValidateAndOptimize(f, conf)
	f.Close()
	if err != nil {
		return Result{Error: "read PDF: " + err.Error()}
	}

	rootDict, err := catalogDict(ctx)
	if err != nil {
		return Result{Error: "catalog: " + err.Error()}
	}

	afObj, found := rootDict.Find("AcroForm")
	var afDict types.Dict
	if found {
		afDict, _ = ctx.DereferenceDict(afObj)
	}

	// Walk /Fields tree and set values
	filled := 0
	var fieldsArr types.Array
	if afDict != nil {
		fieldsObj, found := afDict.Find("Fields")
		if found {
			fieldsDeref, _ := ctx.Dereference(fieldsObj)
			fieldsArr, _ = fieldsDeref.(types.Array)
		}
	}
	for _, fieldRef := range fieldsArr {
		filled += fillField(ctx, fieldRef, "", values)
	}

	// Also scan page annotations for orphan widget fields
	for pageNum := 1; pageNum <= ctx.PageCount; pageNum++ {
		pageDict, _, _, err := ctx.PageDict(pageNum, false)
		if err != nil || pageDict == nil {
			continue
		}
		annotsObj, found := pageDict.Find("Annots")
		if !found {
			continue
		}
		annotsDeref, err := ctx.Dereference(annotsObj)
		if err != nil {
			continue
		}
		annotsArr, ok := annotsDeref.(types.Array)
		if !ok {
			continue
		}
		for _, annotRef := range annotsArr {
			annotDeref, err := ctx.Dereference(annotRef)
			if err != nil {
				continue
			}
			annotDict, ok := annotDeref.(types.Dict)
			if !ok {
				continue
			}
			// Must be a Widget with /FT
			subtype, _ := annotDict.Find("Subtype")
			if st, ok := subtype.(types.Name); !ok || st.Value() != "Widget" {
				continue
			}
			ft := inheritedName(ctx, annotDict, "FT")
			if ft == "" {
				continue
			}
			// Match by /T name or object ID
			fieldName := ""
			if t, found := annotDict.Find("T"); found {
				fieldName = stringVal(ctx, t)
			}
			newVal, matched := values[fieldName]
			if !matched {
				if ir, ok := annotRef.(types.IndirectRef); ok {
					id := fmt.Sprintf("%d", ir.ObjectNumber.Value())
					newVal, matched = values[id]
				}
			}
			if !matched {
				continue
			}
			// Set value
			annotDict["V"] = types.StringLiteral(newVal)
			delete(annotDict, "AP")
			if ir, ok := annotRef.(types.IndirectRef); ok {
				objNr := ir.ObjectNumber.Value()
				entry, exists := ctx.XRefTable.Find(objNr)
				if exists {
					entry.Object = annotDict
				}
			}
			filled++
		}
	}

	// Set NeedAppearances to true so viewers regenerate field appearance streams
	if afDict != nil {
		afDict["NeedAppearances"] = types.Boolean(true)
	}

	// Write to a temp file first, then rename — prevents empty output on failure
	tmpPath := outputPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return Result{Error: "create output: " + err.Error()}
	}

	if err := api.WriteContext(ctx, out); err != nil {
		out.Close()
		os.Remove(tmpPath)
		return Result{Error: "write PDF: " + err.Error()}
	}
	out.Close()

	// Atomic-ish rename (safe on same filesystem)
	if err := os.Rename(tmpPath, outputPath); err != nil {
		os.Remove(tmpPath)
		return Result{Error: "finalize output: " + err.Error()}
	}

	return Result{OutputPath: outputPath}
}

// LockForm flattens all form fields (makes them non-editable).
func (s *Service) LockForm(inputPath, outputPath string) Result {
	return Result{Error: "not yet implemented: lock form"}
}

// ResetForm clears all field values.
func (s *Service) ResetForm(inputPath, outputPath string) Result {
	f, err := os.Open(inputPath)
	if err != nil {
		return Result{Error: "open: " + err.Error()}
	}
	conf := model.NewDefaultConfiguration()
	ctx, err := api.ReadValidateAndOptimize(f, conf)
	f.Close()
	if err != nil {
		return Result{Error: "read PDF: " + err.Error()}
	}

	rootDict, err := catalogDict(ctx)
	if err != nil {
		return Result{Error: "catalog: " + err.Error()}
	}

	afObj, found := rootDict.Find("AcroForm")
	if !found {
		return Result{Error: "no form fields"}
	}

	afDict, err := ctx.DereferenceDict(afObj)
	if err != nil || afDict == nil {
		return Result{Error: "invalid AcroForm"}
	}

	fieldsObj, found := afDict.Find("Fields")
	if !found {
		return Result{Error: "no fields"}
	}

	fieldsDeref, err := ctx.Dereference(fieldsObj)
	if err != nil {
		return Result{Error: err.Error()}
	}

	fieldsArr, ok := fieldsDeref.(types.Array)
	if !ok {
		return Result{Error: "fields not array"}
	}

	for _, fieldRef := range fieldsArr {
		clearField(ctx, fieldRef)
	}

	afDict["NeedAppearances"] = types.Boolean(true)

	out, err := os.Create(outputPath)
	if err != nil {
		return Result{Error: "create output: " + err.Error()}
	}
	defer out.Close()

	if err := api.WriteContext(ctx, out); err != nil {
		return Result{Error: "write: " + err.Error()}
	}

	return Result{OutputPath: outputPath}
}


// catalogDict safely gets the document catalog (root) dictionary.
func catalogDict(ctx *model.Context) (types.Dict, error) {
	if ctx.XRefTable.Root == nil {
		return nil, fmt.Errorf("no document root")
	}
	d, err := ctx.DereferenceDict(*ctx.XRefTable.Root)
	if err != nil {
		return nil, fmt.Errorf("dereference root: %w", err)
	}
	if d == nil {
		return nil, fmt.Errorf("empty root dict")
	}
	return d, nil
}

// ── Field tree walking ───────────────────────────────────────────────────────

func walkField(ctx *model.Context, obj types.Object, parentName string, pageMap map[int]int, fields *[]FormField) {
	deref, err := ctx.Dereference(obj)
	if err != nil {
		return
	}

	fieldDict, ok := deref.(types.Dict)
	if !ok {
		return
	}

	// Build full field name
	localName := ""
	if t, found := fieldDict.Find("T"); found {
		localName = stringVal(ctx, t)
	}
	fullName := localName
	if parentName != "" && localName != "" {
		fullName = parentName + "." + localName
	} else if parentName != "" {
		fullName = parentName
	}

	// Check for Kids (non-terminal node)
	kidsObj, hasKids := fieldDict.Find("Kids")
	if hasKids {
		kidsDeref, err := ctx.Dereference(kidsObj)
		if err == nil {
			if kidsArr, ok := kidsDeref.(types.Array); ok {
				// Check if kids are field nodes or widget annotations
				// If a kid has /T it's a field; otherwise it's a widget
				hasFieldKids := false
				for _, kid := range kidsArr {
					kidDeref, _ := ctx.Dereference(kid)
					if kidDict, ok := kidDeref.(types.Dict); ok {
						if _, hasT := kidDict.Find("T"); hasT {
							hasFieldKids = true
							break
						}
					}
				}
				if hasFieldKids {
					for _, kid := range kidsArr {
						walkField(ctx, kid, fullName, pageMap, fields)
					}
					return
				}
				// Kids are widgets — use the first one for geometry
				// but the field dict for type/value
			}
		}
	}

	// This is a terminal field (possibly with widget kids)
	ff := extractField(ctx, fieldDict, fullName, pageMap, obj)
	if ff != nil {
		*fields = append(*fields, *ff)
	}
}

func extractField(ctx *model.Context, fieldDict types.Dict, name string, pageMap map[int]int, origRef types.Object) *FormField {
	ff := &FormField{
		Name: name,
	}

	// Field type (/FT)
	ft := inheritedName(ctx, fieldDict, "FT")
	switch ft {
	case "Tx":
		ff.Type = "text"
	case "Btn":
		flags := inheritedInt(ctx, fieldDict, "Ff")
		if flags&(1<<16) != 0 {
			ff.Type = "radio"
		} else if flags&(1<<15) != 0 {
			ff.Type = "button"
		} else {
			ff.Type = "checkbox"
		}
	case "Ch":
		flags := inheritedInt(ctx, fieldDict, "Ff")
		if flags&(1<<17) != 0 {
			ff.Type = "dropdown"
		} else {
			ff.Type = "listbox"
		}
	case "Sig":
		ff.Type = "signature"
	default:
		ff.Type = "unknown"
	}

	// Read-only flag (bit 1)
	flags := inheritedInt(ctx, fieldDict, "Ff")
	ff.ReadOnly = flags&1 != 0

	// Comb flag — §12.7.4.3 bit 25 (1-indexed) = bit 24 (0-indexed)
	// Field is divided into MaxLen equally spaced cells
	ff.Comb = flags&(1<<24) != 0

	// Multiline flag — §12.7.4.3 bit 13 (1-indexed) = bit 12 (0-indexed)
	ff.Multiline = flags&(1<<12) != 0

	// For checkboxes: find the "on" appearance name from /AP/N
	if ff.Type == "checkbox" {
		ff.OnValue = findCheckboxOnValue(ctx, fieldDict)
		if ff.OnValue == "" {
			ff.OnValue = "Yes"
		}
	}

	// Value (/V)
	if v, found := fieldDict.Find("V"); found {
		ff.Value = stringVal(ctx, v)
	}

	// Default value (/DV)
	if dv, found := fieldDict.Find("DV"); found {
		ff.Default = stringVal(ctx, dv)
	}

	// Max length (/MaxLen)
	if ml, found := fieldDict.Find("MaxLen"); found {
		d, _ := ctx.Dereference(ml)
		if i, ok := d.(types.Integer); ok {
			ff.MaxLen = int(i)
		}
	}

	// Default Appearance (/DA) — extract font size
	// Format: "/FontName size Tf color g" e.g. "/Helvetica 9 Tf 0 g"
	ff.FontSize = parseDAFontSize(ctx, fieldDict)

	// Options (/Opt) for choice fields
	if optObj, found := fieldDict.Find("Opt"); found {
		optDeref, _ := ctx.Dereference(optObj)
		if optArr, ok := optDeref.(types.Array); ok {
			for _, o := range optArr {
				ff.Options = append(ff.Options, stringVal(ctx, o))
			}
		}
	}

	// Object number for ID
	if ir, ok := origRef.(types.IndirectRef); ok {
		ff.ID = fmt.Sprintf("%d", ir.ObjectNumber.Value())
	} else {
		ff.ID = name
	}

	// Widget geometry — check the field dict itself first, then first kid widget
	rect := getRect(ctx, fieldDict)
	if rect == nil {
		// Check Kids for widget annotations
		if kidsObj, found := fieldDict.Find("Kids"); found {
			kidsDeref, _ := ctx.Dereference(kidsObj)
			if kidsArr, ok := kidsDeref.(types.Array); ok {
				for _, kid := range kidsArr {
					kidDeref, _ := ctx.Dereference(kid)
					if kidDict, ok := kidDeref.(types.Dict); ok {
						rect = getRect(ctx, kidDict)
						if rect != nil {
							// Get page from widget kid
							if ir, ok := kid.(types.IndirectRef); ok {
								if pn, ok := pageMap[ir.ObjectNumber.Value()]; ok {
									ff.PageNum = pn
								}
							}
							break
						}
					}
				}
			}
		}
	}

	if rect != nil && len(rect) == 4 {
		ff.X = rect[0]
		ff.Y = rect[1]
		ff.Width = rect[2] - rect[0]
		ff.Height = rect[3] - rect[1]
		if ff.Width < 0 {
			ff.X = rect[2]
			ff.Width = -ff.Width
		}
		if ff.Height < 0 {
			ff.Y = rect[3]
			ff.Height = -ff.Height
		}
	}

	// Page number from /P or from annotation map
	if ff.PageNum == 0 {
		if p, found := fieldDict.Find("P"); found {
			if ir, ok := p.(types.IndirectRef); ok {
				ff.PageNum = findPageNum(ctx, ir)
			}
		}
	}
	if ff.PageNum == 0 {
		if ir, ok := origRef.(types.IndirectRef); ok {
			if pn, ok := pageMap[ir.ObjectNumber.Value()]; ok {
				ff.PageNum = pn
			}
		}
	}
	if ff.PageNum == 0 {
		ff.PageNum = 1 // fallback
	}

	return ff
}

// ── Fill / Clear ─────────────────────────────────────────────────────────────

func fillField(ctx *model.Context, obj types.Object, parentName string, values map[string]string) int {
	deref, err := ctx.Dereference(obj)
	if err != nil {
		return 0
	}
	fieldDict, ok := deref.(types.Dict)
	if !ok {
		return 0
	}

	localName := ""
	if t, found := fieldDict.Find("T"); found {
		localName = stringVal(ctx, t)
	}
	fullName := localName
	if parentName != "" && localName != "" {
		fullName = parentName + "." + localName
	} else if parentName != "" {
		fullName = parentName
	}

	// Recurse into kids
	if kidsObj, found := fieldDict.Find("Kids"); found {
		kidsDeref, _ := ctx.Dereference(kidsObj)
		if kidsArr, ok := kidsDeref.(types.Array); ok {
			count := 0
			for _, kid := range kidsArr {
				count += fillField(ctx, kid, fullName, values)
			}
			if count > 0 {
				return count
			}
		}
	}

	// Terminal field — check if we have a value for it
	newVal, found := values[fullName]
	if !found {
		// Also try by ID
		if ir, ok := obj.(types.IndirectRef); ok {
			id := fmt.Sprintf("%d", ir.ObjectNumber.Value())
			newVal, found = values[id]
		}
	}
	if !found {
		return 0
	}

	// Set /V
	fieldDict["V"] = types.StringLiteral(newVal)

	// Remove /AP (appearance stream) so viewers regenerate it
	delete(fieldDict, "AP")

	// Write back
	if ir, ok := obj.(types.IndirectRef); ok {
		objNr := ir.ObjectNumber.Value()
		entry, exists := ctx.XRefTable.Find(objNr)
		if exists {
			entry.Object = fieldDict
		}
	}

	return 1
}

func clearField(ctx *model.Context, obj types.Object) {
	deref, err := ctx.Dereference(obj)
	if err != nil {
		return
	}
	fieldDict, ok := deref.(types.Dict)
	if !ok {
		return
	}

	// Recurse
	if kidsObj, found := fieldDict.Find("Kids"); found {
		kidsDeref, _ := ctx.Dereference(kidsObj)
		if kidsArr, ok := kidsDeref.(types.Array); ok {
			for _, kid := range kidsArr {
				clearField(ctx, kid)
			}
		}
	}

	// Clear value
	delete(fieldDict, "V")
	delete(fieldDict, "AP")

	if ir, ok := obj.(types.IndirectRef); ok {
		objNr := ir.ObjectNumber.Value()
		entry, exists := ctx.XRefTable.Find(objNr)
		if exists {
			entry.Object = fieldDict
		}
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// buildPageAnnotMap builds a map from annotation object numbers to page numbers.
func buildPageAnnotMap(ctx *model.Context) map[int]int {
	m := make(map[int]int)
	for i := 1; i <= ctx.PageCount; i++ {
		pageDict, _, _, err := ctx.PageDict(i, false)
		if err != nil {
			continue
		}
		annotsObj, found := pageDict.Find("Annots")
		if !found {
			continue
		}
		annotsDeref, err := ctx.Dereference(annotsObj)
		if err != nil {
			continue
		}
		annotsArr, ok := annotsDeref.(types.Array)
		if !ok {
			continue
		}
		for _, annot := range annotsArr {
			if ir, ok := annot.(types.IndirectRef); ok {
				m[ir.ObjectNumber.Value()] = i
			}
		}
	}
	return m
}

func findPageNum(ctx *model.Context, pageRef types.IndirectRef) int {
	for i := 1; i <= ctx.PageCount; i++ {
		pageDict, ir, _, err := ctx.PageDict(i, false)
		if err != nil || pageDict == nil {
			continue
		}
		if ir != nil && ir.ObjectNumber == pageRef.ObjectNumber {
			return i
		}
	}
	return 0
}

func getRect(ctx *model.Context, d types.Dict) []float64 {
	rectObj, found := d.Find("Rect")
	if !found {
		return nil
	}
	deref, err := ctx.Dereference(rectObj)
	if err != nil {
		return nil
	}
	arr, ok := deref.(types.Array)
	if !ok || len(arr) < 4 {
		return nil
	}
	var r []float64
	for _, elem := range arr[:4] {
		d, _ := ctx.Dereference(elem)
		switch v := d.(type) {
		case types.Integer:
			r = append(r, float64(v))
		case types.Float:
			r = append(r, float64(v))
		default:
			r = append(r, 0)
		}
	}
	return r
}

func stringVal(ctx *model.Context, obj types.Object) string {
	deref, err := ctx.Dereference(obj)
	if err != nil {
		return ""
	}
	switch v := deref.(type) {
	case types.StringLiteral:
		return string(v)
	case types.HexLiteral:
		return string(v)
	case types.Name:
		return v.Value()
	}
	return ""
}

// inheritedName walks up the field hierarchy to find a /Name entry.
func inheritedName(ctx *model.Context, d types.Dict, key string) string {
	obj, found := d.Find(key)
	if found {
		deref, _ := ctx.Dereference(obj)
		if n, ok := deref.(types.Name); ok {
			return n.Value()
		}
	}
	// Try parent
	parentObj, found := d.Find("Parent")
	if found {
		parentDeref, _ := ctx.Dereference(parentObj)
		if parentDict, ok := parentDeref.(types.Dict); ok {
			return inheritedName(ctx, parentDict, key)
		}
	}
	return ""
}

func inheritedInt(ctx *model.Context, d types.Dict, key string) int {
	obj, found := d.Find(key)
	if found {
		deref, _ := ctx.Dereference(obj)
		if i, ok := deref.(types.Integer); ok {
			return int(i)
		}
	}
	parentObj, found := d.Find("Parent")
	if found {
		parentDeref, _ := ctx.Dereference(parentObj)
		if parentDict, ok := parentDeref.(types.Dict); ok {
			return inheritedInt(ctx, parentDict, key)
		}
	}
	return 0
}


// parseDAFontSize extracts the font size from a /DA (Default Appearance) string.
// DA format: "/FontName size Tf [color operators]"
// Example: "/Helvetica 9 Tf 0 g"
func parseDAFontSize(ctx *model.Context, d types.Dict) float64 {
	daObj, found := d.Find("DA")
	if !found {
		// Try parent
		parentObj, found := d.Find("Parent")
		if found {
			parentDeref, _ := ctx.Dereference(parentObj)
			if parentDict, ok := parentDeref.(types.Dict); ok {
				return parseDAFontSize(ctx, parentDict)
			}
		}
		return 0
	}

	da := stringVal(ctx, daObj)
	if da == "" {
		return 0
	}

	// Find "Tf" and extract the number before it
	parts := strings.Fields(da)
	for i, part := range parts {
		if part == "Tf" && i >= 1 {
			size, err := strconv.ParseFloat(parts[i-1], 64)
			if err == nil && size > 0 {
				return size
			}
		}
	}
	return 0
}

// findCheckboxOnValue extracts the "on" appearance name from /AP/N.
// Checkboxes have two appearance states: /Off and one "on" state
// whose name varies (e.g., /Yes, /1, /2, /On).
func findCheckboxOnValue(ctx *model.Context, d types.Dict) string {
	apObj, found := d.Find("AP")
	if !found {
		return ""
	}
	apDict, err := ctx.DereferenceDict(apObj)
	if err != nil || apDict == nil {
		return ""
	}
	nObj, found := apDict.Find("N")
	if !found {
		return ""
	}
	nDict, err := ctx.DereferenceDict(nObj)
	if err != nil || nDict == nil {
		return ""
	}
	// Find the key that isn't "Off"
	for key := range nDict {
		if key != "Off" {
			return key
		}
	}
	return ""
}
