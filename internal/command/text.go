package command

import "strings"

// joinPrompts joins the non-empty prompts with single spaces (SPEC §6.4).
func joinPrompts(prompts ...string) string {
	var parts []string
	for _, p := range prompts {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " ")
}

const placeholderOpen = "<tmpl:"

// removePlaceholders deletes the `<tmpl:name>` placeholders that no template
// replaced (SPEC §6.4). A placeholder between two spaces takes one of them
// with it, so no double space is left behind.
func removePlaceholders(s string) string {
	for {
		start := strings.Index(s, placeholderOpen)
		if start < 0 {
			return s
		}
		end := strings.IndexByte(s[start:], '>')
		if end < 0 || !IsWord(s[start+len(placeholderOpen):start+end]) {
			// Not a placeholder: keep the text and look further on.
			rest := removePlaceholders(s[start+len(placeholderOpen):])
			return s[:start+len(placeholderOpen)] + rest
		}
		before, after := s[:start], s[start+end+1:]
		switch {
		case strings.HasSuffix(before, " ") && strings.HasPrefix(after, " "):
			after = after[1:]
		case before == "":
			after = strings.TrimLeft(after, " ")
		case after == "":
			before = strings.TrimRight(before, " ")
		}
		s = before + after
	}
}

// appendLoRAs concatenates LoRA lists into a new slice.
func appendLoRAs(lists ...[]LoRA) []LoRA {
	var out []LoRA
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

// loraKey is the file a LoRA name stands for: `style` and `style.safetensors`
// are one file, and so are two spellings that differ in the slashes.
func loraKey(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	if i := strings.LastIndex(name, "."); i < 0 || strings.Contains(name[i:], "/") {
		name += ".safetensors"
	}
	return name
}

// dedupeLoRAs keeps the first LoRA of each file (SPEC §6.5, step 4).
func dedupeLoRAs(in []LoRA) []LoRA {
	seen := make(map[string]bool, len(in))
	var out []LoRA
	for _, l := range in {
		if k := loraKey(l.Name); !seen[k] {
			seen[k] = true
			out = append(out, l)
		}
	}
	return out
}
