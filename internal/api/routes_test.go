package api

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
)

// TestEveryRoute walks the whole API once, in the order a user meets it, and
// asserts the documented success code of each route (SPEC §20, acceptance 26).
// It ends by checking that no registered route was left out.
func TestEveryRoute(t *testing.T) {
	a, server := withCivitai(t)
	ok := func(r reply, status int) reply { t.Helper(); return a.expect(r, status, "") }

	ok(a.do("GET", "/healthz", nil), 200)

	t.Run("servers", func(t *testing.T) {
		r := ok(a.do("POST", "/api/v1/servers", map[string]any{"name": "GPU 1", "host": "http://gpu1:8188", "code_name": "gpu1", "max_concurrent_jobs": 2}), 201)
		if r.Header.Get("Location") != "/api/v1/servers/1" || r.json()["online"] != true {
			t.Errorf("created = %v, Location = %q", r.json(), r.Header.Get("Location"))
		}
		two := ok(a.do("POST", "/api/v1/servers", map[string]any{"name": "GPU 2", "host": "http://gpu2:8188", "code_name": "gpu2"}), 201).json()
		if _, there := two["comfyui_path"]; there {
			t.Errorf("a server still has a ComfyUI folder: %v", two)
		}
		list := ok(a.do("GET", "/api/v1/servers", nil), 200).json()
		if list["total"] != float64(2) || list["page"] != float64(1) || list["page_size"] != float64(20) || list["total_pages"] != float64(1) {
			t.Errorf("list = %v", list)
		}
		ok(a.do("GET", "/api/v1/servers/1", nil), 200)
		put := ok(a.do("PUT", "/api/v1/servers/1", map[string]any{"name": "Renamed", "host": "http://gpu1:8188", "code_name": "gpu1", "max_concurrent_jobs": 1}), 200).json()
		if put["name"] != "Renamed" {
			t.Errorf("put = %v", put)
		}
		loras := ok(a.do("GET", "/api/v1/servers/1/loras", nil), 200).json()["loras"].([]any)
		if len(loras) != 2 {
			t.Errorf("loras = %v", loras)
		}
		ok(a.do("DELETE", "/api/v1/servers/2", nil), 204)
	})

	t.Run("generators and fixers", func(t *testing.T) {
		wf := example(t, "creators", "illustrious_xl.json")
		an := ok(a.do("POST", "/api/v1/generators/analyze", map[string]any{"workflow": wf}), 200).json()
		if an["output_type"] != "image" || an["output_node_class_type"] == "" {
			t.Errorf("analysis = %v", an)
		}
		if an["positive_prompt_title"] != "Positive" || an["negative_prompt_title"] != "Negative" {
			t.Errorf("the analysis suggests %v / %v", an["positive_prompt_title"], an["negative_prompt_title"])
		}
		r := ok(a.do("POST", "/api/v1/generators", map[string]any{"name": "Anime", "code_name": "anime", "workflow": wf}), 201)
		if r.json()["positive_prompt_title"] != "Positive" || r.json()["negative_prompt_title"] != "Negative" {
			t.Errorf("a generator created without titles got %v / %v", r.json()["positive_prompt_title"], r.json()["negative_prompt_title"])
		}
		wfBack, isObject := r.json()["workflow"].(map[string]any)
		if !isObject || len(wfBack) == 0 {
			t.Errorf("the workflow must come back as a JSON object, got %T", r.json()["workflow"])
		}
		ok(a.do("GET", "/api/v1/generators", nil), 200)
		ok(a.do("GET", "/api/v1/generators/1", nil), 200)
		ok(a.do("PUT", "/api/v1/generators/1", map[string]any{"name": "Anime 2", "code_name": "anime", "workflow": wf}), 200)
		ok(a.do("POST", "/api/v1/generators", map[string]any{"name": "Tmp", "code_name": "tmp", "workflow": wf}), 201)
		ok(a.do("DELETE", "/api/v1/generators/2", nil), 204)

		fx := map[string]any{
			"name": "Upscale", "code_name": "upscale", "workflow": example(t, "fixers", "simple_upscale.json"),
			"load_image_title": "Load Image", "save_image_title": "Save Image",
		}
		ok(a.do("POST", "/api/v1/fixers", fx), 201)
		ok(a.do("GET", "/api/v1/fixers", nil), 200)
		ok(a.do("GET", "/api/v1/fixers/1", nil), 200)
		ok(a.do("PUT", "/api/v1/fixers/1", fx), 200)
		fx["code_name"] = "tmpfix"
		ok(a.do("POST", "/api/v1/fixers", fx), 201)
		ok(a.do("DELETE", "/api/v1/fixers/2", nil), 204)
	})

	t.Run("groups, items and tags", func(t *testing.T) {
		ok(a.do("POST", "/api/v1/groups", map[string]any{"name": "Characters", "code_name": "characters", "tags": []string{"People"}}), 201)
		withThumb := ok(a.form("POST", "/api/v1/groups", map[string]any{"name": "Poses", "code_name": "poses"},
			part{"thumbnail", "pose.png", "image/png", pngBytes}), 201).json()
		if thumb, _ := withThumb["thumbnail"].(string); !strings.HasPrefix(thumb, a.dirs.Thumbnails) {
			t.Errorf("thumbnail = %v", withThumb["thumbnail"])
		}
		ok(a.do("POST", "/api/v1/groups/import-text", map[string]any{"name": "Places", "code_name": "places", "text": "a beach\na forest"}), 201)
		list := ok(a.do("GET", "/api/v1/groups?tag=people", nil), 200).json()
		if list["total"] != float64(1) {
			t.Errorf("groups tagged people = %v", list["total"])
		}
		ok(a.do("GET", "/api/v1/groups/1", nil), 200)
		ok(a.do("PUT", "/api/v1/groups/1", map[string]any{"name": "Characters", "code_name": "characters", "tags": []string{"people", "main"}}), 200)
		ok(a.do("DELETE", "/api/v1/groups/3", nil), 204)

		for _, it := range []map[string]any{
			{"code_name": "a", "name": "Alice", "positive_prompt": "alice", "tags": []string{"combat"}},
			{"code_name": "b", "name": "Bob", "positive_prompt": "bob", "tags": []string{"calm"}},
		} {
			ok(a.do("POST", "/api/v1/groups/1/items", it), 201)
		}
		item := ok(a.form("POST", "/api/v1/groups/2/items", map[string]any{
			"code_name": "p1", "name": "Pose 1", "positive_prompt": "standing",
			"controlnets": []map[string]any{{"type": "openpose", "strength": 1}},
		}, part{"thumbnail", "t.png", "image/png", pngBytes}, part{"controlnet_0", "pose.png", "image/png", pngBytes}), 201).json()
		if cn := item["controlnets"].([]any)[0].(map[string]any); !strings.HasPrefix(cn["image_path"].(string), a.dirs.ControlNet) {
			t.Errorf("controlnet = %v", cn)
		}
		ok(a.do("POST", "/api/v1/groups/2/items", map[string]any{"code_name": "p2", "name": "Pose 2", "positive_prompt": "sitting"}), 201)
		ok(a.do("GET", "/api/v1/groups/1/items", nil), 200)
		ok(a.do("GET", "/api/v1/items/3", nil), 200)
		ok(a.do("PUT", "/api/v1/items/3", map[string]any{"code_name": "a", "name": "Alice", "positive_prompt": "alice, smiling", "tags": []string{"combat"}}), 200)
		ok(a.do("DELETE", "/api/v1/items/6", nil), 204)

		tags := ok(a.do("GET", "/api/v1/tags?scope=item&prefix=c", nil), 200).json()["tags"].([]any)
		if len(tags) != 2 { // calm, combat
			t.Errorf("tags = %v", tags)
		}
		renamed := ok(a.do("POST", "/api/v1/tags/rename", map[string]any{"scope": "item", "from": "calm", "to": "quiet"}), 200).json()
		if renamed["renamed"] != float64(1) {
			t.Errorf("renamed = %v", renamed)
		}

		civ := ok(a.do("POST", "/api/v1/groups/import-civitai", map[string]any{
			"name": "Foxes", "code_name": "foxes", "server": server, "models": []map[string]any{{"model_id": 7}},
		}), 202).json()
		a.api.Services.Civitai.Wait()
		ok(a.do("POST", "/api/v1/groups/"+jsonNumber(civ["id"].(float64))+"/items/civitai", map[string]any{"model_id": 8, "server": server}), 202)
		a.api.Services.Civitai.Wait()
		ok(a.do("DELETE", "/api/v1/groups/"+jsonNumber(civ["id"].(float64)), nil), 204)
	})

	t.Run("projects and commands", func(t *testing.T) {
		ok(a.do("POST", "/api/v1/projects", map[string]any{"name": "Demo"}), 201)
		ok(a.do("GET", "/api/v1/projects", nil), 200)
		ok(a.do("GET", "/api/v1/projects/1", nil), 200)
		ok(a.do("PUT", "/api/v1/projects/1", map[string]any{"name": "Demo 2"}), 200)

		pv := ok(a.do("POST", "/api/v1/commands/preview", map[string]any{"code": "gpu1 -$ anime: characters * poses"}), 200).json()
		if pv["valid"] != true || pv["jobs"] != float64(2) {
			t.Errorf("preview = %v", pv)
		}
		cmd := ok(a.do("POST", "/api/v1/projects/1/commands", map[string]any{"name": "first", "code": "gpu1 -$ anime: characters * poses"}), 201)
		if cmd.Header.Get("Location") != "/api/v1/commands/1" {
			t.Errorf("Location = %q", cmd.Header.Get("Location"))
		}
		if _, isObject := cmd.json()["command_json"].(map[string]any); !isObject {
			t.Errorf("command_json must be a JSON object, got %T", cmd.json()["command_json"])
		}
		ok(a.do("POST", "/api/v1/projects/1/commands", map[string]any{"name": "second", "code": "gpu1 -$ anime: characters(a)"}), 201)
		list := ok(a.do("GET", "/api/v1/projects/1/commands", nil), 200).json()
		first := list["items"].([]any)[0].(map[string]any)
		if first["total_jobs"] != float64(2) || first["finished_jobs"] != float64(0) {
			t.Errorf("progress = %v / %v", first["finished_jobs"], first["total_jobs"])
		}
		ok(a.do("GET", "/api/v1/commands/1", nil), 200)
		ok(a.do("PUT", "/api/v1/commands/2", map[string]any{"name": "second", "code": "gpu1 -$ anime: characters(b)"}), 200)
		moved := ok(a.do("POST", "/api/v1/commands/2/move", map[string]any{"order": 1}), 200).json()
		if moved["order"] != float64(1) {
			t.Errorf("moved = %v", moved)
		}
		ok(a.do("POST", "/api/v1/projects/1/commands/renumber", nil), 200)

		jobs := ok(a.do("GET", "/api/v1/commands/1/jobs?status=idle", nil), 200).json()
		if jobs["total"] != float64(2) {
			t.Errorf("jobs = %v", jobs["total"])
		}
		ok(a.do("GET", "/api/v1/jobs/1", nil), 200)
		edited := ok(a.do("PUT", "/api/v1/jobs/1", map[string]any{"positive_prompt": "new", "negative_prompt": "bad"}), 200).json()
		if edited["positive_prompt"] != "new" {
			t.Errorf("edited = %v", edited)
		}

		run := ok(a.do("POST", "/api/v1/commands/1/run", nil), 202).json()
		if run["status"] != "queued" {
			t.Errorf("run = %v", run)
		}
		stop := ok(a.do("POST", "/api/v1/commands/1/stop", nil), 200).json()
		if stop["status"] != "idle" {
			t.Errorf("stop = %v", stop)
		}
		ok(a.do("POST", "/api/v1/jobs/1/run", nil), 202)
		ok(a.do("POST", "/api/v1/jobs/1/stop", nil), 200)
		ok(a.do("POST", "/api/v1/commands/1/recreate", nil), 202)

		// Download needs a finished command: finish its jobs by hand.
		for id := int64(1); id <= 8; id++ {
			j, err := a.db.Repos().Jobs.Get(t.Context(), id)
			if err != nil || j.CommandID != 1 {
				continue
			}
			check(t, os.MkdirAll(filepath.Dir(j.ResultPath), 0o750))
			check(t, os.WriteFile(j.ResultPath, pngBytes, 0o600))
			j.Status = domain.StatusFinished
			check(t, a.db.Repos().Jobs.Update(t.Context(), j))
		}
		check(t, a.db.Repos().Commands.SetStatus(t.Context(), 1, domain.StatusFinished))
		dl := ok(a.do("GET", "/api/v1/commands/1/download", nil), 200)
		if dl.Header.Get("Content-Type") != "application/zip" || !strings.Contains(dl.Header.Get("Content-Disposition"), "command-1.zip") {
			t.Errorf("download headers = %v", dl.Header)
		}
		zr, err := zip.NewReader(bytes.NewReader(dl.Body), int64(len(dl.Body)))
		check(t, err)
		if len(zr.File) != 3 { // two results and summary.json
			t.Errorf("archive has %d files", len(zr.File))
		}
		ok(a.do("DELETE", "/api/v1/commands/2", nil), 204)
		ok(a.do("POST", "/api/v1/projects", map[string]any{"name": "Temporary"}), 201)
		ok(a.do("DELETE", "/api/v1/projects/2", nil), 204)
	})

	t.Run("repl", func(t *testing.T) {
		a.expect(a.do("GET", "/api/v1/repl/last", nil), 404, "JobNotFound")
		run := ok(a.do("POST", "/api/v1/repl/run", map[string]any{"server": "gpu1", "generator": "anime", "positive": "a fox", "items": "characters(a)"}), 202).json()
		if run["project_id"] != float64(-1) || run["status"] != "queued" || run["positive_prompt"] != "alice, smiling a fox" {
			t.Errorf("repl job = %v", run)
		}
		last := ok(a.do("GET", "/api/v1/repl/last", nil), 200).json()
		if last["id"] != run["id"] {
			t.Errorf("last = %v", last["id"])
		}
	})

	t.Run("loras", func(t *testing.T) {
		list := ok(a.do("GET", "/api/v1/loras", nil), 200).json()
		if list["total"].(float64) < 2 {
			t.Errorf("the servers' LoRAs are listed: %v", list)
		}
		detail := ok(a.do("GET", "/api/v1/loras/detail?name=dance.safetensors", nil), 200).json()
		if detail["name"] != "dance.safetensors" {
			t.Errorf("detail = %v", detail)
		}
		del := ok(a.do("DELETE", "/api/v1/loras?name=fox.safetensors", nil), 200).json()
		if servers, _ := del["servers"].([]any); len(servers) == 0 {
			t.Errorf("deletion = %v", del)
		}
	})

	t.Run("events, files and the contract", func(t *testing.T) {
		a.events.Append(eventOf("processing"))
		ev := ok(a.do("GET", "/api/v1/events", nil), 200).json()
		list := ev["events"].([]any) // the imports above left their own events before this one
		if n := len(list); n == 0 || ev["last_id"] != float64(n) || ev["gap"] != false || list[n-1].(map[string]any)["type"] != "processing" {
			t.Errorf("events = %v", ev)
		}
		thumbs := storedFiles(t, a.dirs.Thumbnails)
		if len(thumbs) == 0 {
			t.Fatal("no thumbnail was stored")
		}
		img := ok(a.do("GET", "/api/v1/files/thumbnails/"+filepath.Base(thumbs[0]), nil), 200)
		if img.Header.Get("Content-Type") != "image/png" || !bytes.Equal(img.Body, pngBytes) || img.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("file headers = %v", img.Header)
		}
		spec := ok(a.do("GET", "/api/v1/openapi.json", nil), 200).json()
		if spec["openapi"] != "3.1.0" {
			t.Errorf("openapi = %v", spec["openapi"])
		}
	})

	// Acceptance 26: every registered route was exercised above.
	var missed []string
	for _, r := range a.api.Routes() {
		if !a.hit[r.pattern()] {
			missed = append(missed, r.pattern())
		}
	}
	sort.Strings(missed)
	if len(missed) > 0 {
		t.Errorf("routes without a round trip: %v", missed)
	}
}
