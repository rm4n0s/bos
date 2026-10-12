package api

import (
	"testing"
)

// The server of the seeded API is online and holds dance and fox; the item "a"
// carries dance (without its extension) and "mine".
func lorasSeeded(t *testing.T) *testAPI {
	t.Helper()
	a := seeded(t)
	a.expect(a.do("PUT", "/api/v1/items/1", map[string]any{
		"code_name": "a", "name": "Alice", "positive_prompt": "alice",
		"loras": []map[string]any{{"name": "dance", "strength_model": 0.8, "strength_clip": 0.7}, {"name": "mine.safetensors", "strength_model": 1, "strength_clip": 1}},
	}), 200, "")
	return a
}

func TestListLoRAs(t *testing.T) {
	a := lorasSeeded(t)
	got := a.expect(a.do("GET", "/api/v1/loras", nil), 200, "").json()
	items := got["items"].([]any)
	if got["total"] != float64(3) || len(items) != 3 {
		t.Fatalf("list = %v", got)
	}
	first := items[0].(map[string]any)
	if first["name"] != "dance.safetensors" || first["items"] != float64(1) || len(first["servers"].([]any)) != 1 {
		t.Errorf("first = %v", first)
	}
	if u, ok := got["unreachable"].([]any); !ok || len(u) != 0 {
		t.Errorf("unreachable = %v", got["unreachable"])
	}
	got = a.expect(a.do("GET", "/api/v1/loras?q=FOX", nil), 200, "").json()
	if got["total"] != float64(1) || got["items"].([]any)[0].(map[string]any)["name"] != "fox.safetensors" {
		t.Errorf("search = %v", got)
	}
	a.expect(a.do("GET", "/api/v1/loras?page=0", nil), 400, "InvalidRequest")
}

func TestGetLoRA(t *testing.T) {
	a := lorasSeeded(t)
	got := a.expect(a.do("GET", "/api/v1/loras/detail?name=dance", nil), 200, "").json()
	servers := got["servers"].([]any)
	items := got["items"].([]any)
	if got["name"] != "dance.safetensors" || len(servers) != 1 || servers[0].(map[string]any)["installed"] != true || servers[0].(map[string]any)["code_name"] != "gpu1" {
		t.Errorf("servers = %v in %v", servers, got)
	}
	if len(items) != 1 || items[0].(map[string]any)["strength_model"] != 0.8 || items[0].(map[string]any)["group_code"] != "characters" {
		t.Errorf("items = %v", items)
	}
	a.expect(a.do("GET", "/api/v1/loras/detail?name=nothing", nil), 404, "LoRANotFound")
	a.expect(a.do("GET", "/api/v1/loras/detail", nil), 400, "InvalidRequest")
}

func TestDeleteLoRA(t *testing.T) {
	a := lorasSeeded(t)
	a.expect(a.do("DELETE", "/api/v1/loras", nil), 400, "InvalidRequest")
	a.expect(a.do("DELETE", "/api/v1/loras?name=nothing", nil), 404, "LoRANotFound")

	got := a.expect(a.do("DELETE", "/api/v1/loras?name=mine.safetensors", nil), 200, "").json()
	if got["name"] != "mine.safetensors" || got["items"] != float64(1) || len(got["servers"].([]any)) != 0 || got["files"] != float64(0) {
		t.Errorf("deletion = %v", got)
	}
	// The item that carried it is gone.
	a.expect(a.do("GET", "/api/v1/items/1", nil), 404, "ItemNotFound")
}
