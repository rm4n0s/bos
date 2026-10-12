package service

import (
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// loraWorld has two online servers that hold LoRAs and three items that carry
// them.
type loraWorld struct {
	*env
	gpu1, gpu2 *domain.Server
	c1, c2     *fakeClient
	group      *domain.Group
}

func newLoRAWorld(t *testing.T) *loraWorld {
	t.Helper()
	e := newEnv(t)
	w := &loraWorld{env: e}
	w.gpu1, w.gpu2 = e.server(t, "gpu1"), e.server(t, "gpu2")
	e.fleet.online[w.gpu1.ID], e.fleet.online[w.gpu2.ID] = true, true
	w.c1 = &fakeClient{loras: []string{"civitai/12.safetensors", "style.safetensors", "only1.safetensors"}}
	w.c2 = &fakeClient{loras: []string{`civitai\12.safetensors`, "style.safetensors"}}
	e.fleet.clients[w.gpu1.Host], e.fleet.clients[w.gpu2.Host] = w.c1, w.c2
	w.group = e.group(t, "styles")
	e.loraItem(t, w.group.ID, "a", "pa", "", domain.LoRA{Name: "civitai/12.safetensors", StrengthModel: 0.8, StrengthClip: 0.6}, domain.LoRA{Name: "style", StrengthModel: 1, StrengthClip: 1})
	e.loraItem(t, w.group.ID, "b", "pb", "", domain.LoRA{Name: "style.safetensors", StrengthModel: 0.5, StrengthClip: 0.5})
	e.loraItem(t, w.group.ID, "c", "pc", "", domain.LoRA{Name: "gone.safetensors", StrengthModel: 1, StrengthClip: 1})
	e.item(t, w.group.ID, "plain", "no lora")
	return w
}

func names(l LoRAList) []string {
	var out []string
	for _, e := range l.Items {
		out = append(out, e.Name)
	}
	return out
}

func TestLoRAListJoinsItemsAndServers(t *testing.T) {
	w := newLoRAWorld(t)
	got, err := w.LoRAs.List(ctx(t), "", domain.Page{})
	check(t, err)
	want := []string{"civitai/12.safetensors", "gone.safetensors", "only1.safetensors", "style.safetensors"}
	if !reflect.DeepEqual(names(got), want) || got.Total != 4 {
		t.Fatalf("names = %v (total %d), want %v", names(got), got.Total, want)
	}
	byName := map[string]LoRAEntry{}
	for _, e := range got.Items {
		byName[e.Name] = e
	}
	// "style" and "style.safetensors" are one LoRA; a backslash is a slash.
	if e := byName["style.safetensors"]; e.Items != 2 || !reflect.DeepEqual(e.Servers, []string{"gpu1", "gpu2"}) {
		t.Errorf("style = %+v", e)
	}
	if e := byName["civitai/12.safetensors"]; e.Items != 1 || !reflect.DeepEqual(e.Servers, []string{"gpu1", "gpu2"}) {
		t.Errorf("civitai = %+v", e)
	}
	if e := byName["gone.safetensors"]; e.Items != 1 || len(e.Servers) != 0 {
		t.Errorf("a LoRA no server has = %+v", e)
	}
	if e := byName["only1.safetensors"]; e.Items != 0 || !reflect.DeepEqual(e.Servers, []string{"gpu1"}) {
		t.Errorf("a LoRA no item carries = %+v", e)
	}
	if len(got.Unreachable) != 0 {
		t.Errorf("unreachable = %v", got.Unreachable)
	}

	// Search and paging.
	got, err = w.LoRAs.List(ctx(t), "STY", domain.Page{})
	check(t, err)
	if !reflect.DeepEqual(names(got), []string{"style.safetensors"}) {
		t.Errorf("search = %v", names(got))
	}
	got, err = w.LoRAs.List(ctx(t), "", domain.Page{Number: 2, Size: 3})
	check(t, err)
	if !reflect.DeepEqual(names(got), []string{"style.safetensors"}) || got.TotalPages != 2 || got.Page != 2 {
		t.Errorf("page 2 = %v of %d", names(got), got.TotalPages)
	}
	got, err = w.LoRAs.List(ctx(t), "", domain.Page{Number: 9, Size: 3})
	check(t, err)
	if len(got.Items) != 0 || got.Total != 4 {
		t.Errorf("a page past the end = %+v", got)
	}
}

func TestLoRAListSaysWhichServersCouldNotBeAsked(t *testing.T) {
	w := newLoRAWorld(t)
	w.fleet.online[w.gpu2.ID] = false
	got, err := w.LoRAs.List(ctx(t), "", domain.Page{})
	check(t, err)
	if !reflect.DeepEqual(got.Unreachable, []string{"gpu2"}) {
		t.Errorf("unreachable = %v", got.Unreachable)
	}
	for _, e := range got.Items {
		if slices.Contains(e.Servers, "gpu2") {
			t.Errorf("%s lists the server that was not asked", e.Name)
		}
	}

	w.fleet.online[w.gpu2.ID] = true
	w.c2.err = errors.New("ServerUnreachable", "no answer")
	got, err = w.LoRAs.List(ctx(t), "", domain.Page{})
	check(t, err)
	if !reflect.DeepEqual(got.Unreachable, []string{"gpu2"}) || got.Total != 4 {
		t.Errorf("a server that fails: unreachable %v, total %d", got.Unreachable, got.Total)
	}
}

func TestLoRASuggest(t *testing.T) {
	w := newLoRAWorld(t)
	for i := range 20 {
		w.c1.loras = append(w.c1.loras, "bulk/lora"+string(rune('a'+i))+".safetensors")
	}
	got, err := w.LoRAs.Suggest(ctx(t), "")
	check(t, err)
	if len(got) != loraSuggestLimit {
		t.Errorf("an empty search lists %d names, want %d", len(got), loraSuggestLimit)
	}
	got, err = w.LoRAs.Suggest(ctx(t), "ONLY")
	check(t, err)
	if !reflect.DeepEqual(got, []string{"only1.safetensors"}) {
		t.Errorf("suggest = %v", got)
	}
	// What starts with the text comes before what only contains it.
	got, err = w.LoRAs.Suggest(ctx(t), "12")
	check(t, err)
	if !reflect.DeepEqual(got, []string{"civitai/12.safetensors"}) {
		t.Errorf("suggest = %v", got)
	}
	got, err = w.LoRAs.Suggest(ctx(t), "s")
	check(t, err)
	if len(got) < 2 || got[0] != "style.safetensors" {
		t.Errorf("a name that starts with the text is first: %v", got)
	}
}

func TestLoRAGet(t *testing.T) {
	w := newLoRAWorld(t)
	d, err := w.LoRAs.Get(ctx(t), "style")
	check(t, err)
	if d.Name != "style.safetensors" || len(d.Items) != 2 || d.Items[0].Code != "a" || d.Items[0].GroupCode != "styles" || d.Items[1].StrengthModel != 0.5 {
		t.Errorf("detail = %+v", d)
	}
	want := []LoRAServer{{Code: "gpu1", Name: "Server gpu1", Reachable: true, Installed: true}, {Code: "gpu2", Name: "Server gpu2", Reachable: true, Installed: true}}
	if !reflect.DeepEqual(d.Servers, want) {
		t.Errorf("servers = %+v", d.Servers)
	}

	d, err = w.LoRAs.Get(ctx(t), "only1.safetensors")
	check(t, err)
	if len(d.Items) != 0 || d.Servers[0].Installed != true || d.Servers[1].Installed {
		t.Errorf("a LoRA on one server = %+v", d)
	}

	w.fleet.online[w.gpu2.ID] = false
	d, err = w.LoRAs.Get(ctx(t), "gone")
	check(t, err)
	if d.Servers[1].Reachable || d.Servers[1].Installed {
		t.Errorf("a server that was not asked = %+v", d.Servers[1])
	}

	_, err = w.LoRAs.Get(ctx(t), "nothing.safetensors")
	wantRoute(t, err, "LoRAService.Get", "LoRANotFound")
	_, err = w.LoRAs.Get(ctx(t), "  ")
	wantRoute(t, err, "LoRAService.Get", "InvalidRequest")
}

func TestLoRADeleteRemovesItEverywhere(t *testing.T) {
	w := newLoRAWorld(t)
	// A thumbnail of one of the items goes with the item.
	thumb := w.write(t, "thumbs/a.png", "png")
	a, err := w.repos.Items().Get(ctx(t), 1)
	check(t, err)
	a.Thumbnail = thumb
	_, err = w.Items.Update(ctx(t), a.ID, *a)
	check(t, err)
	// The Civitai cache holds the model and its metadata.
	w.dirs.CivitaiCache = filepath.Join(w.root, "civitai")
	w.LoRAs.Dirs = w.dirs
	model := w.write(t, "civitai/12.safetensors", "weights")
	meta := w.write(t, "civitai/12.json", "{}")
	other := w.write(t, "civitai/13.safetensors", "weights")

	got, err := w.LoRAs.Delete(ctx(t), "civitai/12")
	check(t, err)
	if got.Name != "civitai/12.safetensors" || !reflect.DeepEqual(got.Servers, []string{"gpu1", "gpu2"}) || got.Items != 1 || got.Files != 2 {
		t.Errorf("deletion = %+v", got)
	}
	if !reflect.DeepEqual(w.c1.deleted, []string{"loras/civitai/12.safetensors"}) || !reflect.DeepEqual(w.c2.deleted, []string{"loras/civitai/12.safetensors"}) {
		t.Errorf("deleted on gpu1 %v, on gpu2 %v (the server's own spelling is a slash path)", w.c1.deleted, w.c2.deleted)
	}
	if exists(model) || exists(meta) || !exists(other) {
		t.Errorf("cache: model %v, metadata %v, other %v", exists(model), exists(meta), exists(other))
	}
	if exists(thumb) {
		t.Error("the thumbnail of the deleted item is still there")
	}
	left, err := w.repos.Items().AllByGroup(ctx(t), w.group.ID)
	check(t, err)
	var codes []string
	for _, it := range left {
		codes = append(codes, it.CodeName)
	}
	if !reflect.DeepEqual(codes, []string{"b", "c", "plain"}) {
		t.Errorf("items left = %v, want only the one that carried the LoRA gone", codes)
	}
	// It is gone from the list; the other LoRAs are not.
	list, err := w.LoRAs.List(ctx(t), "", domain.Page{})
	check(t, err)
	if slices.Contains(names(list), "civitai/12.safetensors") || !slices.Contains(names(list), "style.safetensors") {
		t.Errorf("list after = %v", names(list))
	}
	// Doing it again finds nothing.
	_, err = w.LoRAs.Delete(ctx(t), "civitai/12.safetensors")
	wantRoute(t, err, "LoRAService.Delete", "LoRANotFound")
}

func TestLoRADeleteOfAnUnspelledNameReachesEveryItem(t *testing.T) {
	w := newLoRAWorld(t)
	got, err := w.LoRAs.Delete(ctx(t), "style")
	check(t, err)
	if got.Items != 2 || got.Name != "style.safetensors" {
		t.Errorf("deletion = %+v", got)
	}
	left, err := w.repos.Items().AllByGroup(ctx(t), w.group.ID)
	check(t, err)
	if len(left) != 2 {
		t.Errorf("%d items left, want the one with gone.safetensors and the plain one", len(left))
	}
}

func TestLoRADeleteOfAFileOnlyAServerHas(t *testing.T) {
	w := newLoRAWorld(t)
	got, err := w.LoRAs.Delete(ctx(t), "only1.safetensors")
	check(t, err)
	if !reflect.DeepEqual(got.Servers, []string{"gpu1"}) || got.Items != 0 || got.Files != 0 {
		t.Errorf("deletion = %+v", got)
	}
	if len(w.c2.deleted) != 0 {
		t.Errorf("gpu2 does not have it, yet was asked to delete %v", w.c2.deleted)
	}
}

func TestLoRADeleteOfAnItemOnlyLoRA(t *testing.T) {
	w := newLoRAWorld(t)
	got, err := w.LoRAs.Delete(ctx(t), "gone.safetensors")
	check(t, err)
	if len(got.Servers) != 0 || got.Items != 1 {
		t.Errorf("deletion = %+v", got)
	}
}

func TestLoRADeleteStopsWhenAServerCannotBeAsked(t *testing.T) {
	w := newLoRAWorld(t)
	w.fleet.online[w.gpu2.ID] = false
	_, err := w.LoRAs.Delete(ctx(t), "style")
	appErr := wantRouteErr(t, err, "LoRAService.Delete", "LoRADeleteFailed")
	if !reflect.DeepEqual(appErr.Metadata["servers"], []string{"gpu2"}) {
		t.Errorf("metadata = %v", appErr.Metadata)
	}
	if len(w.c1.deleted) != 0 {
		t.Errorf("nothing may be deleted while a server is unknown, got %v", w.c1.deleted)
	}
	left, err := w.repos.Items().AllByGroup(ctx(t), w.group.ID)
	check(t, err)
	if len(left) != 4 {
		t.Errorf("%d items left, want all 4", len(left))
	}
}

func TestLoRADeleteStopsWhenAServerRefuses(t *testing.T) {
	w := newLoRAWorld(t)
	w.c2.deleteErr = errors.New("ModelCrudDisabled", "deletes are off")
	_, err := w.LoRAs.Delete(ctx(t), "style")
	appErr := wantRouteErr(t, err, "LoRAService.Delete", "LoRADeleteFailed")
	if !reflect.DeepEqual(appErr.Metadata["servers"], []string{"gpu2"}) || !reflect.DeepEqual(appErr.Metadata["deleted_from"], []string{"gpu1"}) {
		t.Errorf("metadata = %v", appErr.Metadata)
	}
	left, err := w.repos.Items().AllByGroup(ctx(t), w.group.ID)
	check(t, err)
	if len(left) != 4 {
		t.Errorf("the items were deleted although a server kept the file: %d left", len(left))
	}
	// Once the server allows it, doing it again finishes the job: gpu1 no
	// longer has the file, which is not a problem.
	w.c2.deleteErr = nil
	got, err := w.LoRAs.Delete(ctx(t), "style")
	check(t, err)
	if !reflect.DeepEqual(got.Servers, []string{"gpu2"}) || got.Items != 2 {
		t.Errorf("second try = %+v", got)
	}
}

func TestLoRADeleteWithoutName(t *testing.T) {
	w := newLoRAWorld(t)
	_, err := w.LoRAs.Delete(ctx(t), "")
	wantRoute(t, err, "LoRAService.Delete", "InvalidRequest")
}
