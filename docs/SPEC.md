# Bowl of Scenes — Product Specification

**Part I (§1–13)** describes *what* the system does and is technology-neutral: operations are named abstractly. **Part II (§13–20)** binds it to the chosen technology: a **Go** server with a **REST API** and a **Datastar** web UI, using the libraries `rm4n0s/errors`, `rm4n0s/comfyui-client-go` and `rm4n0s/gocivitailib`. Where the two parts differ in detail, Part I defines the behaviour and Part II the delivery.

The two external systems the product integrates with are part of the requirements and are named explicitly:

- **ComfyUI** — a node-graph image/video/3D generation server that accepts a workflow in its "API format" (a map of node-id → `{class_type, inputs, _meta.title}`), runs it, and returns outputs.
- **Civitai** — a public model catalogue, used only to download LoRA models and their metadata.

---

# Part I — Product behaviour

## 1. Purpose

Bowl of Scenes lets a single user **generate images (or video / 3D assets) in bulk** by combining reusable *attributes* — characters, poses, emotions, styles, LoRAs, ControlNet references — and submitting every combination as a job to one or more ComfyUI servers.

The user does not write each prompt by hand. They:

1. Register ComfyUI servers and workflows.
2. Organise reusable prompt fragments (*items*) into *groups*.
3. Write a one-line **command** in a small language that selects groups and items, e.g.

   ```
   local_comfyui -$ workflow_for_anime: characters * poses(~jumping) * emotions(cry) > upscale
   ```

4. The system expands the command into the Cartesian product of the selected items, creates one **job** per combination, runs them sequentially through ComfyUI, and optionally post-processes each result with **fixer** workflows.

### Non-goals

- Not a workflow editor; workflows are supplied as ready-made ComfyUI API-format JSON.
- No multi-user support, authentication or authorisation.
- No cancellation of a job that ComfyUI is already executing.

## 2. Settings

The program is started with the path of one **database file** (`-db`, default `data/bos.db`) and, optionally, a log level (`-log-level`). Everything else is a *setting*, and the settings live **in that database**, in a table of names and text values. They are edited in the web interface (§16.3); there is no configuration file.

| Setting | Default | Meaning |
|---|---|---|
| `results_dir` | `results` next to the database | Generated outputs |
| `controlnet_dir` | `controlnet` next to the database | Uploaded ControlNet images |
| `ipadapter_dir` | `ipadapter` next to the database | Uploaded IP-Adapter images |
| `regions_dir` | `regions` next to the database | Region reference images and their generated masks |
| `thumbnails_dir` | `thumbnails` next to the database | Group and item thumbnails |
| `downloads_dir` | `downloads` next to the database | Per-command export archives |
| `civitai_cache_dir` | `civitai` next to the database | Local cache for downloaded LoRAs |
| `civitai_token` | none | Civitai API token; required for any Civitai download. A credential: never shown again |
| `civitai_domain` | `civitai.com` | The site API is `https://<domain>`, the generation API `https://orchestration.<domain>` (§19) |
| `civitai_pause_seconds` | 15 | Seconds to wait between two downloads of one import (§19.4) |
| `listen_addr` | `127.0.0.1:8080` | (§16.6) |
| `auth_token` | none | Shared bearer token for the API and UI (§15.1, §16.6). A credential: never shown again |
| `cors_allow_list` | none | Origins allowed to call the API cross-origin, separated by commas |
| `upload_limit_bytes` | 64 MiB | Maximum size of one uploaded file (§15.1) |
| `grace_seconds` | 30 | Seconds a server may be unavailable before unpinned jobs are re-assigned elsewhere (§8.7) |
| `max_job_attempts` | 3 | How many times a job is retried after its server became unreachable (§8.7) |
| `delete_comfy_outputs` | `true` | Remove a result from the `output` folder of its ComfyUI once this program has saved it (§8.2), through the comfyui-model-crud extension (§18.8). `true` or `false`; an empty value is the default, on |
| `preview_soft_limit` | 0 (none) | Job count above which the preview warns (§6.11) |
| `log_level` | `info` | `debug`, `info`, `warn` or `error` |
| `event_log_size` | 100 (at least 100) | Number of events retained (§8.5, §15.4) |

**Storage.** One row for each setting that is set (name, text value). A setting that is not stored has its default, and storing an empty value removes the row. The Go struct of the settings (`internal/config`) is the one list of names, defaults and rules; unknown stored names are ignored.

**A new database.** If the database file does not exist, it is created together with its folder and schema (§10), and the folder settings above are **stored** with their defaults: folders next to the database file, as absolute paths. A database that has no folder settings yet (made before the settings were stored in it) gets them on its next start. A folder setting that is empty or removed means the default folder next to the database, never "none".

**The environment.** A variable `BOS_<SETTING>` (the name in capitals) overrides that setting for the run, and is not stored. The settings page says which settings are overridden. The database path is never a setting.

**Changing settings.** The service checks the settings as they would be (every problem together, `ConfigInvalid`, 422) and stores nothing if any is wrong. A credential given empty is left as it is; removing one is asked for by name. A change takes effect when the program restarts: the settings page offers a restart that stops the program as a termination signal does (acceptance 28) and starts it again in the same process, which reads the database anew.

### Startup behaviour

1. Open the database file, creating it, its folder and its schema (§10) if they do not exist; store the default folder settings that are missing; resolve the settings (stored, then the environment, then defaults) and create the folders.
2. Start the background workers described in §8.

## 3. Domain model

Every entity carries creation and last-update timestamps. Identifiers are unique integers assigned by the system. Relationships are by identifier; there are no automatic cascades — deleting a parent must explicitly delete dependants as stated below.

### 3.1 Server
| Field | Notes |
|---|---|
| name | ≤100 chars |
| host | ComfyUI base address, ≤100 chars |
| code_name | unique, ≤100 chars; referenced from commands |
| is_local | boolean — true when ComfyUI can read the application's files directly (shared filesystem); otherwise input files are uploaded to it (§8.7) |
| max_concurrent_jobs | integer ≥ 1, default 1 — jobs this server may run at once |

Derived: **status** `online` / `offline`, reflecting current reachability.

**Every server runs the extension [comfyui-model-crud](https://github.com/rm4n0s/comfyui-model-crud)** (§18.8). The program reaches the `output` and `loras` folders of a server through its HTTP routes and never through the file system, so a server has no folder field, and ComfyUI may run on another machine. (Earlier versions kept an optional `comfyui_path`; migration 4 drops it.)

### 3.2 Generator
A ComfyUI workflow that produces the primary output.

| Field | Notes |
|---|---|
| name, code_name (unique) | |
| workflow | ComfyUI API-format workflow |
| positive_prompt_title, negative_prompt_title | `_meta.title` of the text-encode nodes that receive the prompts. The **positive** one is **required**: when it is left empty, the first node whose title starts with `positive` (any case, a text encoder before any other node) is used, and when there is none the generator is refused with `GeneratorTitleRequired`. The **negative** one is optional (a workflow may have no negative prompt, or its positive node may take both, §8.2): when it is left empty, the first node whose title starts with `negative` is used, if there is one. When no title starts with `positive`, a text-encode node with a `negative_prompt` input is suggested as the positive one |
| output_type | `image`, `video`, `3d`, `unknown` |
| output_attributes | per output type, optional (below) |
| output_node_class_type, output_node_title | the node that produces the result |
| has_random_seed | when true, a fresh random seed is written to every sampler for each job |

Output attributes:

- **Image:** width, height, file_type, batch_size, steps, cfg, sampler_name, scheduler, denoise, seed (all optional).
- **Video:** width, height, file_type, fps, num_frames, duration_seconds.
- **3D:** file_type.

**Output analysis.** Given a workflow, the system infers output type, default attributes and output node. Recognised output nodes: `SaveImage`, `PreviewImage` (png), `SaveAnimatedWEBP` (webp), `SaveAnimatedPNG` (apng), `Image Save`, `SaveImageExtended`, `JWImageSave`, `VHS_VideoCombine` (video), `SaveGLB` (glb), `Save3DModel`. Width/height are taken from the first present node in priority order: `EmptyLatentImage`, `EmptySD3LatentImage`, `EmptyLatentImageSD3`, the Mochi/Hunyuan/LTXV/Wan/Cog/Flux video-latent nodes, then `ImageScale`, `ImageResize`, `LatentUpscale`. If no output node is found the analysis fails with a clear error.

### 3.3 Fixer
A ComfyUI workflow that takes an existing image and returns an improved one (upscale, face detail, background removal, img2img refinement…).

Fields: name, code_name (unique), workflow, `load_image_title` (title of the image-input node to feed), `save_image_title`, and optional positive/negative prompt text and prompt node titles.

The project should ship example generator and fixer workflows (creators for several model families; fixers for adetailer, auto-detection inpainting, ControlNet-tile upscale, background removal, img2img refinement, simple upscale, tiled upscale).

### 3.4 Tags

There is no category entity. Classification is done with **tags**: short labels attached to groups and to items. Tags are not entities of their own; the set of tags in use is derived from the groups and items that carry them.

| Rule | |
|---|---|
| Form | A tag is a lower-case word: letters, digits, underscore (`[a-z0-9_]+`), not a reserved word (§6.1), at most 50 characters. Input with upper-case letters is lower-cased on save; anything else is rejected with `TagInvalid` |
| Multiplicity | A group or item has any number of tags, without duplicates |
| Two vocabularies | **Group tags** organise the user interface (filter and find groups: `character`, `anime`, `wip`). **Item tags** are visible to the command language (`#tag` selectors and `where`, §6.4, §6.7). The two are separate fields; a command never sees group tags, so one tag name cannot mean two things inside a command |
| Matching | `#tag` in a command is compared case-insensitively against item tags |
| Suggestions | The system offers the tags already in use (separately for groups and for items) as autocomplete, so spellings do not drift. A starter list of group tag suggestions is `character, poses, style, clothe, body, emotion, quality, background, camera`; none is required |
| Rename / merge | Renaming a tag changes it on every group (or item) that carries it; renaming to a tag that already exists merges the two. Renaming an **item** tag does not rewrite command texts; the operation returns the commands whose text mentions the old tag, because they will fail validation on their next edit or re-creation |

### 3.5 Group
A named collection of items that share a role.

Fields: name (≤300), description, **code_name (globally unique, ≤100)**, `tags` (group tags, §3.4), thumbnail (optional), and feature flags that decide which sections an item editor offers: `use_controlnet`, `use_ip_adapter`, `use_mask_region`, `use_coordinates_region`, `use_type_attributes`. There is no flag for LoRAs: every item can carry them (§3.6, §9.1). A group made before this change keeps an unused `use_lora` column in the database; it is neither read nor shown, and a request that still sends `use_lora` is refused as an unknown field.

### 3.6 Item
One selectable value inside a group.

| Field | Notes |
|---|---|
| group, code_name | code_name unique **within its group** |
| name | ≤300 |
| positive_prompt, negative_prompt | text; may contain `<tmpl:name>` placeholders (§6.5) |
| loras | list of `{name, strength_model, strength_clip}`; any item may carry them. The name is the file as ComfyUI knows it (`styles/fox.safetensors`; a name without an extension means `.safetensors`). The item dialog shows them as a table (name, the two strengths, a *Remove* button) with a search box that completes names from the LoRAs of all items and all servers (§9.1) |
| ip_adapter | `{image_file, weight, weight_type, start_at, end_at, clip_vision_model, model_name}` |
| controlnets | list of `{type, image_path, is_reference, model_pattern, strength}`; types: openpose, dwpose, tile, canny, midas, depth, lineart, softedge, scribble, normal |
| mask_region_images | `{reference_path, folder_path, mask_files: {keyword → mask image}}`, generated (§7.3) |
| coordinated_regions | list of `{keyword, width, height, x, y}` |
| output_type, output_type_attributes | optional per-item override of the generator's output settings |
| tags | item tags (§3.4), used by `#tag` selectors (§6.4) |
| thumbnail | optional |

Bulk creation:

- **From a text file:** one item per line; code_name and name are the line index, positive_prompt is the line, negative_prompt contains one `<tmpl:tag>` marker per tag found in the line.
- **From Civitai:** see §9.

### 3.7 Project
Name. A container of ordered commands.

### 3.8 Command
Fields: project, `order` (1-based, contiguous within the project), status, name, description, `command_code` (command-language text), `command_json` (parsed form, §6.10), `download_file` (last export path, optional).

### 3.9 Job
One concrete generation. A job is a **snapshot**: it copies everything it needs, so later edits to groups or items do not affect existing jobs.

Fields: project, command, list of `{group, item}` used, original command text, status, `server` (the server that runs / ran the generation: set when the job starts under `dynamic` spread; set at creation, as a *preferred* server, under static spread; null otherwise) with its host, `pinned` flag, per-fixer-step server (explicit `on` server or null = same as generation), `repeat_index`, effective `seed`, `attempts` (retries after server loss), generator code_name / output type / output attributes, ComfyUI prompt id, positive and negative prompt, controlnets, region prompts, ip_adapters, loras, result file path, error message, error trace, `total_fixers` (ordered list of fixer ids), `finished_fixers`, `is_generated`, `is_fixed`.

A single reserved **REPL job** (project = −1, command = −1) supports ad-hoc runs (§8.6).

### 3.10 Enumerations

- **Status** (commands and jobs): `idle`, `queued`, `processing`, `error`, `finished`.
- **Notification type:** `finished`, `error`, `processing`.
- **Output type:** `image`, `video`, `3d`, `unknown`.

## 4. User-facing capabilities

The product exposes the following, however delivered. Every list operation supports pagination (page, page size default 20 → items, total, page, page size, total pages).

| Area | Operations |
|---|---|
| Servers | create, edit, delete, list (with live status) |
| Generators | create, edit, delete, list; upload a workflow and auto-detect output type/node/attributes |
| Fixers | create, edit, delete, list |
| Tags | list the tags in use (groups or items, with optional prefix, for autocomplete); rename / merge a tag (§3.4) |
| Groups | create, edit (including tags), delete (removes its items and their files), list with filter by tag, get; import from a text file; import LoRAs from Civitai |
| Items | create, edit, delete (removes its files), list by group; thumbnails and reference-image uploads |
| Projects | create, edit, delete, list, get |
| Commands | add (append or insert at position), edit, delete, reorder (move / up / down / renumber), list by project with progress `finished_jobs / total_jobs`, **preview** (dry run: validate and report counts, distribution and warnings without creating anything), run, stop, recreate jobs, download export |
| Jobs | list by command, get, run, stop, edit prompts; view result, error and trace |
| REPL | run one ad-hoc job; retrieve the previous ad-hoc job |
| Notifications | retrieve notifications that occurred after a given cursor (§8.5) |

Item editors only show sections enabled by the group's feature flags. The client should surface processing / finished / error notifications to the user as they occur.

## 5. Authoring workflow (informative)

1. Add a server and verify it shows `online`.
2. Add a generator (and optionally fixers) by supplying workflows and the node titles that receive prompts / images.
3. Create groups and items.
4. Create a project; **preview** a command to see how many jobs it makes and how they are distributed; add it; review the generated jobs; run the command.
5. Monitor job statuses; edit or re-run individual jobs; download the result archive.

## 6. Command language

A command is one line of text. It names the ComfyUI server(s) and the generator, selects items from groups, optionally shapes and limits the resulting job list, optionally overrides generation settings, says how work is split across servers, and lists fixers.

```
gpu1, gpu2 -$ anime: characters * poses(~jumping) * emotions(cry) * "masterpiece"
    where not #acrobatic
    sample 20 seed 7  repeat 3
    with steps=30, cfg=6.5
    spread blocks
    > upscale on gpu2
```

(Line breaks are insignificant; the example is wrapped for reading.)

Clauses appear in the fixed order **where → limit/sample → repeat → with → spread → fixers**, which matches the order in which they are evaluated (§6.7), except `with`, which only sets values.

### 6.1 Lexical rules

The input is first **normalised**: leading and trailing whitespace is removed and, outside string literals, every run of whitespace (including line breaks) becomes one space. It is then split into tokens; whitespace separates tokens but is otherwise insignificant.

| Token | Spelling |
|---|---|
| `WORD` | one or more of `A–Z a–z 0–9 _` |
| `PATTERN` | a `WORD` that also contains at least one wildcard: `%` (any run of characters, possibly empty) or `?` (exactly one character) |
| `DECIMAL` | digits, `.`, digits (e.g. `6.5`) |
| `STRING` | `"` … `"`; any characters except an unescaped `"` or a line break; `\"` and `\\` are escapes; content is kept verbatim (no whitespace normalisation) |
| `ARROW` `COLON` `PRODUCT` `MERGE` `ZIP` `FIX` | `-$` `:` `*` `&&` `\|\|` `>` |
| `LPAR` `RPAR` `LBRK` `RBRK` `LBRC` `RBRC` | `(` `)` `[` `]` `{` `}` |
| `COMMA` `NOT` `BANG` `HASH` `EQ` | `,` `~` `!` `#` `=` |

**Keywords.** `where repeat limit sample with spread on and or not in` are reserved words (lower case). `seed dynamic even blocks by pinned` are *contextual*: they have special meaning only at the place the grammar expects them, and are otherwise ordinary words.

Every code name that can appear in a command (server, generator, fixer, group, item), every template tag, every region keyword and every item tag MUST be a `WORD` and MUST NOT be a reserved word. The system MUST reject such a name when the entity is created, so that it can always be referenced. (The original enforced this only for servers and generators; item code names such as Civitai model ids already comply.)

Any other character outside a string literal is a lexical error. Wildcards `%` and `?` exist only inside `PATTERN` tokens, so `*` is always the product operator and `a*b` is unambiguous.

### 6.2 Grammar

ISO-style EBNF: `,` concatenation, `|` alternation, `[ ]` optional, `{ }` zero or more, `( )` grouping, quoted lower-case words are keyword tokens. Terminals are the tokens of §6.1.

```ebnf
command     = placement , ARROW , generator , COLON , selections ,
              [ where ] , [ pick ] , [ repeat ] , [ overrides ] , [ spread ] ,
              { FIX , step } ;

(* ---- where the work runs ---- *)
placement   = target , { COMMA , target } ;
target      = server , [ EQ , count ] ;                      (* weight, see §6.9 *)
step        = fixer , [ "on" , server ] ;

(* ---- what is generated ---- *)
selections  = term , { PRODUCT , term } ;                    (* top level: regions allowed *)
term        = region | combo ;

combo       = operand
            | operand , MERGE , operand , { MERGE , operand }
            | operand , ZIP   , operand , { ZIP   , operand } ;
operand     = template | plain | literal ;

plain       = group , [ filter ] ;
template    = group , [ filter ] , LBRK , bind , { COMMA , bind } , RBRK ;
region      = group , LBRC , rbind , { COMMA , rbind } , RBRC ;
literal     = [ BANG ] , STRING ;                            (* "text" positive, !"text" negative *)

bind        = tag     , COLON , body ;
rbind       = keyword , COLON , body ;
body        = combo , { PRODUCT , combo } ;                  (* nested selections: no regions *)

filter      = LPAR , ( include | exclude ) , RPAR ;
include     = selector , { COMMA , selector } ;
exclude     = NOT , selector , { COMMA , NOT , selector } ;
selector    = item | PATTERN | HASH , tag ;

(* ---- shaping the job list ---- *)
where       = "where" , cond ;
cond        = conj , { "or" , conj } ;
conj        = neg , { "and" , neg } ;
neg         = "not" , neg | LPAR , cond , RPAR | atom ;
atom        = group , EQ , selector
            | group , "in" , LPAR , include , RPAR
            | HASH , tag ;

pick        = "limit" , count
            | "sample" , count , [ "seed" , number ] ;
repeat      = "repeat" , count ;

(* ---- generation settings and distribution ---- *)
overrides   = "with" , assign , { COMMA , assign } ;
assign      = key , EQ , value ;
value       = WORD | DECIMAL | STRING ;

spread      = "spread" , mode , [ "pinned" ] ;
mode        = "dynamic" | "even" | "blocks" | "by" , group ;

server = generator = fixer = group = item = tag = keyword = key = WORD ;
count       = WORD ;                                         (* digits only, ≥ 1 *)
number      = WORD ;                                         (* digits only, ≥ 0 *)
```

### 6.3 Restrictions beyond the grammar

Syntax errors are those the grammar rejects. In addition:

| # | Rule | Reason |
|---|---|---|
| S1 | `&&` and `\|\|` cannot be mixed within one `combo`. There are no grouping parentheses in a selection; `( )` there only delimit a filter. | Avoids an undefined precedence between merge and zip |
| S2 | A filter is either all-include or all-exclude (the grammar enforces this); a group has at most one filter. | The original silently ignored include lists when an exclude list was present |
| S3 | A `region` is only allowed as a top-level `term`: not an operand of `&&` / `\|\|`, and not inside any `body`. | Nested regions have no defined meaning |
| S4 | A region group has **no item filter**. | Its items define the region keywords (§6.6) |
| S5 | At most one `region` per command. | One region set per image |
| S6 | `selections` contains at least one `term`. | |
| S7 | A `STRING` used as a `literal` is not empty (after trimming). | An empty prompt fragment is meaningless |
| S8 | `count` and `number` consist only of digits; `count` ≥ 1. A decimal where an integer is required is an error. | |
| T1 | Targets in `placement` are distinct. | |
| T2 | A weight (`server=n`) is allowed only when the spread mode is `even`, `blocks` or `by`. Default weight 1. | In `dynamic` mode, capacity is a server setting (`max_concurrent_jobs`), not a weight |
| T3 | A `spread` clause requires at least two targets. With two or more targets and no `spread` clause the mode is `dynamic`. | |
| T4 | `pinned` is not allowed with `dynamic`. | A dynamic job has no preferred server to pin to |
| T5 | In `spread by g`, `g` is the group of exactly one top-level `plain` term. | The partitioning item must be unambiguous |
| R1 | `repeat` > 1 requires seed variation: a `seed` override (§6.8) or a generator with `has_random_seed`. | Otherwise every repeat would be an identical image |

**Precedence**, from loosest to tightest binding:

1. `>` — separates the generation expression from the fixer steps.
2. Clauses `where`, `limit`/`sample`, `repeat`, `with`, `spread` — each applies to the whole product, in that fixed order.
3. `*` — Cartesian product of terms.
4. `&&` / `||` — combine operands into one term; S1 forbids mixing, so no relative order is needed.
5. Postfix `( … )`, `[ … ]`, `{ … }` — attach to the group name immediately before them.

All binary operators are associative: `a && b && c` is one merge of three operands.

Inside a `where` condition: `not` binds tighter than `and`, which binds tighter than `or`; parentheses group conditions.

**Differences from the original parser**

- The original needed single spaces around ` * `, ` && `, ` || `, ` > ` and mis-parsed other spacing. This grammar is token-based: `a*b`, `a * b` and `a  *  b` are equivalent.
- The original accepted several parenthesised lists on one group, e.g. `g(a)(~b)`, and decided exclusion from the first entry only. Here S2 applies and every excluded entry carries its own `~`.
- The original accepted nested or merged regions and then evaluated them as plain groups, silently dropping the braces; here they are errors (S3).
- Everything from `where` onward, literals, patterns, tags, multiple servers and per-fixer servers are new.

### 6.4 Evaluation semantics

A *term* evaluates to an **ordered list of items**; the command evaluates to the Cartesian product of the term lists.

**Item order.** Items of a group are taken in creation order (ascending id), so identical data and command always yield jobs in the same order.

**Selectors.** A selector matches items of the group it is attached to:

| Selector | Matches |
|---|---|
| `alice` | the item with that code name |
| `run%`, `%_alt`, `pose_?` | every item whose code name matches the pattern (case-sensitive; `%` = any run incl. empty, `?` = one character) |
| `#acrobatic` | every item whose `tags` contain that tag |

`include` = union of what its selectors match, in group order, without duplicates. `exclude` = all items of the group minus that union. **Every selector MUST match at least one item**, otherwise validation fails (this generalises "unknown item").

**Constructs**

| Construct | Item list |
|---|---|
| `g` | all items of group `g` |
| `g(…)` | per the selector rules above |
| `"text"` | exactly one anonymous item whose positive prompt is `text`; nothing else |
| `!"text"` | exactly one anonymous item whose negative prompt is `text` |
| `x && y && …` | the operands' lists **concatenated** in operand order; duplicates are kept |
| `x \|\| y \|\| …` | positional tuples of the operand lists, truncated to the shortest; each tuple is **fused** into one item: code name = member code names joined with `+`; positive and negative prompts = the non-empty member prompts joined by a single space; LoRAs, ControlNets and IP-Adapters = members' concatenated in order |
| template | §6.5 |
| region term | the base group's items, all of them (S4), plus region prompts that multiply the result (§6.6) |

Literal items have the code name `lit<k>`, where `k` is the literal's 1-based position among the command's literals; the name is used only in file names.

**Product.** For terms `T1 * T2 * … * Tn`, combinations are ordered lexicographically with the **last term varying fastest**. Writing `R` for the number of region-prompt sets (§6.6; 1 without a region term), the full product has

```
N = |T1| × |T2| × … × |Tn| × R
```

*combinations*. A combination is one choice of an item from every term plus, if present, one region-prompt set.

**Job prompt.** For one combination the positive prompt is its items' non-empty positive prompts joined by single spaces in term order; the negative prompt likewise. Remaining `<tmpl:…>` placeholders are removed afterwards.

### 6.5 Templates

Item prompts may contain placeholders `<tmpl:name>`. For `base[tag1: body1, tag2: body2]`:

1. Select the base group's items (filter applies).
2. Evaluate each tag's `body` as a normal selection expression (§6.4). Each combination of items from that body yields one **fragment** with concatenated positive text, concatenated negative text and merged LoRAs.
3. Take the product of the fragment lists across tags.
4. For every base item × tag-fragment combination, replace `<tmpl:tagN>` in the base item's positive and negative prompt with the fragment's text; merge LoRAs, de-duplicating by LoRA name (first occurrence wins). A tag that does not occur in the base item's prompt leaves it unchanged.
5. De-duplicate the resulting items by a hash of `positive + negative`; keep the first.
6. Each resulting item gets a code name derived from the base item and the fragments used.

Upper bound on the term's size: `|base| × Π over tags (fragments of that tag)`.

### 6.6 Regions (regional prompting)

`base{red: body1, blue: body2}` gives different sub-selections to different parts of the image. The keywords must be those the base group's items define, from either source:

- **Mask regions:** an item holds a reference image; per-region masks are generated (§7.3). Keywords are colour names or `background`.
- **Coordinate regions:** an item holds rectangles `{keyword, width, height, x, y}`.

Evaluation:

1. For each keyword, evaluate its `body` (§6.4) and take its product: this yields that keyword's list of **region prompts** `{keyword, mask_file | coordinates, prompt, loras}`. The prompt is the non-empty positive prompts of the combination's items joined by single spaces; LoRAs are concatenated. Only positive prompts are used per region.
2. Take the product across keywords. Each element is one complete set of region prompts for one combination. `R` is the number of elements.

### 6.7 Shaping the job list: `where`, `limit` / `sample`, `repeat`

The product of §6.4 is processed by a fixed pipeline:

```
combinations (N)  →  where  →  limit | sample  →  repeat  →  jobs
```

**1. `where` — constraints.** Keeps only combinations for which the condition is true.

Each combination has a **membership set**: the `(group, item)` pairs of every original item that contributes to it — the chosen item of a plain or merge operand; every member of a zipped tuple; for a template, the base item and every item used in its fragments; for a region term, the chosen base item and every item used in its region-prompt set. Literals contribute nothing.

| Atom | True when the membership set… |
|---|---|
| `g = sel` | contains an item of group `g` matched by selector `sel` |
| `g in (sel, …)` | contains an item of group `g` matched by any listed selector |
| `#tag` | contains any item (of any group) carrying that tag |

Atoms combine with `not`, `and`, `or` and parentheses. Every group named in a `where` MUST occur somewhere in the command's selections (otherwise the atom could never be true or would be silently ignorable); every selector MUST match at least one item of its group. If the filtered set is empty the command is invalid ("where removed every combination").

**2. `limit n`** keeps the first `n` combinations in product order. **`sample n [seed s]`** keeps `n` distinct combinations chosen uniformly at random without replacement, **retained in product order** (not shuffled). If `n` is not smaller than the number of combinations, all are kept (a preview warning, not an error). When `seed` is omitted the system picks one and **stores it** in the parsed form (§6.10); re-creating jobs from the stored command therefore reproduces the same selection. Given the same data and seed, one implementation must always select the same combinations; the random generator itself is not prescribed.

**3. `repeat k`** turns each remaining combination into `k` jobs placed consecutively, with repeat index `r = 0 … k−1`. Seeds differ per repeat as defined in §6.8.

The resulting number of jobs is

```
jobs = min(N_after_where, n) × k          (limit/sample n; omit min() and n when absent; k = 1 when absent)
```

`limit` and `sample` count **combinations**, not jobs: `limit 20 repeat 4` yields 80 jobs. The preview (§7.2) reports all of N, N after `where`, the combinations kept, and the jobs.

### 6.8 Generation overrides: `with`

`with key=value, …` sets output attributes for **every** job of the command. Precedence, lowest to highest: generator attributes → item attributes (§3.6) → command overrides. Allowed keys and value types:

| Output type | Key | Value |
|---|---|---|
| image | `width` `height` `steps` `batch_size` `seed` | integer |
| image | `cfg` `denoise` | integer or decimal (`denoise` in 0…1) |
| image | `sampler_name` `scheduler` `file_type` | `WORD` or `STRING` |
| video | `width` `height` `num_frames` | integer |
| video | `fps` `duration_seconds` | integer or decimal |
| video | `file_type` | `WORD` or `STRING` |
| 3d | `file_type` | `WORD` or `STRING` |

A key not valid for the **generator's** output type is a validation error. If an item switches a particular job to another output type that does not support a key, the key is ignored for that job. Duplicate keys are an error.

**Seed rules** (images):

1. `with seed=S` → the job's seed is `S + r`, where `r` is the repeat index (so with no `repeat` all jobs use `S`, which keeps results comparable across combinations).
2. Without a `seed` override, if the generator has `has_random_seed` → each job gets a fresh random 64-bit seed.
3. Otherwise the workflow's own seed is used unchanged, and `repeat > 1` is rejected (R1).

The effective seed MUST be recorded on the job (the original discarded it) so that any result can be reproduced.

### 6.9 Splitting work across ComfyUI servers: `placement` and `spread`

The head of a command lists one or more **targets**, e.g. `gpu1, gpu2, gpu3 -$ …`. A single target behaves exactly as in a one-server system. With several targets the finished job list (§6.7) is divided among them according to the `spread` mode. All targets run the **same generator workflow**; the models, LoRAs and custom nodes it uses must exist on every target (§8.7).

Each server executes at most `max_concurrent_jobs` jobs at a time (§3.1, default 1); different servers always run in parallel.

| Mode | Assignment | Typical use |
|---|---|---|
| `dynamic` (default) | **Pull**: no job is pre-assigned. Whenever a server has a free slot it takes the next unstarted job in job order. | Servers of unequal or varying speed; the batch finishes as early as possible |
| `even` | **Static round-robin**: job `i` (0-based) goes to the `i mod W`-th entry of the sequence formed by repeating each target `weight` times in listed order (`W` = sum of weights). Equal weights give `a, b, c, a, b, c …`; `a=3, b=1` gives `a, a, a, b, a, a, a, b …`. | Predictable, evenly interleaved plan |
| `blocks` | **Static contiguous blocks**: target `t` receives `⌊N·w_t / W⌋` consecutive jobs in listed order, where `N` is the number of jobs; the remaining jobs go one each to the targets in listed order. Example: 12 jobs, `a=3, b=1` → jobs 1–9 on `a`, 10–12 on `b`. | Consecutive jobs share earlier terms, so each server reuses the same loaded LoRAs / models |
| `by g` | **Static by item**: the items of group `g` (in the order of its term's list, 0-based position `k`) are assigned to the sequence of `even` (weights apply); every job containing item `k` of `g` goes to that target. | Keep all work for one character / LoRA on one server; route a particular item to a particular server |

`pinned` (static modes only): the assignment is binding — if the preferred server is unavailable the job waits. Without `pinned`, a static assignment is a *preference*: if the preferred server has been unavailable longer than the grace period (§8.7) the job may run on any other available target.

Static modes record each job's preferred server **when jobs are created**, so the plan is visible and reviewable before running. In `dynamic` mode a job's server is set when it starts.

**Fixers on a different server.** A fixer step may carry `on <server>`: `> upscale on gpu2` runs that step on `gpu2` regardless of where the image was generated. The server need not be among the targets, and the step is never re-assigned (it waits if the server is unavailable). Without `on`, a step runs on the server that generated the image. This lets, for example, small GPUs generate and one large GPU upscale. Fixer steps always run in the written order for a given job.

### 6.10 Parsed form

The parsed command is stored with the command (`command_json`) and MUST round-trip to the same structure. Absent clauses are omitted.

```json
{
  "servers": [{"server_code_name": "gpu1", "weight": 1}, {"server_code_name": "gpu2", "weight": 1}],
  "generator_code_name": "anime",
  "group_selections": [
    {"group_code_name": "characters", "include_only": null, "exclude": null},
    {"group_code_name": "poses", "include_only": null, "exclude": ["jumping"]},
    {"literal": "masterpiece", "negative": false}
  ],
  "where": {"op": "not", "arg": {"op": "tag", "tag": "acrobatic"}},
  "sample": {"count": 20, "seed": 7},
  "repeat": 3,
  "overrides": {"steps": 30, "cfg": 6.5},
  "spread": {"mode": "blocks", "by_group": null, "pinned": false},
  "fixers": [{"fixer_code_name": "upscale", "on": "gpu2"}]
}
```

- Selector strings in `include_only` / `exclude` are stored as written: items as-is, patterns as `run%`, tags as `#combat`.
- `where` is a tree: `{"op":"and"|"or","args":[…]}`, `{"op":"not","arg":…}`, `{"op":"eq","group":…,"selector":…}`, `{"op":"in","group":…,"selectors":[…]}`, `{"op":"tag","tag":…}`.
- Merge, zip, template and region terms add `is_merged`/`merged_groups`, `is_zipped`/`zipped_groups`, `is_template`/`template_group_selections` (tag → list of selections) and `is_regioned`/`region_group_selections` (keyword → list of selections), recursively. For a merge or zip, `group_code_name` is the operands' names joined with `+` or `|` and is informational only.
- `sample.seed` is always present in the stored form, even if the user omitted it.

### 6.11 Validation (semantic checks)

Performed when a command is added or edited, and by the preview, after parsing succeeds. All errors are collected and returned together; nothing is persisted on failure.

- Every target server, the generator, every fixer and every fixer `on` server exist.
- Every group referenced anywhere — in selections at any nesting depth, `where` atoms, and `spread by` — exists.
- Every selector (item, pattern, tag) at any depth, including those in `where`, matches at least one item of its group.
- For a region term: the set of keywords in the command **equals** the set defined on the base group's items (mask keys ∪ coordinate keywords). Both "missing in command" and "missing in group" are reported.
- Every group named in a `where` occurs in the command's selections.
- No term evaluates to an empty list, and `where` leaves at least one combination.
- Every `with` key is valid for the generator's output type, values have the right type and range, and no key is duplicated.
- `repeat > 1` satisfies R1.
- For `spread by g`, rule T5 holds.

Syntax and restriction failures (§6.2, §6.3) are reported before any semantic check, each with the offending token and its position.

**Warnings** (returned by the preview, not blocking): `sample`/`limit` larger than the available combinations; a target currently offline; a LoRA used by the command that is not installed on one of the targets; weights that leave a target with zero jobs; total jobs above a configurable soft limit.

### 6.12 Examples

Sample data: groups `characters` = alice, bob, carol; `poses` = standing, jumping, sitting, running (`jumping` and `running` are tagged `acrobatic`); `emotions` = happy, sad, cry; `anime` = 2 items; `scene` = 2 items whose positive prompt contains `<tmpl:subject>`; `duo` = 1 item with mask keywords `red` and `blue`; `style` = 2 items. Servers `gpu1`, `gpu2` exist. Generator `g` has a fixed seed; generator `gr` has `has_random_seed`. Commands below omit the common prefix where it is obvious.

**Valid**

| Command | Jobs | Notes |
|---|---|---|
| `gpu1 -$ g: characters * poses(~jumping) * emotions(cry)` | 3 × 3 × 1 = **9** | |
| `gpu1 -$ g: characters(alice, bob) * emotions` | 2 × 3 = **6** | |
| `gpu1 -$ g: characters && anime` | **5** | one term, 3 + 2 |
| `gpu1 -$ g: characters \|\| emotions` | **3** | zipped pairs, shortest list |
| `gpu1 -$ g: characters*poses(standing)` | **3** | no spaces needed |
| `gpu1 -$ g: characters * poses(#acrobatic)` | 3 × 2 = **6** | tag selector |
| `gpu1 -$ g: characters * poses(run%)` | 3 × 1 = **3** | pattern matches `running` |
| `gpu1 -$ g: characters * "masterpiece, best quality"` | 3 × 1 = **3** | positive literal |
| `gpu1 -$ g: characters * !"blurry"` | **3** | negative literal |
| `gpu1 -$ g: "red hair" && "blue hair" * characters` | 2 × 3 = **6** | literals as alternatives |
| `gpu1 -$ g: scene[subject: characters(alice, bob)]` | 2 × 2 = **4** | template |
| `gpu1 -$ g: duo{red: characters(alice), blue: characters(bob, carol)} * style` | 1 × 2 × R, R = 1 × 2 = 2 → **4** | keywords exactly `red`, `blue` |
| `gpu1 -$ g: characters * poses where not #acrobatic` | 12 → **6** | |
| `gpu1 -$ g: characters * poses where not (characters = alice and poses = jumping)` | 12 − 1 = **11** | |
| `gpu1 -$ g: characters * poses * emotions limit 20` | N = 36 → **20** | first 20 in product order |
| `gpu1 -$ g: characters * poses * emotions sample 10 seed 7` | **10** | reproducible; kept in product order |
| `gpu1 -$ gr: characters * poses limit 5 repeat 4` | 5 × 4 = **20** | random seed per job |
| `gpu1 -$ g: characters * poses repeat 3 with seed=42` | 12 × 3 = **36** | seeds 42, 43, 44 per combination |
| `gpu1 -$ g: characters with steps=30, cfg=6.5, sampler_name=dpmpp_2m` | **3** | overrides |
| `gpu1, gpu2 -$ g: characters * poses` | **12** | `dynamic` by default, both servers pull jobs |
| `gpu1=3, gpu2=1 -$ g: characters * poses spread blocks` | **12** | jobs 1–9 on `gpu1`, 10–12 on `gpu2` |
| `gpu1, gpu2 -$ g: characters * poses spread even pinned` | **12** | alternating; never re-assigned |
| `gpu1, gpu2 -$ g: characters * poses spread by characters` | **12** | `alice` → `gpu1`, `bob` → `gpu2`, `carol` → `gpu1` |
| `gpu1 -$ g: characters > upscale on gpu2 > face` | **3** | generate on `gpu1`; `upscale` on `gpu2`; `face` back on the generating server |

**Invalid**

| Command | Rejected because |
|---|---|
| `gpu1 -$ g:` | S6, missing selection |
| `gpu1 - g: characters` | missing `-$` |
| `gpu1 -$ g: characters && poses \|\| emotions` | S1, mixed operators |
| `gpu1 -$ g: poses(standing, ~jumping)` | S2, include and exclude mixed |
| `gpu1 -$ g: poses(a)(~b)` | S2, several filters |
| `gpu1 -$ g: scene[subject: duo{red: x}]` | S3, region inside a body |
| `gpu1 -$ g: duo{red: x} && characters` | S3, region as a merge operand |
| `gpu1 -$ g: duo(a){red: x}` | S4, filter on a region group |
| `gpu1 -$ g: duo{red: x} * duo{blue: y}` | S5, two regions |
| `gpu1 -$ g: characters * ""` | S7, empty literal |
| `gpu1 -$ g: characters limit 5 sample 3` | at most one of `limit` / `sample` |
| `gpu1 -$ g: characters repeat 3 where #acrobatic` | clause order |
| `gpu1 -$ g: characters with steps=` | syntax: missing value |
| `gpu1 -$ g: characters limit 0` | S8, count must be ≥ 1 |
| `gpu1 -$ g: characters repeat 3` | R1, generator `g` has no seed variation |
| `gpu1, gpu1 -$ g: characters` | T1, duplicate target |
| `gpu1=2, gpu2 -$ g: characters` | T2, weights need `even` / `blocks` / `by` |
| `gpu1 -$ g: characters spread even` | T3, needs two targets |
| `gpu1, gpu2 -$ g: characters spread dynamic pinned` | T4 |
| `gpu1, gpu2 -$ g: characters && anime spread by characters` | T5, group is not a plain top-level term |
| `gpu1 -$ g: characters(nobody)` | valid syntax; §6.11: unknown item |
| `gpu1 -$ g: poses(#nosuchtag)` | §6.11: selector matches nothing |
| `gpu1 -$ g: characters where emotions = sad` | §6.11: `emotions` not part of the command |
| `gpu1 -$ g: characters with colour=3` | §6.11: unknown key |
| `gpu1 -$ with: characters` | `with` is a reserved word |
| `gpu1 -$ g: chars$` | lexical error, `$` outside `-$` |

## 7. Job creation

### 7.1 Expansion

1. Re-parse the command text; resolve the servers, generator and fixers.
2. Evaluate the terms and take the Cartesian product (§6.4), including region-prompt sets (§6.6).
3. Apply `where`, then `limit` / `sample`, then `repeat` (§6.7).
4. For every resulting job:
   - positive/negative prompts: items' prompts joined by a single space in term order, then placeholders removed;
   - LoRAs, ControlNets, IP-Adapters: concatenated across items in term order; **a LoRA that two items share is kept once, with the strengths of the first** (two spellings of one file, `style` and `style.safetensors` or `a\b` and `a/b`, are one file), because loading it twice would apply it twice;
   - used `{group, item}` list (literals contribute none);
   - output type/attributes: generator's → overridden by the last item that defines both → overridden by the command's `with` values (§6.8); effective seed recorded;
   - repeat index;
   - result file name derived from the generator, command id, group and item code_names, region index and repeat index (**not** the server: under `dynamic` spread it is not known yet), with the extension from the output file type;
   - `total_fixers` = ordered fixer steps, each with its optional server; status `idle`.
5. Assign preferred servers for static spread modes (§6.9).

Jobs are created when the command is added or edited, **not** when it is run, so the user can inspect and edit them first. A command is `finished` when **all** of its jobs are `finished` (the original relied on one job being flagged last, which breaks when jobs finish out of order on several servers).

### 7.2 Command operations

| Operation | Behaviour |
|---|---|
| Preview | Parse and validate a command text (§6.11) and return: errors, warnings, N, N after `where`, combinations kept, number of jobs, and the job count per server (static spread) or "dynamic". Persists nothing |
| Add | Append (order = max + 1) or insert at a position, shifting later commands by +1; validate; persist; create jobs |
| Edit | If the command text changed: re-parse, re-validate, update name/description/code, delete old jobs and their files, create new jobs. (Original: no change if the text is identical, even if name/description changed.) |
| Delete | Delete jobs and result files, delete the command, shift later orders by −1 |
| Move / up / down | Reorder, shifting neighbours; no-op at boundaries |
| Renumber | Reassign orders 1..N |
| Run | `idle` jobs → `queued`, command → `queued`, hand the command to the execution queue |
| Stop | `queued` jobs → `idle`, command → `idle`; does not interrupt a running job |
| Recreate | Delete jobs, regenerate from stored text, command → `idle` |
| Download | Only when `finished`. Produce an archive containing every job's result file plus a JSON summary (each job's positive prompt, negative prompt, LoRAs); remember its path on the command |

### 7.3 Mask generation

Given a reference image, produce one mask image per region and return `{keyword → mask}`. The reference is a **flat-colour** picture: regions are painted in solid colours on a white background, with no anti-aliasing, and saved as PNG (lossy formats such as JPEG introduce stray colours and are rejected).

- One binary mask per distinct non-white colour (so at most 10 distinct non-white colours; more is an error).
- Named colours: red, green, blue, yellow, magenta, cyan, maroon, dark_green, navy, olive, orange, purple, pink, brown, gray; any other colour is named `color_R_G_B`.
- A `background` mask is added: white pixels.
- Failure cases (unreadable image, no non-white colour, more than 10 colours, unsupported format) are reported as `MaskGenerationFailed` with a reason in the metadata.

Automatic detection of people (segmentation) is **out of scope**: the original used a machine-learning model for it, and this version deliberately has none. Regions are defined by hand-painted colour masks or by coordinates (§3.6).

### 7.4 Deletion rules

- Delete item: remove its IP-Adapter image, mask reference image and mask folder, and ControlNet images.
- Delete group: delete all its items (as above), then the group.
- Delete project: delete all its jobs (and result files), all its commands, then the project.

## 8. Execution

### 8.1 Workers

The following run concurrently for the life of the application:

| Worker | Behaviour |
|---|---|
| Server monitor | Every second, probe each registered server (any cheap request that succeeds only if ComfyUI is reachable). Reachable → mark available; unreachable → mark unavailable, record the time since which it has been unavailable, and release its client |
| Command dispatcher | Takes commands from a command queue and makes their jobs eligible for scheduling in job order |
| Scheduler / server runners | One runner per available server. Each runner executes up to the server's `max_concurrent_jobs` jobs at once, choosing jobs as defined in §8.7. Servers run in parallel. For each chosen job it generates if not yet generated, otherwise applies the next fixer step |

### 8.2 Generation

Starting from the generator's workflow, apply in this order:

1. Write positive and negative prompt text into the nodes with the configured titles: into the node's `text` input, or into `prompt` when it has no `text` (as the Qwen Image text encoders do). A node that also has a `negative_prompt` input takes the negative prompt itself: the negative title may be left empty or name the same node. A node with no such input is an `InjectorFailed` error, never a silent no-op. A node without a `_meta.title` is addressed by its class name.
2. For image generators, apply the effective output attributes (generator → item → command `with`; §6.8) to every sampler (`KSampler`, `KSamplerAdvanced`: steps, cfg, sampler_name, scheduler, denoise, seed) and to the empty-latent nodes (width, height); only non-null values. No sampler present → error.
3. Seed: apply the job's effective seed (§6.8). If `has_random_seed` and no seed override exists, generate a random 64-bit seed per job, set it on every `KSampler` and record it on the job.
4. **IP-Adapter** injection: for each adapter add a CLIP-vision loader, image loader, IP-Adapter model loader and an advanced IP-Adapter node, chained.
5. **LoRA** injection (`internal/lorainjector`): checkpoint-style workflows (a `CheckpointLoader*` node) get a `LoraLoader` per LoRA, reading MODEL and CLIP from the checkpoint; split-style workflows (`UNETLoader` + `CLIPLoader`) get a `LoraLoaderModelOnly` per LoRA, reading MODEL from the UNET loader, so `strength_clip` is not used there. A name without `.safetensors` gets it. The loaders are chained in the order of the job's LoRA list (the items in term order, then the LoRAs of the regions), and every reader of the original model (and, for a checkpoint, CLIP) is rewired to the last loader; the VAE stays. The loader is the first checkpoint loader (or the UNET loader) of the workflow: a workflow with several checkpoint loaders is ambiguous. The injector does not drop repeats: the job plan keeps each LoRA file once, with the strengths of the first item (§7.1).
6. **ControlNet** injection: per config add an image loader, an optional preprocessor, a preview save node for the preprocessed image, a ControlNet loader and `ControlNetApplyAdvanced`, wired between the text encoders and the sampler. Preprocessors: openpose → `OpenposePreprocessor`, dwpose → `DWPreprocessor`, canny → `CannyEdgePreprocessor`, midas → `MidasDepthMapPreprocessor`, depth → `DepthAnythingPreprocessor`, lineart → `LineArtPreprocessor`, softedge → `HEDPreprocessor`, scribble → `ScribblePreprocessor`, normal → `BAE-NormalMapPreprocessor`, tile → none. `is_reference = true` means preprocess; `false` means use the image as-is.
7. **Region** injection: starting at the sampler's positive input, follow `ControlNetApplyAdvanced` / `ControlNetApply` back to the base text-encode node. For each region add a text encode, then either `LoadImage → ImageToMask(red) → ConditioningSetMask` (mask) or `ConditioningSetArea` (coordinates), and chain with `ConditioningCombine`. A region must have a mask xor coordinates. Requires a `KSampler`.

Then: submit to the job's server (after making its input files available there, §8.7), record the prompt id, status → `processing`, notify `processing`; wait for completion (success event, or queue drained); fetch output images; save to the job's result path; `is_generated = true`.

**Removing the result from ComfyUI.** ComfyUI has no call that deletes a file, so when the setting `delete_comfy_outputs` is on (the default), the file the job just saved is removed through the comfyui-model-crud extension of the server (§18.8) after the copy is written, for the generation and for every fixer step (§8.3): `DELETE /model_crud/output?path=<subfolder>/<filename>`. Only files of type `output` are removed, never `temp` or `input` files. A file that is already gone (`file_not_found`) is not an error. A file that cannot be removed (the server is not reachable, the extension is missing, switched off or refuses) is logged as a warning with the tag of the failure (`ModelCrudMissing`, `ModelCrudDisabled`, `ModelCrudRejected`, `ServerUnreachable`) and does **not** fail the job, which has its copy. ComfyUI's history keeps its entry for the prompt.

- Fixers remaining → status `queued`, re-enqueue.
- Otherwise → `finished`; if all of the command's jobs are now `finished`, the command also becomes `finished`.
- Notify `finished`.

**On a workflow failure** (ComfyUI rejects or fails the workflow, an injector cannot apply, no sampler present…): store error message and trace, status `error`, return all of the command's other `queued` jobs to `idle` (halting the batch), notify `error`. Jobs already running on other servers are not interrupted and finish normally. **Loss of the server** (unreachable, connection dropped) is *not* a workflow failure and is handled by §8.7.

### 8.3 Fixing

Take the first fixer step in `total_fixers` not in `finished_fixers`. Run it on its step server (explicit `on` server, else the server that generated the image; §6.9, §8.7). Feed the job's current result image into the fixer's image-input node, run it, and **replace** the result image with the output. Append the fixer to `finished_fixers`. When all are done: `is_fixed = true`, status `finished`, command `finished` if all its jobs are `finished`; otherwise status `queued` and the job is re-offered to the scheduler. Notify after each step.

### 8.4 Job operations

| Operation | Behaviour |
|---|---|
| Run | If `idle` → `queued`, enqueue |
| Stop | If `queued` → `idle`, resetting `is_generated`, `is_fixed`, `finished_fixers` |
| Edit | If either prompt changed: save, reset generation/fix flags and fixer progress, status `idle` |

### 8.5 Notifications

Because several servers produce events at once, a single "latest" slot would lose events. The system keeps a bounded, ordered log (at least the last 100) of `{id, type, job id, command id, project id, server, created_at}` with increasing ids. A client asks for events after the last id it saw and receives them in order. (The original kept only one notification and discarded it after 4 s.)

### 8.6 REPL

Runs a single job without a project. Inputs: server, generator, extra positive and negative text, a list of items written `group(item),group(item)` (must match `^(\w+\(\w+\))(,\w+\(\w+\))*$`), and an optional LoRA list in which every entry has `name`, `strength_model`, `strength_clip`. Item prompts and LoRAs are concatenated, then the user's text appended. Any previous REPL job and its result are deleted; the new job (project = command = −1) is queued; the result is always stored under the same fixed name.

### 8.7 Multi-server scheduling

**Roles.** A job has a *preferred server* (static spread) or none (`dynamic`), and a `pinned` flag. A server is *available* when its last probe succeeded; it has *capacity* when it runs fewer than `max_concurrent_jobs` jobs.

**Choosing work.** Whenever an available server has capacity, it takes the lowest-numbered `queued` job (in job order) that is **eligible** for it:

| Job | Eligible on server `s` when |
|---|---|
| no preferred server (`dynamic`) | always |
| preferred server = `s` | always |
| preferred server ≠ `s`, not pinned | the preferred server has been unavailable for longer than the grace period |
| preferred server ≠ `s`, pinned | never |
| fixer step with an explicit `on` server ≠ `s` | never |
| fixer step without `on`, image generated on another server | the generating server has been unavailable longer than the grace period (the application holds the image, so any server can run the step) |

Static plans therefore never leave work unassigned, but they can leave a server idle while another is busy; that is the cost of a predictable plan. Under `dynamic` a server is idle only when no queued job remains.

**Server loss.** If the server of a running job becomes unreachable, the job's execution is abandoned (best effort cancel), its status returns to `queued`, and `attempts` increases. After `max job attempts` the job becomes `error`, with a message naming the server. This does **not** halt the rest of the command.

**Input files.** Everything a workflow reads — reference images, ControlNet images, mask images, IP-Adapter images and the image fed to a fixer — MUST be available to the target server. The system SHOULD upload the file through ComfyUI's upload facility before submitting, under a unique name, for every server (§18.6); passing a path is only allowed for `is_local` servers. Results are always downloaded to the application's results location, whichever server produced them.

**Model availability.** The same generator workflow runs everywhere, so checkpoint, LoRA, ControlNet and IP-Adapter model names must exist on every target. The system does not install models; a missing model surfaces as a workflow failure. The preview SHOULD warn when a LoRA used by the command is not listed by one of the targets (§9, last bullet).

**Stopping.** `stop` returns `queued` jobs to `idle` on all servers; jobs already running finish.

## 9. Civitai integration

Requires the API token and LoRA cache directory; otherwise the operation fails with a clear message.

- Download a model by id, producing `<id>.safetensors` and `<id>.json` (trigger words, tags, description, model name). The newest version is taken unless a version id is given; a chosen version is stored below `versions/<version id>` in the cache, so versions of one model do not overwrite each other, and it is installed as `<id>-<version id>.safetensors`.
- Upload the model to a chosen ComfyUI server, into its `loras` folder and a chosen sub-folder, through the comfyui-model-crud extension (§18.8); the server is named by its code name, must exist and be online, and is checked before anything is downloaded. The web dialogs of the import (group import and adding a LoRA to a group) show a select of the servers, by name, and a single server is chosen at once. The REST API takes the server's code name (`server` in the body).
- Create a group of LoRAs: one item per model with code_name = model id (`<model id>_<version id>` when a version was chosen), name = Civitai model name, positive_prompt = trigger words separated by blank lines, and one LoRA entry with the chosen model/clip strengths. When importing several models, pause 15 s between downloads.
- Create a single LoRA item in an existing group.
- List LoRAs available on a ComfyUI server.

### 9.1 LoRA pages

A LoRA has no record of its own. It is a file name that items carry (§3.6) and that ComfyUI servers hold in their `loras` folder. The **LoRAs page** (`/loras`) and the LoRA routes (§15.2) join the two: one row for each file name that any item carries or any server that answers lists, with the number of items that carry it and the code names of the servers that have it. Two spellings of one file (`style` and `style.safetensors`; a backslash and a slash) are one row, shown as `<path>.safetensors` with slashes. The list can be filtered by a part of the name (not case sensitive) and is paged. A server that is offline or does not answer is not asked; the list names it, because its LoRAs are missing from the list.

- **Details.** Opening a LoRA says, for every server, whether it is installed there (or that the server was not asked), and lists the items that carry it (group, item, the two strengths). A name that no item carries and no server has is `LoRANotFound`.
- **Deleting a LoRA** removes it everywhere: its file from every server that has it (`DELETE /model_crud/models/loras?path=<path>` of the extension, §18.8; a file that is already gone is not an error), its model and metadata from the Civitai cache (only for names an import made: `<id>.safetensors`, `<id>-<version>.safetensors`, §9), and **every item that carries it**, with those items' files. The web page asks twice, and the dialog says how many items go. **Nothing is touched unless every server answers**: a server that is offline or does not answer, or that refuses the delete, stops the operation with `LoRADeleteFailed`, naming those servers, and no item is deleted; doing it again after fixing them finishes the job, because the servers that already deleted the file are not asked to find it. Deleting an item that a command's jobs used does not change those jobs (§7.4).
- **Autocomplete** of the item dialog (`GET /ui/loras/suggest?q=`): the names that contain the text, those that start with it first, at most 12, from the same join. A name can only be added from this list; a name that nothing and nobody has cannot be typed in.

## 10. Persistence and files

- Schema is created automatically. A reimplementation should add schema versioning and migrations.
- Uploaded files get unique prefixed names so uploads never collide.
- Result and reference files are readable by the user interface; the system provides no access control (§11).
- **Importing data from the original application:** its category of a group (other than `none`) becomes a group tag; its categories are otherwise dropped.

## 11. Non-functional requirements

- **Security:** the original has no authentication. A reimplementation should either require it or document that it must only run on a trusted network. Server addresses are user-supplied, so the system will connect to arbitrary hosts; API tokens and workflows are stored in plain text.
- **Privacy:** no telemetry; the application contacts only the servers, the Civitai domain the user configured, and nothing else.
- **Concurrency:** servers run in parallel, each up to its `max_concurrent_jobs`. A job is claimed by exactly one server at a time (claiming is atomic); state shared between workers (queues, server availability, notification log) must be safe for concurrent access.
- **Reliability:** state survives restart. Jobs left `queued` or `processing` after a crash need a defined recovery rule (the original has none).
- **Observability:** use structured logging; the original only printed diagnostics.
- **Testing:** unit tests for the command parser and expansion, mask generation, and each workflow injector; integration tests with a fake ComfyUI.

## 12. External-service requirements

The README of the original lists the author's ComfyUI custom nodes, noting uncertainty about which are actually required: Unload-Model, A8R8 nodes, UltimateSDUpscale, Advanced-ControlNet, IPAdapter-plus, Impact-Pack, Inspire-Pack, controlnet_aux, prompt-control, RMBG, Shinsplat.

From the injectors, the target ComfyUI must provide: `IPAdapterAdvanced`, `IPAdapterModelLoader`, `CLIPVisionLoader`; `ControlNetApplyAdvanced` and the preprocessors in §8.2; core nodes `LoraLoader`, `LoraLoaderModelOnly`, `ConditioningSetMask`, `ConditioningSetArea`, `ConditioningCombine`, `ImageToMask`. Each fixer additionally needs whatever nodes its own workflow uses.

## 13. Acceptance tests

1. `s -$ g: a * b(~x) * c(y)` parses into three selections with correct include/exclude; adding `> f1 > f2` gives fixers `[f1, f2]`.
1a. Every row of the valid table in §6.12 parses and yields the stated job count; every row of the invalid table is rejected with the stated rule (S1–S8, T1–T5, R1, lexical or semantic) and the offending token's position.
1b. `a*b`, `a * b` and `a   *   b` parse to identical structures; the parsed form of any valid command converts to JSON and back unchanged.
1c. Job order for `A * B` with 2 and 3 items is A1B1, A1B2, A1B3, A2B1, A2B2, A2B3, and is identical across repeated runs.
2. `a && b` yields one merged selection whose items are the concatenation, duplicates kept; `a || b` yields min(len) paired items whose code name joins the members with `+`, including items that carry ControlNets.
3. Groups of sizes 3, 4 and 2 yield 24 jobs, in the order of §6.4.
4. A command referencing an unknown server, generator, fixer, group or item — including one nested inside a template tag or region body — returns one error per problem and creates nothing.
5. A region command whose keywords differ from the group's is rejected with both diff messages; two region groups in one command is rejected.
6. A generator with `has_random_seed` produces different seeds on repeated jobs.
7. LoRA injection works for both a checkpoint-style and a split-style workflow.
8. A job with two fixers moves `queued → processing → queued → processing → finished`, ends with `finished_fixers == total_fixers`, and its command becomes `finished` after the last job.
9. A failure during generation sets error and trace, notifies `error`, and returns the command's other queued jobs to `idle`.
10. Downloading an unfinished command is refused; a finished one yields an archive with every result and the prompt summary.
11. Template expansion: `base[tag: g]` replaces `<tmpl:tag>`, de-duplicates identical prompts, and leaves no placeholder in any job prompt.
12. Deleting a group, item or project removes the dependent records and files listed in §7.4.
13. Notification retrieval returns, in order, every event after a given cursor, and none that occurred before it, even when two servers emit events simultaneously.
14. Every row of the valid and invalid tables in §6.12 behaves as stated, including job counts.
15. Selectors: `poses(run%)`, `poses(pose_?)` and `poses(#acrobatic)` match exactly the items described in §6.4; an exclusion with a pattern removes all matches; a selector matching nothing is a validation error.
16. Literals: `"x"` adds exactly one positive-only item, `!"x"` one negative-only item; a merge of two literals yields two alternatives; a literal never appears in a job's `{group, item}` list.
17. `where`: `not #acrobatic`, `g = sel`, `g in (…)`, `and` / `or` / parentheses behave per §6.7 on combinations produced by merge, zip, template and region terms; a `where` that removes everything is rejected.
18. `limit n` keeps the first `n` combinations; `sample n seed s` returns the same selection every time, in product order; omitting the seed stores the generated seed so re-creating the command reproduces the same jobs; `limit 20 repeat 4` yields 80 jobs.
19. `repeat 3 with seed=42` yields seeds 42, 43, 44 for each combination; a generator with `has_random_seed` yields distinct recorded seeds; `repeat 2` with neither is rejected (R1).
20. `with` overrides win over item and generator attributes; a key invalid for the generator's output type is rejected.
21. Spread: with 12 jobs, `a=3, b=1` under `blocks` gives jobs 1–9 to `a` and 10–12 to `b`; under `even` gives `a,a,a,b` repeating; `by characters` sends every job with the same character to the same server; `dynamic` with two servers of different speed finishes the batch earlier than `even` and never assigns two jobs to one server beyond its `max_concurrent_jobs`.
22. Failover: when a server becomes unreachable mid-job, its job returns to `queued` and runs elsewhere after the grace period (unless `pinned`), the batch keeps going, and the job fails only after `max job attempts`; a workflow failure still halts the remaining queued jobs but does not interrupt running ones.
23. `> upscale on gpu2` runs the step on `gpu2` even when generation ran on `gpu1`; the image is uploaded when `gpu2` is not local; a step with no `on` runs where the image was generated.
24. Preview returns the same job count that Add then creates, reports per-server counts for static modes, persists nothing, and warns for an offline target.
25. A command is `finished` only after all its jobs are `finished`, regardless of the order in which servers complete them.

---

# Part II — Technical binding: Go, REST, Datastar

Sections 1–13 describe behaviour and stay technology-neutral. Sections 14–20 bind that behaviour to the chosen technology. Where they appear to conflict, §1–13 define *what* happens and §13–20 define *how* it is delivered.

## 14. Technology and architecture

### 14.1 Stack

| Concern | Choice |
|---|---|
| Language | Go ≥ 1.26.3 (the version the three project libraries declare in `go.mod`) |
| Delivery | One server process, one binary, serving the REST API, the web UI and the static files |
| API | REST over HTTP, JSON bodies, versioned under `/api/v1` (§15) |
| UI | Server-rendered HTML enhanced with **Datastar** (v1 SSE protocol); no client-side build step (§16) |
| Errors | `github.com/rm4n0s/errors` — used for every error, project-wide (§17) |
| ComfyUI | `github.com/rm4n0s/comfyui-client-go` behind an internal port (§18) |
| Civitai | `github.com/rm4n0s/gocivitailib` behind an internal port (§19) |
| HTTP, JSON, logging, files, zip, images | Standard library: `net/http` (Go 1.22+ method/path patterns), `encoding/json`, `log/slog`, `os`, `archive/zip`, `image/*`, `embed`, `html/template` |
| Storage | SQLite through `gorm.io/gorm` with the official `gorm.io/driver/sqlite` driver (§14.4) |

Further dependencies require explicit approval (project rule); the approved set is in §14.4. `gorilla/websocket` arrives transitively through `comfyui-client-go` and is not a new direct dependency.

### 14.2 Package layout

Follows the repository layout rules: thin `cmd/`, application code in `internal/`, tests beside the code.

```
cmd/bos/main.go            parse flags, load config, build the object graph, start the server
cmd/bosctl/main.go         command-line client for the REST API (§15.6); thin, code in internal/cli
internal/config            configuration loading and validation (§2)
internal/domain            entities, enums, value types (§3); no I/O, no dependencies
internal/command           command language: lexer, parser, AST, validator, expander (§6); pure, no I/O
internal/workflow          workflow editing on ComfyUI API-format graphs: attribute, seed, LoRA,
                           ControlNet, IP-Adapter and region injectors; output analysis (§8.2, §3.2)
internal/mask              region-mask generation (§7.3)
internal/store             repositories over SQLite with gorm; numbered Go schema migrations (§10)
internal/service           application services (use cases): servers, generators, fixers, tags,
                           groups, items, projects, commands, jobs, repl, civitai import
internal/scheduler         server monitor, dispatcher, per-server runners, event log (§8)
internal/comfy             ComfyUI port + adapter over comfyui-client-go, event hub (§18)
internal/modelcrud         client of the comfyui-model-crud extension: delete outputs, upload models (§18.8)
internal/civitai           Civitai `Models` port + adapter over gocivitailib; LoRA upload to ComfyUI (§19)
internal/events            ordered, bounded event log with subscriptions (§8.5)
internal/api               REST handlers, DTOs, error mapping (§15)
internal/cli               `bosctl` commands: an HTTP client of §15, no access to the store or services (§15.6)
internal/ui                Datastar handlers, templates, embedded static assets (§16)
internal/files             file storage under the configured directories, upload handling, safe serving
```

### 14.3 Architecture rules

1. **One application core, two thin front ends.** `internal/api` and `internal/ui` both call `internal/service`; neither contains business rules, and the UI never calls the REST API. This keeps both in step by construction.
2. **Dependencies point inward.** `domain` and `command` import nothing from the project. `service` depends on small interfaces (ports) for the store, ComfyUI, Civitai, files and clock, defined next to the code that uses them; concrete adapters are wired in `cmd/bos`. This is what lets §20 use fakes.
3. **No global mutable state.** Everything (config, store, hub, scheduler, event log) is constructed in `main` and injected through constructors.
4. **`context.Context` first** on anything that does I/O. Request contexts propagate into services; background workers take the application context and stop cleanly on shutdown (`http.Server.Shutdown`, then workers, then store).
5. **Concurrency.** Each server runner is a goroutine owned by the scheduler; shared state is behind the scheduler/hub/event-log types, never touched directly by handlers. Claiming a job is a single atomic store operation (e.g. `UPDATE … WHERE status='queued' AND id=?` returning the affected row count).
6. **Pure core for the command language.** Lexing, parsing, validation against a read-only catalogue interface, and expansion (item lists, product, `where`, sample, repeat, spread) are deterministic functions testable without a database.

### 14.4 Decisions (confirmed)

| # | Decision | Outcome |
|---|---|---|
| D1 | Database access | `gorm.io/gorm` with the official `gorm.io/driver/sqlite` (SQLite through `github.com/mattn/go-sqlite3`, so **cgo and a C compiler are required** to build and run). No SQL is written by hand: queries use gorm's builder, and records in `internal/store` map to the domain types |
| D2 | Settings | Stored in the database (a table of names and text values), edited in the web interface; `BOS_<SETTING>` environment variables override them for one run; the command line gives only the database path and the log level |
| D3 | HTML templating | `html/template` |
| D4 | Datastar server SDK | `github.com/starfederation/datastar-go` (`NewSSE`, `PatchElements`, `MarshalAndPatchSignals`, `ReadSignals`) |
| D5 | Person segmentation (the original's YOLO model) | **Removed.** Regions come from colour masks and coordinates only (§7.3) |
| D6 | Image formats for mask references | PNG only (flat colours); thumbnails and other images: PNG, JPEG, GIF. `image/webp` is not in the standard library |
| D7 | Authentication | Listen on `127.0.0.1` by default; optional shared bearer token (§15.1, §16.6) |
| D8 | Schema migrations | Numbered Go migrations that use gorm's `Migrator` on structs frozen for each migration, applied in order, one transaction each, version stored in a table |

New direct dependencies are therefore exactly: `gorm.io/gorm`, `gorm.io/driver/sqlite`, `github.com/starfederation/datastar-go`, and the three `rm4n0s` libraries.

## 15. REST API

### 15.1 Conventions

| Topic | Rule |
|---|---|
| Base path | `/api/v1` |
| Formats | Requests and responses are `application/json; charset=utf-8`, except file uploads (`multipart/form-data`) and file downloads |
| Names | JSON fields use `snake_case`, matching the field names of §3 |
| IDs | Positive integers. Code names (§6.1) are unique and also accepted where a resource is addressed by code name in a *query*, but paths use ids |
| Timestamps | RFC 3339 UTC |
| Pagination | List endpoints take `page` (default 1) and `page_size` (default 20, max 200) and return `{"items":[…],"total":N,"page":1,"page_size":20,"total_pages":N}` |
| Create | `201 Created`, `Location` header, full resource in the body |
| Update | `PUT` replaces all editable fields; returns the resource |
| Delete | `204 No Content`; deletion cascades are those of §7.4 |
| Long-running | Starting work (`run`, `recreate`, imports) returns `202 Accepted` with the affected resource; progress is observed through §15.4 |
| Unknown JSON fields | Rejected with `400` |
| Body limits | JSON bodies ≤ 8 MiB (workflows are large); upload size limits are configuration settings. A body over the limit is `UploadTooLarge` (413) |
| Uploads | Where a resource takes files (groups, items), the request is either JSON or `multipart/form-data` with the JSON in a part named `data` and the files in further parts: `thumbnail` for a group or an item, and for an item `ip_adapter_image`, `mask_reference` and `controlnet_<n>` (the image of the n-th entry of `controlnets`, counting from 0). Thumbnails are PNG, JPEG or GIF; the other images may also be WebP, except `mask_reference`, which must be PNG (§7.3). The type is read from the content, not from the name the client gives. Clients upload files but never name them: a file path in the JSON must be one the entity already has, and the mask folder and masks are made by the server. A request that fails leaves no uploaded file behind |
| Auth | If `auth_token` is configured, every `/api/v1` request needs `Authorization: Bearer <token>` (`401` otherwise). `/healthz` is always open |
| CORS | Off. Same-origin only unless an allow-list is configured |

**Errors.** Every non-2xx response has this body, produced from an `*errors.Error` (§17):

```json
{"error": {"tag": "CommandValidationFailed", "message": "command is not valid", "metadata": {"errors": [ … ]}}}
```

`tag` is the stable, machine-readable identifier. Two envelopes are not in the tag catalogue because they say nothing about the application's own rules: `Unauthorized` (401, a missing or wrong bearer token) and `InternalError` (500, a failure that is not an application error, or a panic). The details of any 500 are withheld from the client and logged, and a `path` in the metadata is never sent. The stack trace and route are **never** sent to clients; they are logged (§17.4).

### 15.2 Resources

`{id}` are integers. "Err" lists the notable tags (full list in §17.2).

**Servers**

| Method and path | Operation | Success | Notable errors |
|---|---|---|---|
| `GET /servers` | list with live `status` | 200 | |
| `POST /servers` | create | 201 | `CodeNameInvalid`, `ServerCodeNameTaken` |
| `GET /servers/{id}` · `PUT` · `DELETE` | get / edit / delete | 200 / 200 / 204 | `ServerNotFound`, `ServerCodeNameTaken` |
| `GET /servers/{id}/loras` | LoRAs installed on that ComfyUI | 200 | `ServerUnreachable` |

**LoRAs** (§9.1)

| Route | Purpose | Success | Notable errors |
|---|---|---|---|
| `GET /loras?q=&page=&page_size=` | page of `{name, items, servers}` (the LoRAs of all items and all servers) plus `unreachable` (servers not asked) | 200 | `InvalidRequest` |
| `GET /loras/detail?name=` | `{name, items:[{id, group_id, group_code, code_name, name, strength_model, strength_clip}], servers:[{code_name, name, reachable, installed}]}` | 200 | `InvalidRequest`, `LoRANotFound` |
| `DELETE /loras?name=` | delete the LoRA from every server and the cache, and the items that carry it; returns `{name, servers, items, files}` | 200 | `InvalidRequest`, `LoRANotFound`, `LoRADeleteFailed` |

**Generators and fixers**

| Method and path | Operation | Success | Notable errors |
|---|---|---|---|
| `GET /generators` · `POST /generators` | list / create | 200 / 201 | `GeneratorCodeNameTaken`, `GeneratorTitleRequired`, `WorkflowInvalid`, `WorkflowTitleDuplicated` |
| `GET /generators/{id}` · `PUT` · `DELETE` | get / edit / delete | 200 / 200 / 204 | `GeneratorNotFound` |
| `POST /generators/analyze` | body `{workflow}` → inferred `output_type`, `output_attributes`, output node, candidate prompt-node titles and the suggested `positive_prompt_title` / `negative_prompt_title` (the nodes whose titles start with `positive` / `negative`); persists nothing | 200 | `WorkflowInvalid`, `OutputNodeNotFound` |
| `GET/POST /fixers`, `GET/PUT/DELETE /fixers/{id}` | CRUD | | `FixerNotFound`, `FixerCodeNameTaken` |

**Groups and items**

| Method and path | Operation | Success | Notable errors |
|---|---|---|---|
| `GET /groups` (filter `?tag=`) · `POST /groups` | list / create (fields include `tags`; thumbnail via multipart) | 200 / 201 | `GroupCodeNameTaken`, `CodeNameInvalid`, `CodeNameReserved` |
| `GET /groups/{id}` · `PUT` · `DELETE` | get / edit / delete (removes items and files) | | `GroupNotFound` |
| `POST /groups/import-text` | body: group fields + `text` (one item per line) | 201 | `GroupCodeNameTaken` |
| `POST /groups/import-civitai` | body: group fields + `models:[{model_id,version_id,model_strength,model_clip}]` (a strength left out is 1, a `version_id` left out is the newest version), `server` (the code name of the server the LoRAs are uploaded to) and `lora_subfolder` (below the `loras` folder). The group is created at once and returned; the downloads go on in the background, one import at a time, with a pause between models, and report as `import_progress`, `import_error` and `import_finished` events (§15.4) that carry `group_id` and `message` | 202 | `CivitaiNotConfigured`, `InvalidRequest` (an unusable sub-folder, or no server), `ServerNotFound`, `ServerUnreachable` |
| `GET /groups/{id}/items` · `POST /groups/{id}/items` | list / create (multipart for images) | 200 / 201 | `ItemCodeNameTaken`, `GroupNotFound`, `UploadTooLarge`, `MaskGenerationFailed` |
| `GET /items/{id}` · `PUT` · `DELETE` | get / edit / delete | | `ItemNotFound` |
| `POST /groups/{id}/items/civitai` | add one Civitai LoRA as an item: `{model_id, version_id, model_strength, model_clip, server, lora_subfolder}`; returns the group, the item arrives in the background | 202 | `CivitaiNotConfigured`, `GroupNotFound`, `InvalidRequest`, `ServerNotFound`, `ServerUnreachable` |

**Tags**

| Method and path | Operation | Success | Notable errors |
|---|---|---|---|
| `GET /tags?scope=group\|item&prefix=` | tags in use with their counts, for autocomplete | 200 | `InvalidRequest` |
| `POST /tags/rename` | body `{scope, from, to}`; merges when `to` exists; for `scope: item` the response also lists `affected_commands` (ids and names of commands whose text contains `#from`) | 200 | `TagInvalid`, `TagNotFound` |

**Projects, commands**

| Method and path | Operation | Success | Notable errors |
|---|---|---|---|
| `GET/POST /projects`, `GET/PUT/DELETE /projects/{id}` | CRUD (delete cascades per §7.4) | | `ProjectNotFound` |
| `GET /projects/{id}/commands` | list, with `finished_jobs` / `total_jobs` | 200 | |
| `POST /projects/{id}/commands` | body `{name, description, code, insert_at?}` | 201 | `Command*` (§17.2), `ProjectNotFound` |
| `POST /commands/preview` | body `{code}` → §7.2 preview; validation problems are returned **in the 200 body** (`valid`, `errors[]` each with `rule`, `message`, `position`, `warnings[]`, counts, per-server distribution), never as an HTTP error | 200 | `InvalidRequest` only |
| `GET /commands/{id}` · `PUT` · `DELETE` | get / edit / delete | | `CommandNotFound`, `Command*` |
| `POST /commands/{id}/move` | body `{order}` | 200 | |
| `POST /projects/{id}/commands/renumber` | | 200 | |
| `POST /commands/{id}/run` · `/stop` · `/recreate` | §7.2 | 202 / 200 / 202 | `CommandNotRunnable` |
| `GET /commands/{id}/download` | ZIP of results + prompt summary | 200 (`application/zip`) | `CommandNotFinished` (409) |

**Jobs, REPL, events, files**

| Method and path | Operation | Success | Notable errors |
|---|---|---|---|
| `GET /commands/{id}/jobs` | list, paginated, filter `status=` | 200 | |
| `GET /jobs/{id}` | job with error and trace | 200 | `JobNotFound` |
| `PUT /jobs/{id}` | body `{positive_prompt, negative_prompt}`, the names the job itself uses (§8.4) | 200 | |
| `POST /jobs/{id}/run` · `/stop` | §8.4 | 202 / 200 | `JobNotRunnable` |
| `POST /repl/run` | body `{server, generator, positive, negative, items, loras:[{name,strength_model,strength_clip}]}`, `server` and `generator` being code names (§8.6) | 202 | `ReplInvalid`, `ServerNotFound`, `GeneratorNotFound`, `GroupNotFound`, `ItemNotFound`, `JobBusy` |
| `GET /repl/last` | previous REPL job or `404` | 200 | `JobNotFound` |
| `GET /events` | event log, see §15.4 | 200 | |
| `GET /files/{kind}/{name}` | read-only files; `kind` ∈ `results`, `thumbnails`, `controlnet`, `ipadapter`, `regions`, `downloads`. `name` is a relative path of plain segments (results and masks live in folders of their own, e.g. `results/12/fox.png`); empty segments, `.`, `..`, backslashes and control characters are rejected, and a folder is not a file | 200 | `FileNotFound` |
| `GET /healthz` (no `/api/v1` prefix) | liveness | 200 | |

### 15.3 Command text endpoints

`POST /commands/preview` and the validation inside `POST …/commands` and `PUT /commands/{id}` use the same parser and validator (§6). The error entries in `metadata.errors` are structured, so a client can underline the offending token:

```json
{"rule": "S1", "message": "&& and || cannot be mixed in one term", "position": {"offset": 34, "length": 2}}
```

`rule` is one of the identifiers of §6.3 (`S1`…`S8`, `T1`…`T5`, `R1`), `LEX` for lexical errors, `SYN` for grammar errors, or `SEM` for the semantic checks of §6.11.

### 15.4 Events

`GET /api/v1/events` serves the event log of §8.5.

- With `Accept: text/event-stream`: a stream of events; each carries `id: <event id>`, `event: <type>` (`processing`, `finished`, `error`, plus `server_online`, `server_offline`, `command_finished`) and a JSON `data` line `{job_id, command_id, project_id, server, created_at}`. A reconnecting client sends `Last-Event-ID` and receives the events after it. A comment line (`: keep-alive`) is sent every 15 s. A client that connects without a cursor receives only the events that happen from then on.
- Otherwise: JSON `{"events":[…],"last_id":N}` for clients that poll, honouring `?after=<id>`.
- If `after` / `Last-Event-ID` is older than the retained log, the server replies with the oldest retained events and sets `gap: true`, so the client knows to reload. On the stream the same condition is announced first by an `event: gap` with the data `{"gap":true}`.

### 15.5 Contract documentation

The server describes its registered routes as an OpenAPI 3.1 document at `/api/v1/openapi.json`; it is built from the route table itself, so a route cannot exist without being described. The same document is kept in the repository as `docs/openapi.json`, and a test fails when the two differ (`go test ./internal/api -update` rewrites the file).

### 15.6 Command-line client

`bosctl` is a second binary that drives a running server through §15. It is a client like any other: it does not open the database, import `internal/service` or read the server's configuration file.

| Topic | Rule |
|---|---|
| Coverage | Every operation in the OpenAPI document (§15.5) has a command, except those on an explicit exclusion list in the code (for example `/healthz`). A test fails when an operation has neither |
| Shape | `bosctl <resource> <verb> [arguments] [flags]`, e.g. `bosctl command run 12`, `bosctl job list --command 12 --status error`. `bosctl help` and `bosctl <resource> help` list commands |
| Connection | Flags `--server` and `--token`, then the environment variables `BOS_SERVER` and `BOS_TOKEN`, then `http://127.0.0.1:8080` and no token |
| Input | Bodies are built from flags; `--file` reads a JSON body from a path (`-` for standard input). Workflows and masks are read from file paths and uploaded |
| Output | A table for people, or the raw response body with `--json`. Lists honour `--page` and `--page-size`. Normal output goes to standard output, diagnostics to standard error |
| Streaming | `bosctl events --follow` prints events as they arrive and reconnects with `Last-Event-ID`; `bosctl command run --wait` returns when the command finishes |
| Errors | The error envelope of §15.1 is printed as `<Tag>: <message>` (the whole envelope with `--json`). Exit code 0 on success, 1 for an error returned by the server, 2 for a usage error, 3 when the server cannot be reached |
| Dependencies | Standard library only |

## 16. Web UI (Datastar)

### 16.1 Principles

1. **Hypermedia first.** The server renders HTML; the browser holds as little state as possible. Datastar attributes (`data-*`) declare behaviour in the markup; there is no JavaScript build step and no framework runtime beyond the Datastar script.
2. **The Datastar script is vendored.** A pinned Datastar v1 file is embedded in the binary and served from `/static/`, so the UI works offline and nothing is loaded from a CDN (in line with the privacy requirement).
3. **Server-sent events carry every update.** Handlers answer with `text/event-stream` and send:
   - `datastar-patch-elements` — `elements` (HTML), optional `selector`, `mode` (default morphs the outer HTML; also `inner`, `append`, `prepend`, `remove` …), to update the page;
   - `datastar-patch-signals` — a JSON object, to update client-side signals (set a field to `null` to remove it).
4. **Signals are the client state**: form fields, the open dialog, the current page number, filters. Signal names are namespaced per screen (e.g. `$servers.form.name`).
5. **Elements are addressed by stable `id`s** (`id="job-1234"`, `id="server-table"`), so a patch morphs the right element in place and keeps scroll and focus.
6. **No business logic in handlers or templates.** Handlers translate between signals/forms and service calls, then render.
7. **Both front ends share services.** The UI is not a client of the REST API (§14.3).

### 16.2 Routes

Full pages are plain `GET`s that return a complete HTML document. Everything interactive is under `/ui/`.

| Kind | Examples | Notes |
|---|---|---|
| Page | `GET /` · `/servers` · `/generators` · `/fixers` · `/groups` · `/groups/{id}/items` · `/loras` · `/projects` · `/projects/{id}/commands` · `/commands/{id}/jobs` · `/repl` | One per screen of §4; the common header menu is part of every page |
| Fragment (read) | `GET /ui/servers/table?page=2` · `GET /ui/groups/{id}/items/list` | Return an SSE response that patches the table/list; used for paging, filtering and refresh. Every table of 20-row pages (servers, generators, fixers, groups, items, projects, commands, jobs) has its pager **above and below** it, shown when there is more than one page: *First*, *Previous*, the page numbers (the first and the last page, the current one with two on each side, and a gap `…` where pages are left out), *Next*, *Last*, and "Page n of m · total in all" |
| Action (write) | `POST /ui/servers` (creates, or updates when the form carries an id) · `DELETE /ui/servers/{id}` · `POST /ui/commands/{id}/run` … | Read the signals (JSON body, `Datastar-Request: true`), call a service, patch the affected element(s) and close the dialog via a signal patch |
| Upload | `POST /ui/groups/{id}/items` | The form uses Datastar's `contentType: 'form'`, which sends `multipart/form-data` |
| Live stream | `GET /ui/stream/commands/{id}` · `GET /ui/stream/toasts` | Long-lived SSE connections (§16.4) |
| Picture list | `GET /ui/commands/{id}/jobs/pictures[?status=]` | JSON, not SSE: `[{url, label}]` for every finished image job of the command in job order, past the page size; the viewer on the job list loads it (the table says where in `data-pictures`) and asks again when it reaches the last picture. A status other than `finished` has none; an unknown status is 400 |
| Preview | `POST /ui/commands/preview` | Live feedback while typing (§16.3) |
| Settings | `GET /settings` · `POST /ui/settings` · `POST /ui/settings/restart` | The settings page, saving it, and restarting the program (§2) |
| LoRAs | `GET /ui/loras/table?q=&page=` · `GET /ui/loras/detail?name=&q=` · `DELETE /ui/loras?name=&q=` · `GET /ui/loras/suggest?q=` | The LoRAs page (§9.1): the table, the dialog of one LoRA, the deletion, and the item dialog's autocomplete |
| Completion | `POST /ui/commands/complete?pos=N` | Code names that fit at the caret of the command text area (§16.3); `N` is the caret in UTF-16 units |
| Static | `GET /static/…` | Embedded Datastar script, the scripts of the interface, CSS |

Read actions use `GET` (signals arrive in the `datastar` query parameter); writes use `POST`/`PUT`/`DELETE` (signals arrive as a JSON body). Route paths in the UI are separate from `/api/v1` and are not a public contract.

### 16.3 Screens

Each screen implements the capabilities of §4. UI-specific behaviour worth fixing in the spec:

| Screen | Behaviour |
|---|---|
| Servers | Table with a status badge updated live; add/edit dialog (it names the extension every server must run); "test connection" button |
| Generators | Paste or upload workflow JSON → the dialog calls the `analyze` service by itself (when the box loses focus after an edit, after a paste, and when a file is chosen; there is no button) and fills output type, attributes, output node and the prompt-node fields (the nodes titled `positive…` and `negative…`, without overwriting what was typed) and offers the discovered node titles as dropdowns for them; duplicate titles are flagged |
| Fixers | Same, with image-input and image-output node pickers |
| Groups | Table with thumbnail, tag chips (click to filter), feature-flag chips (no chip or checkbox for LoRAs: every item can carry them); tag input with autocomplete; dialogs for text-file import and Civitai import (progress shown through the event stream) |
| LoRAs | Search box (filters as you type) and a table of name, number of items and the servers that have it; a name opens a dialog with the servers (installed, not installed, or not asked) and the items that carry it, and a two-step *Delete everywhere* button; a note names the servers that could not be asked |
| Items | The LoRA section is always there: a table of the item's LoRAs (name, two strength fields, *Remove*) and a search box that suggests names as you type (arrows choose, Enter adds; a LoRA is added once). The other sections appear only for the flags the group enables; image previews from `/files/…`; mask-region upload shows the generated keywords immediately; item `tags` input with autocomplete; renaming an item tag shows the commands that mention it |
| Commands | The command text area sends its content, debounced, to `/ui/commands/preview` and patches a result panel: errors with the offending span highlighted, warnings, total jobs, jobs kept after `where`/`limit`/`sample`, and (for several servers) jobs per server. Add is enabled only when the preview is valid. While the user types, the text area also offers the **code names that fit at the caret**: servers (at the start and after `on`), the generator after `-$`, groups after `:`, `*`, `&&`, `||` (also in template and region bodies, in `where` and after `spread by`), the items of the group whose parenthesis the caret is in (and in `where group = …` and `group in (…)`), item tags after `#`, and fixers after `>`. Nothing is offered inside a string, in a pattern (`%`, `?`), where a free name is typed (template tags, region keywords) or where no name belongs. The list is sorted, holds at most 12 names, and is a service call (`CommandService.Complete`) that stores nothing and fails only when the store does. Tab takes the first name (or the chosen one), the arrow keys choose, Enter takes a name only after an arrow chose it, Esc closes the list. List shows progress `finished/total`, run/stop/recreate/download buttons, drag-or-button reordering |
| Jobs | Grid or table of jobs with status, assigned server, thumbnail of the result, seed; click to open prompts for editing; a click on a result picture opens it in a viewer over the same page, with previous and next buttons (also the arrow keys and a swipe) over **every picture of the command** (the whole collection, not the page on the screen; with the status filter that the table shows), and a modifier-click still opens it in a new tab; error and trace shown for failed jobs |
| Settings | Every setting of §2 in sections (folders, Civitai, server, jobs, log), with its default as the hint; an empty field is the default; a credential is a password field that says whether a value is set and has a *remove it* box; a note names the `BOS_…` variable that overrides a setting. *Save* checks all of them together and shows every problem; *Restart now* restarts the program and the page reloads when it is back |
| REPL | Form as in §8.6; result image replaces itself when the job finishes |

### 16.4 Live updates

- The page for a command opens `data-init="@get('/ui/stream/commands/{id}')"`. The handler subscribes to the event log (§8.5), filters by command, and for each event re-renders only the affected job row and the progress header, sending `datastar-patch-elements` for `#job-<id>` and `#command-progress`.
- `GET /ui/stream/toasts` is a global stream for the header: it renders a transient toast per `processing` / `finished` / `error` event. This replaces polling of the original "latest notification".
- Streams honour `Last-Event-ID` after a dropped connection and end when the request context is cancelled. A keep-alive comment is sent every 15 s.
- Events are coalesced per element (at most one patch per job per 250 ms) so a fast batch does not flood the browser.

### 16.5 Forms and errors

- A validation or business failure in a UI action returns **200** with a patch into the form's error container (message derived from the error `Tag` and metadata; field-level where the metadata names the field). The dialog stays open. Datastar's default behaviour on HTTP errors is not relied on for user-visible messages.
- Unexpected failures (`500`) patch a generic toast; the detailed error goes to the log.
- Destructive actions (delete group, project, command) ask for confirmation in-page before sending the request.
- Empty states say what to do next ("No servers yet — add one").
- The generator and fixer dialogs carry no HTML `required` attributes: Datastar does not send a form the browser holds invalid, and the workflow analysis of §16.3 is sent before the name is typed. The services validate every field (`NameInvalid`, `FixerTitleRequired`, `GeneratorTitleRequired`, …) and the answer lands in the form's error container.

### 16.6 Security

- The server listens on `127.0.0.1:<port>` by default; exposing it on another address requires explicit configuration, and a warning is logged if `auth_token` is empty.
- If `auth_token` is set, the UI shows a sign-in form once, stores a signed cookie (`HttpOnly`, `SameSite=Strict`), and both the UI and the REST API honour the token.
- **CSRF.** All state-changing `/ui/` routes require the header `Datastar-Request: true` and a same-origin `Origin`/`Sec-Fetch-Site`; others are rejected with `403`. Cookies are `SameSite=Strict`.
- Response header `Content-Security-Policy` allows scripts only from `'self'` (plus `'unsafe-eval'`, because Datastar compiles the expressions of its attributes with the `Function` constructor; no inline script and no remote script is allowed), and nothing else from outside: `default-src 'none'`, styles, images and connections from `'self'`, `frame-ancestors 'none'`; user-supplied text (prompts, names) is always HTML-escaped by `html/template`. Datastar attribute values that interpolate user data are escaped for attribute context; user text is never placed inside an expression.
- **No caching.** Nothing the interface sends is kept by the browser: pages, fragments, the static scripts and styles, and the files under `/files` all carry `Cache-Control: no-store`. A result keeps its name when its job runs again (and the REPL result always has the same one), and a kept style sheet next to a new script breaks the page.
- File routes serve only from the configured directories, a relative path of plain segments (§15.2), with `Content-Type` fixed by extension and `X-Content-Type-Options: nosniff`.

## 17. Error handling (`github.com/rm4n0s/errors`)

### 17.1 Rules

These repeat the project rules (see `CLAUDE.md`) because they shape the API:

1. Every error is produced with `errors.New`, `errors.NewErr` or `errors.NewErrf`; `fmt.Errorf` and the standard `errors` package are not imported. The package re-exports `Is`, `As` and `Unwrap`.
2. **Library note:** the current constructors are `New`, `NewErr` and `NewErrf`; the older `NewFromErr` no longer exists. `gocivitailib` will use the standard library `errors` instead (§19.1), and its errors are wrapped at the adapter boundary (§19.4). **New failure with a fixed message:** `errors.New("Tag", "message", "key", value, …)`. **Wrapping a foreign error** (database driver, `comfyui-client-go`, `gocivitailib`, stdlib): `errors.NewErr("Tag", err)`, adding metadata with `.SetMetadata("key", value, …)`. `SetMetadata` replaces the metadata map, so set it once with all pairs.
3. An error that is already an `*errors.Error` is returned **untouched** (`return err`); re-wrapping would replace the tag and the stack and break route tests. `errors.FromError` is a plain type assertion, so nothing may wrap these errors.
4. Functions return `error`, never `*errors.Error`.
5. Every new failure case has its **own unique PascalCase tag** and a test that asserts the route (§20).
6. Method names are part of the testable contract: a route is `Type.Method->Type.Method.Tag` (outermost to innermost). Renaming a service method changes routes and their tests.

### 17.2 Tag catalogue and HTTP mapping

`internal/api` owns one table `tag → HTTP status` (default 500). A test fails if a tag in the catalogue has no entry. The catalogue below is the initial set; each new failure case adds its own tag and entry.

| Group | Tags | HTTP |
|---|---|---|
| Request | `InvalidRequest` (malformed body, unknown field, bad id), `UploadTooLarge` | 400, 413 |
| Not found | `ServerNotFound`, `GeneratorNotFound`, `FixerNotFound`, `GroupNotFound`, `ItemNotFound`, `ProjectNotFound`, `CommandNotFound`, `JobNotFound`, `FileNotFound`, `LoRANotFound` | 404 |
| Naming | `CodeNameInvalid`, `CodeNameReserved`, `TagInvalid` | 422 |
| Settings | `ConfigInvalid` (metadata `problems`) | 422 (at startup: abort) |
| Generators | `GeneratorTitleRequired` (metadata `candidates`) | 422 |
| Tags | `TagNotFound` | 404 |
| Uniqueness | `ServerCodeNameTaken`, `GeneratorCodeNameTaken`, `FixerCodeNameTaken`, `GroupCodeNameTaken`, `ItemCodeNameTaken` | 409 |
| Command language | `CommandLexError`, `CommandSyntaxError`, `CommandRestrictionViolated` (metadata `rule`), `CommandValidationFailed` (metadata `errors`), `CommandSelectionEmpty`, `CommandWhereEmpty` | 422 |
| State | `CommandNotRunnable`, `CommandNotFinished`, `JobNotRunnable`, `ReplInvalid` | 409, 422 |
| Workflow | `WorkflowInvalid`, `WorkflowTitleDuplicated`, `WorkflowTitleNotFound`, `OutputNodeNotFound`, `NoSamplerInWorkflow`, `InjectorFailed`, `RegionInvalid`, `MaskGenerationFailed` | 422 (on create/analyze) or recorded on the job |
| ComfyUI | `ServerUnreachable`, `ComfyUIRejected` (node errors), `ComfyUIExecutionFailed` (traceback in metadata), `ComfyUIUploadFailed`, `ComfyUIResultMissing`, `ModelCrudMissing` (no `/model_crud` routes: the extension is not installed), `ModelCrudDisabled` (the extension forbids uploads or deletes), `ModelCrudRejected` (any other error code of the extension, in metadata `code`), `LoRADeleteFailed` (a LoRA could not be deleted from every server; metadata `servers`, `deleted_from`) | 502 / 503, or recorded on the job |
| Civitai | `CivitaiNotConfigured`, `CivitaiModelNotFound`, `CivitaiRateLimited`, `CivitaiDownloadFailed`, `CivitaiMetadataInvalid` | 409, 404, 503, 502, 502 |
| Infrastructure | `StoreFailure`, `MigrationFailed`, `FileStoreFailure` | 500 (startup: abort) |

Errors that happen **inside a job** are not HTTP responses; the scheduler stores the tag, message and the `ToJson()` trace on the job (`error`, `traceback`) and emits an `error` event.

### 17.3 Where errors are created

- `internal/command` returns `CommandLexError` / `CommandSyntaxError` / `CommandRestrictionViolated` with `position` and `rule` in metadata; the validator in `service` aggregates semantic problems into **one** `CommandValidationFailed` whose metadata lists all of them (§6.11: errors are collected, not first-fail).
- `internal/store` wraps driver errors with `NewErr("StoreFailure", err)` and translates unique-constraint violations into the matching `*CodeNameTaken` tag with `errors.New`.
- Adapters in `internal/comfy` and `internal/civitai` wrap library errors (which use `fmt.Errorf`) with `NewErr`, so no foreign error crosses a port.

### 17.4 Logging

Errors are logged once, at the boundary that handles them (HTTP middleware, scheduler runner), with `log/slog`: the message, and the result of `appErr.ToJson()` as a structured attribute so the failure can be replayed in a test with `errors.FromJson`. An error is never logged and then returned again.

## 18. ComfyUI integration (`comfyui-client-go`)

### 18.1 What the library provides (verified against the repository, commit `26c3013`)

Everything is in the root package; the only dependency is `gorilla/websocket`; a `Client` is safe for concurrent use; the library uses plain standard-library errors (wrapped with `%w`).

| Area | API |
|---|---|
| Client | `NewClient(baseURL, opts…)`; options `WithClientID`, `WithTimeout` (time to response headers, default 60 s, **no overall timeout**), `WithHTTPClient`, `WithoutWebSocket` (HTTP-only client); `ClientID()`, `Close()` |
| Events | `Listen(ctx) <-chan *WebSocketMessage`, `Done()`, `Err()`; `(*WebSocketMessage).Decode()` returns typed values (`StatusData`, `ExecutionStartData`, `ExecutionCachedData`, `ExecutingData`, `ProgressData`, `ExecutedData`, `ExecutionSuccessData`, `ExecutionErrorData`, `ExecutionInterruptedData`, `[]byte` for binary previews, `json.RawMessage` for unknown types) |
| Run | `QueuePrompt(ctx, map[string]json.RawMessage) (promptID, error)`; `GetHistory(ctx, promptID)`; `ViewImage(ctx, ViewParams)` (streams any output file); `UnloadModels(ctx)` |
| Outputs | `NodeOutput.Images` plus `Other` (every other key, verbatim); `NodeOutput.Files()` lists every `{key, filename, subfolder, type}` found under any key; `OutputFile.ViewParams()` |
| Files | `UploadImage(ctx, name, io.Reader, UploadOptions{Type, Subfolder, Overwrite}) (UploadResult, error)`; `UploadResult.InputValue()` is the string to put in a `LoadImage` input |
| Models | `ListModels(ctx, folder)` (`loras`, `checkpoints`, …); `ObjectInfo(ctx, nodeClass)` |
| Queue | `Queue(ctx)` → `QueueState{Running, Pending}` (ids, pending in execution order); `DeleteQueued(ctx, ids…)`; `Interrupt(ctx, promptID)` (empty id = whatever is running) |
| Health | `Ping(ctx)`, `SystemStats(ctx)` (version, RAM, devices with VRAM) |
| Errors | `*StatusError{Status, Body}`; `*NodeErrorsError{PromptID, NodeErrors, Message}`; `ErrPromptNotFound` (history entry not there yet), `ErrNodeClassNotFound`, `ErrClosed`, `ErrNoWebSocket` |
| Workflow editing | Deterministic (nodes ordered by id, numeric ids first): `FindNodeIDsByTitle`, `FindNodeIDByTitle`, `DuplicateTitles`, `EditWorkflowByTitle`, `EditWorkflowByNodeID`, `EditWorkflowByClassType`, `EditInputsForClassType` (every node of a class), `MaxNumericNodeID`, `GetTitlesFromClassType[Containing]`, `TitleWithClassTypeExists`. Edits preserve other node fields, including integers too large for `float64` (64-bit seeds) |

### 18.2 Port used by the application

The scheduler and services depend on this interface, not on the library:

```go
package comfy

type Client interface {
    Ping(ctx context.Context) error
    QueuePrompt(ctx context.Context, wf workflow.Graph) (promptID string, warnings []NodeWarning, err error)
    Wait(ctx context.Context, promptID string) (Result, error) // success / failure / interrupt
    Fetch(ctx context.Context, f OutputFile) (io.ReadCloser, error)
    Upload(ctx context.Context, name string, r io.Reader) (inputValue string, err error)
    ListModels(ctx context.Context, folder string) ([]string, error)
    Cancel(ctx context.Context, promptID string) error // DeleteQueued if pending, Interrupt if running
    DeleteOutput(ctx context.Context, f OutputFile) error // §18.8; files of type "output" only
    UploadModel(ctx context.Context, folder, subfolder, filename string, size int64, r io.Reader) (path string, err error) // §18.8
    DeleteModel(ctx context.Context, folder, path string) error // §18.8; a missing file is not an error
    Close() error
}
```

`workflow.Graph` is `map[string]json.RawMessage`. `Result` carries the output files (images, video, 3D) and, on failure, the node, exception type and traceback. Failures are returned as `ComfyUIRejected`, `ComfyUIExecutionFailed` or `ServerUnreachable` errors (§17.2).

### 18.3 Event hub

ComfyUI sends the events of a prompt **only to the client id that queued it**, over **one WebSocket per client id**. Connecting a second client with the same id silently takes the events from the first. The library offers one reader goroutine per client (a second `Listen` while a stream is active returns the same channel). Therefore `internal/comfy` runs one **hub** per server connection:

1. One `Client` per server, with its own random client id. The hub is the **only** consumer of `Listen`; it routes every message by `prompt_id` to the waiter registered for that prompt.
2. A prompt's id is known only after `QueuePrompt` returns, so events can arrive before a waiter exists. The hub keeps a short buffer of recent events per prompt; `Wait` first checks `GetHistory` (completed already? `ErrPromptNotFound` means not yet) and then reads buffered and live events.
3. `execution_success` ends the wait with the history outputs; `execution_error` and `execution_interrupted` end it with the matching error. `status` events with `queue_remaining == 0` are **not** completion (the original used them; they are unreliable with concurrent prompts).
4. **Connection loss.** The library does not reconnect: when `Done()` closes, all waiters fail with `ServerUnreachable`, the server is marked unavailable, and the hub creates a **new** `Client` with back-off until `Ping` succeeds. The library detects a silently dead connection by pinging every 20 s and timing out after 60 s, so loss detection can take up to a minute; the health probe (§8.1) usually notices sooner.
5. **Recovering prompts after a reconnect.** A new client has a new id and will not receive events for prompts the old client queued. For a job whose `comfyui_prompt_id` is known, recovery therefore uses polling: `GetHistory` (finished → fetch outputs) and `Queue` (still running or pending → keep waiting by polling). Only if the prompt is in neither is the job returned to `queued` (§8.7).

### 18.4 Library coverage of the application's needs

All of the following were requested for this specification and are present in the library.

| # | Need | Provided by | What the adapter still does |
|---|---|---|---|
| G1 | Upload an image | `UploadImage`, `UploadResult.InputValue()` | Name and place files (§18.6) and put `InputValue()` in the `LoadImage` input |
| G2 | List installed models | `ListModels(ctx, "loras")`, `ObjectInfo` | Compare against the LoRAs a command uses (§6.11 warning, §15.2 `/servers/{id}/loras`) |
| G3 | Interrupt / remove from queue | `Interrupt`, `DeleteQueued`, `Queue` | See §18.7 on interrupt semantics |
| G4 | Cheap health probe | `Ping` on a `WithoutWebSocket` client | One probe client per server, every second (§8.1) |
| G5 | Non-image outputs | `NodeOutput.Files()` | Map the generator's output type (§3.2) to the files to keep |
| G6 | Safe event reading | `Listen(ctx)`, `Done()`, `Err()` | The hub of §18.3 |
| G7 | Configurable HTTP timeout | `WithTimeout`, `WithHTTPClient`; no overall timeout | Bound each call with a context |
| G8 | Deterministic editing | `FindNodeIDsByTitle`, `EditWorkflowByNodeID`, `EditInputsForClassType`, `DuplicateTitles`, `MaxNumericNodeID` | §18.5 |

### 18.5 Workflow editing

- `internal/workflow` owns all graph changes (§8.2): attribute and seed edits, LoRA, ControlNet, IP-Adapter and region injection, output analysis. It uses the library's edit helpers for simple input changes and decodes nodes itself for structural injections (adding nodes, rewiring links), always on a per-job deep copy of the stored graph.
- Seeds are written as `uint64`; the library preserves integers beyond `float64` precision.
- **Titles must be unique** for the nodes the application addresses (prompt nodes, a fixer's image-input node). Creating or editing a generator or fixer whose workflow has duplicates is rejected with `WorkflowTitleDuplicated`, using `DuplicateTitles`. Note that the library matches a title against a node's `_meta.title` **or** its `class_type`, so a title equal to some node's class name matches that node too; the uniqueness check must therefore consider both.
- New node ids are allocated from `MaxNumericNodeID + 1`; non-numeric ids are ignored when computing it.
- A sampler seed is set on **every** `KSampler` with `EditInputsForClassType`, and the number of nodes edited is checked (zero → `NoSamplerInWorkflow`).

### 18.6 Input files

Every file a workflow reads is made available to the target server before submission (§8.7). **Decision:** upload for every server (`UploadImage`) and use `UploadResult.InputValue()` as the `LoadImage` input. Files are named by the **SHA-256 of their content** plus extension and placed in an application sub-folder (for example `bos/<hash>.png`): ComfyUI reuses a file whose content is identical, so a reference image shared by many jobs is stored once per server, and uploads never collide. The returned name, not the requested one, is always used. ComfyUI has no delete call, so uploaded inputs accumulate on the server; this is documented to the user (the extension of §18.8 could delete them; the program does not use that yet). Passing an absolute path is permitted only for servers marked `is_local`, because it depends on how that ComfyUI build resolves `LoadImage` paths.

### 18.7 Behaviours the adapter must handle

| Situation | Handling |
|---|---|
| `QueuePrompt` returns a `*NodeErrorsError` **and** a non-empty prompt id | ComfyUI accepted the prompt but will skip some outputs (HTTP 200). The prompt **is queued**: keep the id, record the node errors as job warnings, and continue. If the prompt id is empty the prompt was rejected → `ComfyUIRejected` |
| `Interrupt(promptID)` | Targets one prompt only on ComfyUI ≥ 0.3.57; the library checks `Queue` first and sends nothing unless the prompt is running, but that check is not atomic, so on older servers a prompt that starts in between can be interrupted. `Cancel` therefore prefers `DeleteQueued` for pending prompts and warns once per server if `SystemStats` reports an older version |
| `GetHistory` → `ErrPromptNotFound` | Not an error: the prompt has not finished (or was evicted from history) |
| Application restart with jobs in `processing` | If the job's prompt id is still in history, collect its outputs instead of re-running; otherwise return the job to `queued` (acceptance test 28) |
| `Listen` stream ends | Distinguish context cancellation (normal shutdown) from a dropped connection with `Err()` |


### 18.8 The comfyui-model-crud extension

ComfyUI can neither delete a file nor receive a model, so every server of this program runs the custom node [comfyui-model-crud](https://github.com/rm4n0s/comfyui-model-crud) (contract: its `openapi.yaml`, version 0.1.0). `internal/modelcrud` is its client, over plain `net/http`; `comfy.Server` owns one per server and exposes it as `DeleteOutput`, `UploadModel` and `DeleteModel` of the port (§18.2). No other part of the program talks to it.

| Use | Calls |
|---|---|
| Delete a result (§8.2) | `DELETE /model_crud/output?path=<subfolder>/<filename>`; `file_not_found` is success |
| Delete a model (§9.1) | `DELETE /model_crud/models/{folder}?path=<path relative to the folder>`; `file_not_found` is success |
| Upload a model (§9) | `POST /model_crud/uploads` `{folder, filename, size, subfolder?, overwrite}` → `{upload_id, path, chunk_size, total_chunks}`; `PUT /model_crud/uploads/{id}/chunks/{i}` with the raw bytes of chunk `i`, one after the other; `POST /model_crud/uploads/{id}/complete` → `{folder, path, size}` |

- An upload starts with `overwrite: false`. On `file_exists` the client lists `GET /model_crud/models/{folder}`: a file of the same path and size is taken as already uploaded (nothing is sent), another size is replaced with a second session that sets `overwrite: true`.
- A chunk that gets no answer, or `429`/`5xx`, is sent again (4 attempts, with back-off); other answers fail at once. A failed upload is cancelled with `DELETE /model_crud/uploads/{id}`, because the server keeps the disk space of an open session for a day.
- Errors carry `{error, message}`. A `404` without that body means that the routes do not exist: `ModelCrudMissing`. `uploads_disabled` and `delete_disabled` are `ModelCrudDisabled`. Any other code is `ModelCrudRejected` with the code in the metadata. No answer, or a proxy's `502`/`503`/`504`, is `ServerUnreachable`; a cancelled context is `ComfyUICallCancelled`.
- The extension has no authentication. Keep ComfyUI on a network you trust (the extension itself can be made read-only with `MODEL_CRUD_READ_ONLY=1`, which makes `ModelCrudDisabled` of every delete and upload).

## 19. Civitai integration (`gocivitailib`)

### 19.1 What the library provides (verified against the repository, commit `c1e9d5a`)

Standard library only (tests use testify); errors are standard-library errors wrapped with `%w`.

| Area | API |
|---|---|
| Client | `NewClient(opts…)`; options `WithToken`, `WithDomain` (default `civitai.com`; derives `https://civitai.com` and `https://orchestration.civitai.com`), `WithSiteURL`, `WithOrchestrationURL`, `WithHttpClient` (redirects never forward the token to another host), `WithLogger` (method, path, status, duration only), `WithMaxRetries` (default 3, GET only), `WithRetryBackoff`. Reads `CIVITAI_TOKEN` and `CIVITAI_DOMAIN` from the environment unless options say otherwise. Default HTTP client has dial/TLS/header timeouts and no overall timeout |
| Models | `GetModel(ctx, id) (*ModelInfo, error)` (public endpoint); `DownloadModelVersion(ctx, versionID, io.Writer)`; `DownloadFile(ctx, url, io.Writer)`; `DownloadModel(ctx, modelID, dir, opts…) (*DownloadedModel, error)` — picks the newest version (or `WithVersionID`) and its primary `.safetensors` file, keeps an existing file of the expected size, streams to a temporary file, verifies SHA-256 when known, renames into place, writes `<dir>/<modelID>.json` (`ModelMetadata`: `name`, `baseModel`, `triggerWords`, `tags`, `description`, `versionId`), and removes temporary files on every failure |
| Generation | `SubmitImageWorkflow(ctx, ImageWorkflowRequest) (*ImageWorkflowResponse, status, error)` (never retried, because it spends Buzz; returns 200 finished / 202 still running); `GetImageWorkflow(ctx, id)`; `IsTerminalStatus`; `ImageGenInput.Validate()` (engine/ecosystem field rules, size multiples of 16, quantity ranges) |
| Errors | Sentinels `ErrMissingToken`, `ErrNoDownloadableFile`, `ErrInvalidResponse`, `ErrHashMismatch`; typed `*StatusError{Status, Body}`, `*RateLimitedError{RetryAfter}` (also matches `*StatusError`), `*ValidationError{Field, Rule, Message}` with rules `required`, `not_allowed`, `multiple_of_16`, `out_of_range` |

A token is required for every model **file** download and for the generation API; model metadata is public. The earlier defects of this library (use of the removed `NewFromErr`, a submit call that could never succeed, ignored HTTP statuses, debug output, missing defaults, an always-failing test) are fixed in this version; the dependency on `rm4n0s/errors` was dropped, so the project builds against a single `errors` version.

### 19.2 Status of the requested changes

| # | Item | Status |
|---|---|---|
| C1 | Stop using the removed `NewFromErr`; standard-library errors | Done |
| C2 | Submit decodes into a value | Done |
| C3 | Typed errors for non-2xx responses | Done (`*StatusError`) |
| C4 | No debug output; optional logger | Done (`WithLogger`) |
| C5 | Default domain, timeouts, missing-token error | Done |
| C6 | Unit tests with `httptest`; integration test only with a token | Done |
| C7 | Model metadata | Done (`GetModel`) |
| C8 | Streaming download | Done (`DownloadModelVersion`, `DownloadFile`, `DownloadModel`) |
| C9 | Rate limiting and retries | Done (`WithMaxRetries`, `*RateLimitedError`) |
| C10 | Separate site and orchestration base URLs | Done (`WithSiteURL`, `WithOrchestrationURL`) |

### 19.3 Port used by the application

```go
package civitai

type Models interface {
    // Download stores <dir>/<modelID>.safetensors and <dir>/<modelID>.json for
    // the newest version; with a versionID (not 0) it stores that version below
    // <dir>/versions/<versionID> instead.
    Download(ctx context.Context, modelID, versionID int, dir string) (Model, error)
}

type Model struct {
    ID           int
    VersionID    int // the version downloaded
    Requested    int // the version asked for, 0 for the newest
    Name         string
    BaseModel    string
    TriggerWords []string
    Tags         []string
    Description  string
    FilePath     string
    MetadataPath string
    Size         int64
    Skipped      bool // already present, nothing downloaded
}
```

The adapter calls `gocivitailib.DownloadModel` (with `WithVersionID` when a version is asked for) and maps its `DownloadedModel` (`Info`, `Version`, `File`, `ModelPath`, `MetadataPath`, `Size`, `Skipped`) into `Model`.

### 19.4 Requirements on the adapter

From §9:

- `CivitaiNotConfigured` when the token or the cache directory is not configured (the library would return `ErrMissingToken`; the adapter checks first so nothing is attempted).
- Pass the configured token and domain to `NewClient` explicitly; do not rely on environment variables.
- Pause between consecutive downloads in a batch (configurable, default 15 s, as in the original). The library's retries handle `429` within a download; the pause is the application's politeness.
- Uploading the model to a ComfyUI server's `loras/<subfolder>/` folder (§9) goes through `comfy.Client.UploadModel` (§18.8); `internal/civitai` only opens the downloaded file, checks that the sub-folder is a plain relative path (otherwise `InvalidRequest`, checked before anything is downloaded) and hands the bytes over. A server that does not exist or is not online is refused before anything is downloaded (`ServerNotFound`, `ServerUnreachable`).
- The LoRA item is built from `Model`: `code_name` = model id (`<model id>_<version id>` for a chosen version), `name` = model name, positive prompt = trigger words separated by blank lines, one LoRA entry `<subfolder>/<id>.safetensors` (`<subfolder>/<id>-<version id>.safetensors` for a chosen version) with the chosen strengths.
- Error mapping, using `errors.Is` / `errors.As` on the library's errors and wrapping with `errors.NewErr`:

| Library error | Tag | Metadata |
|---|---|---|
| `ErrMissingToken` | `CivitaiNotConfigured` | |
| `*StatusError` with status 404 | `CivitaiModelNotFound` | `model_id` |
| `*RateLimitedError` | `CivitaiRateLimited` | `retry_after` |
| other `*StatusError`, `ErrHashMismatch`, `ErrNoDownloadableFile` | `CivitaiDownloadFailed` | `status`, `reason` |
| `ErrInvalidResponse` | `CivitaiMetadataInvalid` | |

### 19.5 Proposed, not committed: Civitai as a generation backend

Because the library already speaks Civitai's generation API (with request validation and status helpers), a command could target Civitai's cloud instead of, or next to, a ComfyUI server, reusing groups, items, `where`, `sample`, `repeat` and result handling. This would need:

- a server `kind` (`comfyui` | `civitai`) with the token as its credential;
- a second kind of generator described by an `ImageGenInput` template instead of a ComfyUI workflow;
- mapping of items' LoRAs to AIR URNs;
- cost awareness (Civitai charges Buzz): a dry-run estimate in the preview. The library currently sends `whatif=false` only, so a `whatif` variant would have to be added;
- polling of `GetImageWorkflow` and download of the signed, expiring image URLs.

It is **not** part of this version of the specification; it is recorded so the port above can grow a `Generator` interface without reshaping the rest. A decision is needed before it is specified.

## 20. Testing

All of this runs with `go test -race ./...`; tests live next to the code and are table-driven.

| Layer | Approach |
|---|---|
| `internal/command` | Table tests driven by the tables of §6.12 (valid: AST and job count; invalid: rule id and token position). Go native **fuzz tests** for the lexer/parser (must never panic; valid parse → format → parse is stable). Property tests for expansion: count equals the formula of §6.4/§6.7; order is deterministic |
| `internal/workflow` | Golden-file tests on the example workflows: each injector's output compared to a stored graph; checkpoint-style and split-style LoRA cases; seed handling; duplicate-title rejection |
| `internal/store` | Against in-memory SQLite: CRUD, cascades (§7.4), unique constraints, atomic job claiming under concurrent goroutines (`-race`) |
| `internal/service` | With fake store, fake `comfy.Client`, fake `civitai.Models`, fake clock: preview vs add parity; recreate reproduces `sample` selection; state transitions of commands and jobs |
| `internal/scheduler` | Fake clients with controllable latency and failures: `dynamic` beats `even` with unequal servers; static modes assign as §6.9; server loss → retry elsewhere after the grace period; `pinned` waits; workflow failure halts queued jobs but not running ones; `max_concurrent_jobs` respected; events ordered |
| `internal/comfy` | An `httptest` server that implements the ComfyUI endpoints used, with a WebSocket endpoint: hub routing, buffering of early events, reconnect |
| `internal/api` | `httptest` round trips for every route of §15.2: status codes, envelope, pagination, uploads, unknown-field rejection, tag→status table completeness, OpenAPI coverage |
| `internal/ui` | `httptest` against handlers: assert the SSE events (`datastar-patch-elements`/`-signals`), CSRF rejection without the header, escaping of hostile text, live stream resumes from `Last-Event-ID` |
| Errors | For **every** tag in §17.2 a test asserts the route, e.g. `appErr.HasRoute("CommandService.Add->Validator.Validate.CommandValidationFailed")`, and the HTTP mapping |

Definition of done for any change (project rule): `gofmt`, `go vet ./...`, `golangci-lint run`, `go test -race ./...` all pass, and any skipped step is reported.

Additional acceptance tests for this Part:

26. Every route of §15.2 exists, returns the documented success code, and returns the error envelope of §15.1 for each listed tag.
27. `POST /commands/preview` for an invalid command returns `200` with `valid:false` and structured errors; the same command posted to create a command returns `422` with tag `CommandValidationFailed`.
28. Stopping the server mid-batch (`SIGTERM`) finishes in-flight HTTP requests, stops the workers, and leaves no job in `processing` after the next start: a job whose prompt id is still known to ComfyUI has its outputs collected, any other is returned to `queued` (§18.7).
29. Two browsers on the same command page receive the same job updates; a reconnect with `Last-Event-ID` receives only the missed events.
30. A state-changing `/ui/` request without `Datastar-Request: true`, or from another origin, is rejected with `403`.
31. A prompt containing `<script>` is rendered inert in every UI surface (tables, previews, toasts, stream patches).
32. A generator workflow with two nodes titled `Positive` is rejected with `WorkflowTitleDuplicated`.
33. Concurrent `dynamic` runners never execute the same job twice (stress test with `-race`).
34. After a dropped ComfyUI connection the hub reconnects with a new client, in-flight jobs that finished meanwhile are completed from history without re-running, and jobs still running are awaited by polling.
35. A `QueuePrompt` that returns node errors together with a prompt id leaves the job running with warnings recorded; one that returns node errors without an id fails the job with `ComfyUIRejected`.
36. The same reference image used by 100 jobs is uploaded once per server (content-hash name), and every job's `LoadImage` input uses the returned `InputValue()`.
37. `civitai.Models.Download` maps each library error to its tag of §19.4; a second download of the same model reports `Skipped` and transfers nothing.
38. Tags are lower-cased on save and invalid ones (`Bad Tag!`) are rejected with `TagInvalid`; duplicates collapse; `#Combat` in a command matches an item tagged `combat`.
39. `GET /tags?scope=item&prefix=co` lists only item tags starting with `co`, with counts; group tags are not included.
40. Renaming an item tag to an existing tag merges them on every item and returns the commands whose text mentions the old tag; the commands themselves are unchanged.
41. `GET /groups?tag=character` returns only groups carrying that tag; a command can never select by a group tag.
