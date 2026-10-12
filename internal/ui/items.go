package ui

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/uploads"
	"github.com/rm4n0s/errors"
)

// controlNetSlots is how many ControlNets the item dialog offers.
const controlNetSlots = 3

// controlNetTypes are the kinds of SPEC §3.6.
var controlNetTypes = []string{"openpose", "dwpose", "tile", "canny", "midas", "depth", "lineart", "softedge", "scribble", "normal"}

type ipForm struct {
	Enabled                            bool
	ImageURL                           string
	Weight, WeightType, StartAt, EndAt string
	ClipVisionModel, ModelName         string
}

type controlNetSlot struct {
	Slot               int
	Type, Strength     string
	ModelPattern, Keep string
	IsReference        bool
}

// itemForm is what the item dialog shows. The group's feature flags decide
// which sections appear (SPEC §16.3).
type itemForm struct {
	Group            domain.Group
	Flags            domain.Group
	ID               int64
	CodeName, Name   string
	Positive         string
	Negative         string
	Tags             string
	ThumbURL         string
	LoRAs            []loraRow
	IP               ipForm
	ControlNets      []controlNetSlot
	ControlNetTypes  []string
	MaskKeywords     []string
	Coords           string
	OutputType       string
	OutputAttributes string
}

type itemsTable struct {
	Group    domain.Group
	Items    []domain.Item
	Pager    pagerData
	ItemTags []domain.TagCount
	Blank    itemForm
	// Civitai is what the import dialog shows; only the page fills it.
	Civitai civitaiForm
}

func fmtNum(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func (u *UI) itemFormOf(g *domain.Group, it *domain.Item) itemForm {
	f := itemForm{Group: *g, Flags: *g, ControlNetTypes: controlNetTypes, IP: ipForm{Weight: "1", WeightType: "linear", StartAt: "0", EndAt: "1"}}
	for i := range controlNetSlots {
		f.ControlNets = append(f.ControlNets, controlNetSlot{Slot: i, Strength: "1"})
	}
	if it == nil {
		return f
	}
	f.ID, f.CodeName, f.Name, f.Positive, f.Negative = it.ID, it.CodeName, it.Name, it.PositivePrompt, it.NegativePrompt
	f.Tags = strings.Join(it.Tags, ", ")
	f.ThumbURL = u.fileURL("thumbnails", it.Thumbnail)
	for _, l := range it.LoRAs {
		f.LoRAs = append(f.LoRAs, loraRow{Name: l.Name, Model: fmtNum(l.StrengthModel), Clip: fmtNum(l.StrengthClip)})
	}
	if ip := it.IPAdapter; ip != nil {
		f.IP = ipForm{
			Enabled: true, ImageURL: u.fileURL("ipadapter", ip.ImageFile), Weight: fmtNum(ip.Weight), WeightType: ip.WeightType,
			StartAt: fmtNum(ip.StartAt), EndAt: fmtNum(ip.EndAt), ClipVisionModel: ip.ClipVisionModel, ModelName: ip.ModelName,
		}
	}
	for i, c := range it.ControlNets {
		if i >= controlNetSlots {
			break
		}
		f.ControlNets[i] = controlNetSlot{Slot: i, Type: c.Type, Strength: fmtNum(c.Strength), ModelPattern: c.ModelPattern, Keep: c.ImagePath, IsReference: c.IsReference}
	}
	if m := it.MaskRegion; m != nil {
		for k := range m.MaskFiles {
			f.MaskKeywords = append(f.MaskKeywords, k)
		}
		sort.Strings(f.MaskKeywords)
	}
	var coords []string
	for _, c := range it.CoordinatedRegions {
		coords = append(coords, fmt.Sprintf("%s %d %d %d %d", c.Keyword, c.Width, c.Height, c.X, c.Y))
	}
	f.Coords = strings.Join(coords, "\n")
	f.OutputType = string(it.OutputType)
	if len(it.OutputTypeAttributes) > 0 {
		if b, err := json.MarshalIndent(it.OutputTypeAttributes, "", "  "); err == nil {
			f.OutputAttributes = string(b)
		}
	}
	return f
}

// loraRow is one line of the LoRA table of the item dialog.
type loraRow struct{ Name, Model, Clip string }

// readLoRAs reads the LoRA table of the item dialog: the fields lora_name,
// lora_model and lora_clip come once for each row, in the same order. A
// strength that is left empty is 1.
func readLoRAs(f *form) ([]domain.LoRA, error) {
	names, models, clips := f.values["lora_name"], f.values["lora_model"], f.values["lora_clip"]
	var out []domain.LoRA
	for i, name := range names {
		l := domain.LoRA{Name: strings.TrimSpace(name), StrengthModel: 1, StrengthClip: 1}
		if l.Name == "" {
			return nil, errors.New("InvalidRequest", "a LoRA row has no name", "field", "lora_name")
		}
		for _, s := range []struct {
			list  []string
			field string
			dst   *float64
		}{{models, "lora_model", &l.StrengthModel}, {clips, "lora_clip", &l.StrengthClip}} {
			if i >= len(s.list) || strings.TrimSpace(s.list[i]) == "" {
				continue
			}
			n, err := strconv.ParseFloat(strings.TrimSpace(s.list[i]), 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return nil, errors.New("InvalidRequest", "the strength of the LoRA "+l.Name+" is not a number: "+s.list[i], "field", s.field, "value", s.list[i])
			}
			*s.dst = n
		}
		out = append(out, l)
	}
	return out, nil
}

// parseLoRAs reads `name, model strength, text-encoder strength` lines; the
// strengths default to 1.
func parseLoRAs(text string) ([]domain.LoRA, error) {
	var out []domain.LoRA
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, ",")
		l := domain.LoRA{Name: strings.TrimSpace(parts[0]), StrengthModel: 1, StrengthClip: 1}
		for i, dst := range []*float64{&l.StrengthModel, &l.StrengthClip} {
			if len(parts) > i+1 && strings.TrimSpace(parts[i+1]) != "" {
				n, err := strconv.ParseFloat(strings.TrimSpace(parts[i+1]), 64)
				if err != nil {
					return nil, errors.New("InvalidRequest", "a LoRA strength is not a number: "+strings.TrimSpace(line), "line", line)
				}
				*dst = n
			}
		}
		if l.Name == "" {
			return nil, errors.New("InvalidRequest", "a LoRA line has no name", "line", line)
		}
		out = append(out, l)
	}
	return out, nil
}

// parseRegions reads `keyword width height x y` lines.
func parseRegions(text string) ([]domain.CoordinatedRegion, error) {
	var out []domain.CoordinatedRegion
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 5 {
			return nil, errors.New("InvalidRequest", "a box region needs: keyword width height x y — "+strings.TrimSpace(line), "line", line)
		}
		var nums [4]int
		for i := range nums {
			n, err := strconv.Atoi(fields[i+1])
			if err != nil {
				return nil, errors.New("InvalidRequest", "a box region needs whole numbers: "+strings.TrimSpace(line), "line", line)
			}
			nums[i] = n
		}
		out = append(out, domain.CoordinatedRegion{Keyword: fields[0], Width: nums[0], Height: nums[1], X: nums[2], Y: nums[3]})
	}
	return out, nil
}

func (u *UI) itemsTable(r *http.Request, group *domain.Group) (itemsTable, error) {
	p, err := pageParam(r)
	if err != nil {
		return itemsTable{}, err
	}
	out, err := u.Services.Items.ListByGroup(r.Context(), group.ID, p)
	if err != nil {
		return itemsTable{}, err
	}
	tags, err := u.Services.Tags.List(r.Context(), domain.TagScopeItem, "")
	if err != nil {
		return itemsTable{}, err
	}
	return itemsTable{
		Group: *group, Items: out.Items, Pager: pagerOf("/ui/groups/"+strconv.FormatInt(group.ID, 10)+"/items/table", out),
		ItemTags: tags, Blank: u.itemFormOf(group, nil),
	}, nil
}

func (u *UI) itemRoutes() {
	u.handle("GET /groups/{id}/items", func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r, "id")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		g, err := u.Services.Groups.Get(r.Context(), id)
		if err != nil {
			u.notFoundOrFail(w, r, err)
			return
		}
		t, err := u.itemsTable(r, g)
		if err != nil {
			u.failPage(w, r, err)
			return
		}
		if t.Civitai, err = u.civitaiForm(r); err != nil {
			u.failPage(w, r, err)
			return
		}
		u.renderPage(w, r, "items", page{Title: g.Name, Nav: "groups", Data: t})
	})
	u.handle("GET /ui/groups/{id}/items/table", view(u, func(r *http.Request, _ *struct{}, s *stream) error {
		g, err := u.groupOf(r)
		if err != nil {
			return err
		}
		t, err := u.itemsTable(r, g)
		if err != nil {
			return err
		}
		return s.render("items_table", t)
	}))
	u.handle("GET /ui/groups/{id}/items/new", view(u, func(r *http.Request, _ *struct{}, s *stream) error {
		g, err := u.groupOf(r)
		if err != nil {
			return err
		}
		if err := s.open("item-form-body", "item_form", u.itemFormOf(g, nil)); err != nil {
			return err
		}
		return s.set(map[string]any{"items": map[string]any{"open": true}})
	}))
	u.handle("GET /ui/items/{id}/edit", view(u, func(r *http.Request, _ *struct{}, s *stream) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		it, err := u.Services.Items.Get(r.Context(), id)
		if err != nil {
			return err
		}
		g, err := u.Services.Groups.Get(r.Context(), it.GroupID)
		if err != nil {
			return err
		}
		if err := s.open("item-form-body", "item_form", u.itemFormOf(g, it)); err != nil {
			return err
		}
		return s.set(map[string]any{"items": map[string]any{"open": true}})
	}))
	u.handle("POST /ui/groups/{id}/items", u.actForm("items-error", u.saveItem))
	u.handle("DELETE /ui/items/{id}", act(u, "", func(r *http.Request, _ *struct{}, s *stream) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		it, err := u.Services.Items.Get(r.Context(), id)
		if err != nil {
			return err
		}
		g, err := u.Services.Groups.Get(r.Context(), it.GroupID)
		if err != nil {
			return err
		}
		if err := u.Services.Items.Delete(r.Context(), id); err != nil {
			return err
		}
		return u.refreshItems(r, s, g, "Item deleted.")
	}))
}

func (u *UI) groupOf(r *http.Request) (*domain.Group, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return nil, err
	}
	return u.Services.Groups.Get(r.Context(), id)
}

// notFoundOrFail answers a page request whose entity could not be read.
func (u *UI) notFoundOrFail(w http.ResponseWriter, r *http.Request, err error) {
	if appErr, ok := errors.FromError(err); ok && strings.HasSuffix(appErr.Tag, "NotFound") {
		http.NotFound(w, r)
		return
	}
	u.failPage(w, r, err)
}

// saveItem creates or updates an item from the dialog. Only the sections the
// group offers are read; what the dialog does not show is kept as it was.
func (u *UI) saveItem(r *http.Request, f *form, s *stream) (err error) {
	group, err := u.groupOf(r)
	if err != nil {
		return err
	}
	id, err := f.id()
	if err != nil {
		return err
	}
	var old *domain.Item
	in := domain.Item{}
	if id > 0 {
		if old, err = u.Services.Items.Get(r.Context(), id); err != nil {
			return err
		}
		if old.GroupID != group.ID {
			return errors.New("InvalidRequest", "the item belongs to another group")
		}
		in = *old
		in.LoRAs = append([]domain.LoRA(nil), old.LoRAs...)
		in.ControlNets = append([]domain.ControlNet(nil), old.ControlNets...)
	}
	in.CodeName, in.Name = f.str("code_name"), f.str("name")
	in.PositivePrompt, in.NegativePrompt, in.Tags = f.raw("positive_prompt"), f.raw("negative_prompt"), splitList(f.str("tags"))
	if f.checked("clear_thumbnail") {
		in.Thumbnail = ""
	}

	set := uploads.NewSet()
	defer func() {
		if err != nil {
			uploads.Discard(u.Files, set, u.Logger)
		}
	}()
	d := u.Dirs
	if _, err := u.upload(f, "thumbnail", "thumbnail", uploads.Kind{Dir: d.Thumbnails, MIMEs: uploads.ThumbnailTypes}, set); err != nil {
		return err
	}
	if in.LoRAs, err = readLoRAs(f); err != nil {
		return err
	}
	if group.UseIPAdapter {
		if err := u.readIPAdapter(f, &in, set); err != nil {
			return err
		}
	}
	if group.UseControlnet {
		if err := u.readControlNets(f, &in, set); err != nil {
			return err
		}
	}
	if group.UseMaskRegion {
		if f.checked("clear_mask") {
			in.MaskRegion = nil
		}
		if ok, err := u.upload(f, "mask_reference", "mask_reference", uploads.Kind{Dir: d.Regions, MIMEs: uploads.MaskTypes}, set); err != nil {
			return err
		} else if ok && in.MaskRegion == nil {
			in.MaskRegion = &domain.MaskRegion{}
		}
	}
	if group.UseCoordinatesRegion {
		if in.CoordinatedRegions, err = parseRegions(f.raw("coords")); err != nil {
			return err
		}
	}
	if group.UseTypeAttributes {
		in.OutputType = domain.OutputType(f.str("output_type"))
		in.OutputTypeAttributes = nil
		if attrs := strings.TrimSpace(f.raw("output_attributes")); attrs != "" {
			if err := json.Unmarshal([]byte(attrs), &in.OutputTypeAttributes); err != nil {
				return errors.New("InvalidRequest", "the output attributes are not valid JSON: "+err.Error(), "field", "output_attributes")
			}
		}
	}

	var existing []string
	if old != nil {
		existing = old.Files()
	}
	if err := uploads.ApplyItem(&in, set, existing); err != nil {
		return err
	}
	if id > 0 {
		_, err = u.Services.Items.Update(r.Context(), id, in)
	} else {
		_, err = u.Services.Items.Create(r.Context(), group.ID, in)
	}
	if err != nil {
		return err
	}
	if err := s.set(map[string]any{"items": map[string]any{"open": false}}); err != nil {
		return err
	}
	if err := s.into("items-error", "error_text", ""); err != nil {
		return err
	}
	return u.refreshItems(r, s, group, "Item saved.")
}

func (u *UI) readIPAdapter(f *form, in *domain.Item, set *uploads.Set) error {
	if !f.checked("ip_enabled") {
		in.IPAdapter = nil
		return nil
	}
	ip := &domain.IPAdapter{WeightType: f.str("ip_weight_type"), ClipVisionModel: f.str("ip_clip_vision_model"), ModelName: f.str("ip_model_name")}
	if in.IPAdapter != nil {
		ip.ImageFile = in.IPAdapter.ImageFile
	}
	var err error
	for name, dst := range map[string]*float64{"ip_weight": &ip.Weight, "ip_start_at": &ip.StartAt, "ip_end_at": &ip.EndAt} {
		if *dst, err = f.decimal(name); err != nil {
			return err
		}
	}
	in.IPAdapter = ip
	_, err = u.upload(f, "ip_adapter_image", "ip_adapter_image", uploads.Kind{Dir: u.Dirs.IPAdapter, MIMEs: uploads.ReferenceTypes}, set)
	return err
}

// readControlNets reads the slots of the dialog. A slot with a type is a
// ControlNet; the slots are packed, so the file of the third slot may become
// the image of the second ControlNet.
func (u *UI) readControlNets(f *form, in *domain.Item, set *uploads.Set) error {
	var list []domain.ControlNet
	for slot := range controlNetSlots {
		prefix := "cn_" + strconv.Itoa(slot) + "_"
		typ := f.str(prefix + "type")
		if typ == "" {
			continue
		}
		strength, err := f.decimal(prefix + "strength")
		if err != nil {
			return err
		}
		c := domain.ControlNet{
			Type: typ, Strength: strength, ModelPattern: f.str(prefix + "model_pattern"),
			IsReference: f.checked(prefix + "reference"), ImagePath: f.str(prefix + "keep"),
		}
		index := len(list)
		if _, err := u.upload(f, prefix+"image", "controlnet_"+strconv.Itoa(index), uploads.Kind{Dir: u.Dirs.ControlNet, MIMEs: uploads.ReferenceTypes}, set); err != nil {
			return err
		}
		list = append(list, c)
	}
	in.ControlNets = list
	return nil
}

func (u *UI) refreshItems(r *http.Request, s *stream, group *domain.Group, message string) error {
	t, err := u.itemsTable(r, group)
	if err != nil {
		return err
	}
	if err := s.render("items_table", t); err != nil {
		return err
	}
	if err := s.set(map[string]any{"_armed": 0}); err != nil {
		return err
	}
	return s.toast("ok", message)
}
