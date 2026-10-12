package service

import (
	"context"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/bos/internal/civitai"
	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// loraFolder is the model folder of ComfyUI that holds LoRAs.
const loraFolder = "loras"

// loraListTimeout bounds the question "which LoRAs do you have?" to one server.
const loraListTimeout = 10 * time.Second

// loraSuggestLimit is how many names one autocomplete lists at most.
const loraSuggestLimit = 12

// LoRAService is the LoRA pages (SPEC §9.1): the LoRAs of all items and of all
// ComfyUI servers in one list, where each is installed, and its deletion. A
// LoRA has no record of its own; it is a file name that items carry and that
// servers hold, and two spellings of the same file (`style` and
// `style.safetensors`) are one LoRA.
type LoRAService struct{ base }

// LoRAEntry is one row of the LoRA list.
type LoRAEntry struct {
	// Name is the file name ComfyUI knows the LoRA by: slashes, never
	// backslashes, and an extension.
	Name string `json:"name"`
	// Items is how many items carry it.
	Items int `json:"items"`
	// Servers are the code names of the servers that have it installed, sorted.
	Servers []string `json:"servers"`
}

// LoRAList is one page of the LoRA list.
type LoRAList struct {
	domain.Paged[LoRAEntry]
	// Unreachable are the code names of the servers that could not be asked, so
	// their LoRAs are not in the list.
	Unreachable []string `json:"unreachable"`
}

// LoRAItem is an item that carries a LoRA, with the strengths it gives it.
type LoRAItem struct {
	ID            domain.ID `json:"id"`
	GroupID       domain.ID `json:"group_id"`
	GroupCode     string    `json:"group_code"`
	Code          string    `json:"code_name"`
	Name          string    `json:"name"`
	StrengthModel float64   `json:"strength_model"`
	StrengthClip  float64   `json:"strength_clip"`
}

// LoRAServer says whether one server has a LoRA.
type LoRAServer struct {
	Code string `json:"code_name"`
	Name string `json:"name"`
	// Reachable is false when the server could not be asked; Installed is then
	// not known and false.
	Reachable bool `json:"reachable"`
	Installed bool `json:"installed"`
}

// LoRADetail is what the page of one LoRA shows.
type LoRADetail struct {
	Name    string       `json:"name"`
	Items   []LoRAItem   `json:"items"`
	Servers []LoRAServer `json:"servers"`
}

// LoRADeletion is what a deletion removed.
type LoRADeletion struct {
	Name string `json:"name"`
	// Servers are the code names of the servers the file was deleted from.
	Servers []string `json:"servers"`
	// Items is how many items were deleted because they carried the LoRA.
	Items int `json:"items"`
	// Files is how many files of the Civitai cache were removed.
	Files int `json:"files"`
}

// loraSurvey is what the items and the servers say about LoRAs right now.
type loraSurvey struct {
	servers []domain.Server
	// installed maps a server code to its LoRAs: the normalised name to the
	// name the server lists it by.
	installed map[string]map[string]string
	// unreachable are the servers that could not be asked, with the reason.
	unreachable map[string]error
	// usage maps a normalised name to the items that carry it.
	usage map[string][]LoRAItem
}

// names returns every normalised name, sorted without regard to case.
func (v *loraSurvey) names() []string {
	seen := map[string]bool{}
	for n := range v.usage {
		seen[n] = true
	}
	for _, list := range v.installed {
		for n := range list {
			seen[n] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i]), strings.ToLower(out[j])
		if a != b {
			return a < b
		}
		return out[i] < out[j]
	})
	return out
}

// installedOn returns the codes, sorted, of the servers that have the LoRA.
func (v *loraSurvey) installedOn(name string) []string {
	out := []string{}
	for code, list := range v.installed {
		if _, ok := list[name]; ok {
			out = append(out, code)
		}
	}
	sort.Strings(out)
	return out
}

func (v *loraSurvey) unreachableCodes() []string {
	out := make([]string, 0, len(v.unreachable))
	for code := range v.unreachable {
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}

// survey reads the items and asks every server that answers which LoRAs it
// holds. A server that is offline or does not answer is noted, not an error.
func (s *LoRAService) survey(ctx context.Context) (*loraSurvey, error) {
	r := s.Store.Repos()
	uses, err := r.Items().LoRAUsage(ctx)
	if err != nil {
		return nil, err
	}
	servers, err := r.Servers().All(ctx)
	if err != nil {
		return nil, err
	}
	v := &loraSurvey{
		servers: servers, installed: map[string]map[string]string{}, unreachable: map[string]error{},
		usage: map[string][]LoRAItem{},
	}
	for _, u := range uses {
		for _, l := range u.LoRAs {
			name := loraFile(l.Name)
			v.usage[name] = append(v.usage[name], LoRAItem{
				ID: u.ItemID, GroupID: u.GroupID, GroupCode: u.GroupCode, Code: u.ItemCode, Name: u.ItemName,
				StrengthModel: l.StrengthModel, StrengthClip: l.StrengthClip,
			})
		}
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, srv := range servers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			names, err := s.listOn(ctx, srv)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				v.unreachable[srv.CodeName] = err
				return
			}
			list := make(map[string]string, len(names))
			for _, n := range names {
				list[loraFile(n)] = n
			}
			v.installed[srv.CodeName] = list
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, errors.NewErr("ServerUnreachable", err).SetMetadata("operation", "list LoRAs")
	}
	return v, nil
}

// listOn asks one server for its LoRAs. A server that the monitor sees offline
// is not asked.
func (s *LoRAService) listOn(ctx context.Context, srv domain.Server) ([]string, error) {
	if s.Fleet == nil || !s.Fleet.Available(srv.ID) {
		return nil, errors.New("ServerUnreachable", "the server is offline", "server", srv.CodeName)
	}
	ctx, cancel := context.WithTimeout(ctx, loraListTimeout)
	defer cancel()
	client, err := s.Fleet.Client(ctx, srv)
	if err != nil {
		return nil, err
	}
	return client.ListModels(ctx, loraFolder)
}

// List returns one page of the LoRAs that items carry or servers hold, by name,
// whose name contains q (not case sensitive; empty means all).
func (s *LoRAService) List(ctx context.Context, q string, p domain.Page) (LoRAList, error) {
	v, err := s.survey(ctx)
	if err != nil {
		return LoRAList{}, err
	}
	var all []LoRAEntry
	for _, name := range matching(v.names(), q) {
		all = append(all, LoRAEntry{Name: name, Items: len(v.usage[name]), Servers: v.installedOn(name)})
	}
	p = p.Normalised()
	from := min(p.Offset(), len(all))
	to := min(from+p.Size, len(all))
	return LoRAList{Paged: domain.NewPaged(all[from:to], len(all), p), Unreachable: v.unreachableCodes()}, nil
}

// Suggest returns the names for the autocomplete of the item editor: those that
// contain q, the ones that start with it first, at most loraSuggestLimit.
func (s *LoRAService) Suggest(ctx context.Context, q string) ([]string, error) {
	v, err := s.survey(ctx)
	if err != nil {
		return nil, err
	}
	found := matching(v.names(), q)
	prefix := strings.ToLower(strings.TrimSpace(q))
	sort.SliceStable(found, func(i, j int) bool {
		return strings.HasPrefix(strings.ToLower(found[i]), prefix) && !strings.HasPrefix(strings.ToLower(found[j]), prefix)
	})
	if len(found) > loraSuggestLimit {
		found = found[:loraSuggestLimit]
	}
	return found, nil
}

func matching(names []string, q string) []string {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return names
	}
	var out []string
	for _, n := range names {
		if strings.Contains(strings.ToLower(n), q) {
			out = append(out, n)
		}
	}
	return out
}

func checkLoRAName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("InvalidRequest", "a LoRA name is needed", "field", "name")
	}
	return loraFile(name), nil
}

// Get says where a LoRA is installed and which items carry it. It fails with
// LoRANotFound when no item carries it and no server that answered has it.
func (s *LoRAService) Get(ctx context.Context, name string) (*LoRADetail, error) {
	name, err := checkLoRAName(name)
	if err != nil {
		return nil, err
	}
	v, err := s.survey(ctx)
	if err != nil {
		return nil, err
	}
	d := &LoRADetail{Name: name, Items: v.usage[name], Servers: []LoRAServer{}}
	if d.Items == nil {
		d.Items = []LoRAItem{}
	}
	found := len(d.Items) > 0
	for _, srv := range v.servers {
		list, reachable := v.installed[srv.CodeName]
		_, installed := list[name]
		found = found || installed
		d.Servers = append(d.Servers, LoRAServer{Code: srv.CodeName, Name: srv.Name, Reachable: reachable, Installed: installed})
	}
	if !found {
		return nil, errors.New("LoRANotFound", "no item carries this LoRA and no server has it", "lora", name, "unreachable", v.unreachableCodes())
	}
	sort.Slice(d.Servers, func(i, j int) bool { return d.Servers[i].Code < d.Servers[j].Code })
	return d, nil
}

// Delete removes a LoRA everywhere: its file from every ComfyUI server that has
// it, its files from the Civitai cache, and every item that carries it (with
// the files of those items). A server that cannot be reached or refuses stops
// the deletion before anything else is touched: LoRADeleteFailed names the
// servers, and doing it again after fixing them finishes the job, because a
// file that is already gone is not an error. It fails with LoRANotFound when
// there is nothing to delete.
func (s *LoRAService) Delete(ctx context.Context, name string) (*LoRADeletion, error) {
	name, err := checkLoRAName(name)
	if err != nil {
		return nil, err
	}
	v, err := s.survey(ctx)
	if err != nil {
		return nil, err
	}
	if failed := v.unreachableCodes(); len(failed) > 0 {
		for _, code := range failed {
			logError(s.Logger, "a server could not be asked about its LoRAs", v.unreachable[code], "lora", name, "server", code)
		}
		return nil, errors.NewErrf("LoRADeleteFailed", "these servers could not be asked, so the LoRA was not deleted: %s", strings.Join(failed, ", ")).SetMetadata("lora", name, "servers", failed)
	}
	holders := v.installedOn(name)
	items := v.usage[name]
	if len(holders) == 0 && len(items) == 0 {
		return nil, errors.New("LoRANotFound", "no item carries this LoRA and no server has it", "lora", name)
	}

	out := &LoRADeletion{Name: name, Servers: []string{}}
	var failed []string
	for _, srv := range v.servers {
		listed, ok := v.installed[srv.CodeName][name]
		if !ok {
			continue
		}
		if err := s.deleteOn(ctx, srv, listed); err != nil {
			logError(s.Logger, "a LoRA could not be deleted from a server", err, "lora", name, "server", srv.CodeName)
			failed = append(failed, srv.CodeName)
			continue
		}
		out.Servers = append(out.Servers, srv.CodeName)
	}
	if len(failed) > 0 {
		return nil, errors.NewErrf("LoRADeleteFailed", "the LoRA could not be deleted from these servers, so nothing else was deleted: %s", strings.Join(failed, ", ")).SetMetadata("lora", name, "servers", failed, "deleted_from", out.Servers)
	}
	sort.Strings(out.Servers)

	var stale []string
	ids := make([]domain.ID, 0, len(items))
	for _, it := range items {
		if !slices.Contains(ids, it.ID) {
			ids = append(ids, it.ID)
		}
	}
	err = s.Store.InTx(ctx, func(r Repos) error {
		for _, id := range ids {
			files, err := r.Items().Delete(ctx, id)
			if err != nil {
				return err
			}
			stale = append(stale, files...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out.Items = len(ids)
	s.removeFiles(stale)

	out.Files = s.removeCached(name)
	return out, nil
}

// removeCached removes the files the Civitai cache holds for a LoRA and says
// how many there were. A file that cannot be removed is logged, like the files
// of a deleted item (SPEC §17.4).
func (s *LoRAService) removeCached(name string) int {
	n := 0
	for _, f := range civitai.CacheFiles(s.Dirs.CivitaiCache, name) {
		r, err := s.Files.Open(f)
		if err != nil {
			continue // not there, or not ours to read
		}
		if err := r.Close(); err != nil {
			logError(s.Logger, "could not close a cached file", err, "path", f)
		}
		s.removeFiles([]string{f})
		n++
	}
	return n
}

func (s *LoRAService) deleteOn(ctx context.Context, srv domain.Server, listed string) error {
	client, err := s.Fleet.Client(ctx, srv)
	if err != nil {
		return err
	}
	return client.DeleteModel(ctx, loraFolder, strings.ReplaceAll(listed, `\`, "/"))
}
