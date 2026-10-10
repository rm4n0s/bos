package cli

// kind is the type of a flag that becomes a field of the request.
type kind int

const (
	kString kind = iota
	kInt
	kFloat
	kBool
	// kList is repeated or comma-separated text and becomes a JSON array.
	kList
	// kJSON is JSON given inline or as @file.
	kJSON
	// kText is text given inline or as @file.
	kText
)

// field is a flag that becomes a query parameter or a field of the JSON body.
type field struct {
	Flag string // --code-name
	Key  string // code_name
	Kind kind
	Help string
}

// filePart is a flag that names a file to upload in a multipart request.
type filePart struct {
	Flag string
	Part string
	Help string
	// Indexed parts are given as N=path and named Part_N (controlnet_0).
	Indexed bool
}

// column is one column of a table.
type column struct {
	Header string
	Key    string // a dotted path into the object
	Max    int    // longest text shown; 0 means no limit
}

// result says what a command prints.
type result int

const (
	resultList   result = iota // a page of items as a table
	resultOne                  // one resource
	resultNone                 // nothing (204)
	resultBytes                // a file, saved with --output
	resultStream               // server-sent events
	resultDoc                  // the body as it is
)

// command is one operation of the REST API.
type command struct {
	Resource string
	Verb     string
	Summary  string
	Method   string
	Path     string // with {name} placeholders, relative to /api/v1
	// Args are the positional arguments, in order, one for each placeholder.
	Args []string
	// Query and Body are the flags that fill the query string and the JSON body.
	Query []field
	Body  []field
	Files []filePart
	// Merge makes an update start from the current resource, so that only the
	// flags given change (a PUT replaces everything).
	Merge bool
	// Required are the body keys that must be given when creating.
	Required []string
	Result   result
	Columns  []column
	// Paged commands accept --page and --page-size.
	Paged bool
	// DataFile commands accept --file, a JSON body.
	DataFile bool
	// Special names behaviour beyond a request: "wait" (command run), "follow"
	// (events), "code" (the command text may be an argument).
	Special string
}

// groupFields are the editable fields of a group.
var groupFields = []field{
	{"name", "name", kString, "the name"},
	{"code-name", "code_name", kString, "the code name used in commands"},
	{"description", "description", kString, "a description"},
	{"tags", "tags", kList, "group tags, separated by commas"},
	{"use-lora", "use_lora", kBool, "its items can carry LoRAs"},
	{"use-controlnet", "use_controlnet", kBool, "its items can carry ControlNets"},
	{"use-ip-adapter", "use_ip_adapter", kBool, "its items can carry an IP-Adapter"},
	{"use-mask-region", "use_mask_region", kBool, "its items can carry mask regions"},
	{"use-coordinates-region", "use_coordinates_region", kBool, "its items can carry box regions"},
	{"use-type-attributes", "use_type_attributes", kBool, "its items can set output attributes"},
}

var itemFields = []field{
	{"code-name", "code_name", kString, "the code name used in commands"},
	{"name", "name", kString, "the name"},
	{"positive-prompt", "positive_prompt", kText, "the positive prompt (or @file)"},
	{"negative-prompt", "negative_prompt", kText, "the negative prompt (or @file)"},
	{"tags", "tags", kList, "item tags, separated by commas"},
	{"loras", "loras", kList, "LoRAs as name:model:clip, repeatable"},
	{"ip-adapter", "ip_adapter", kJSON, "the IP-Adapter as JSON (or @file)"},
	{"controlnets", "controlnets", kJSON, "the ControlNets as a JSON list (or @file)"},
	{"mask-region", "mask_region_images", kJSON, "the mask region as JSON (or @file)"},
	{"coordinated-regions", "coordinated_regions", kJSON, "the box regions as a JSON list (or @file)"},
	{"output-type", "output_type", kString, "image, video, 3d or unknown"},
	{"output-attributes", "output_type_attributes", kJSON, "output attributes as JSON (or @file)"},
}

var itemFiles = []filePart{
	{Flag: "thumbnail", Part: "thumbnail", Help: "a thumbnail image"},
	{Flag: "ip-adapter-image", Part: "ip_adapter_image", Help: "the IP-Adapter reference image"},
	{Flag: "mask-reference", Part: "mask_reference", Help: "the PNG that the mask regions are made from"},
	{Flag: "controlnet-image", Part: "controlnet", Indexed: true, Help: "the image of a ControlNet, as N=path (repeatable)"},
}

var civitaiFields = []field{
	{"comfyui-path", "comfyui_path", kString, "the ComfyUI installation folder"},
	{"lora-subfolder", "lora_subfolder", kString, "the sub-folder of models/loras"},
}

// commands is the table of everything bosctl can do. A test fails when an
// operation of the API is neither here nor on the list of exclusions.
var commands = []command{
	// servers
	{Resource: "server", Verb: "list", Summary: "List servers with their status", Method: "GET", Path: "/servers", Paged: true, Result: resultList,
		Columns: []column{{"ID", "id", 0}, {"CODE", "code_name", 0}, {"NAME", "name", 0}, {"HOST", "host", 0}, {"JOBS", "max_concurrent_jobs", 0}, {"ONLINE", "online", 0}}},
	{Resource: "server", Verb: "show", Summary: "Show a server", Method: "GET", Path: "/servers/{id}", Args: []string{"id"}, Result: resultOne},
	{Resource: "server", Verb: "create", Summary: "Add a server", Method: "POST", Path: "/servers", Result: resultOne, DataFile: true,
		Body: []field{{"name", "name", kString, "the name"}, {"host", "host", kString, "the ComfyUI address, http://host:8188"},
			{"code-name", "code_name", kString, "the code name used in commands"}, {"local", "is_local", kBool, "ComfyUI reads this machine's files"},
			{"max-jobs", "max_concurrent_jobs", kInt, "jobs it may run at once"},
			{"comfyui-path", "comfyui_path", kString, "the folder of ComfyUI's installation, offered when LoRAs are imported"}}, Required: []string{"name", "host", "code_name"}},
	{Resource: "server", Verb: "update", Summary: "Change a server", Method: "PUT", Path: "/servers/{id}", Args: []string{"id"}, Result: resultOne, Merge: true, DataFile: true,
		Body: []field{{"name", "name", kString, "the name"}, {"host", "host", kString, "the ComfyUI address"},
			{"code-name", "code_name", kString, "the code name"}, {"local", "is_local", kBool, "ComfyUI reads this machine's files"},
			{"max-jobs", "max_concurrent_jobs", kInt, "jobs it may run at once"},
			{"comfyui-path", "comfyui_path", kString, "the folder of ComfyUI's installation"}}},
	{Resource: "server", Verb: "delete", Summary: "Delete a server", Method: "DELETE", Path: "/servers/{id}", Args: []string{"id"}, Result: resultNone},
	{Resource: "server", Verb: "loras", Summary: "List the LoRAs installed on a server", Method: "GET", Path: "/servers/{id}/loras", Args: []string{"id"}, Result: resultDoc},

	// generators
	{Resource: "generator", Verb: "list", Summary: "List generators", Method: "GET", Path: "/generators", Paged: true, Result: resultList,
		Columns: []column{{"ID", "id", 0}, {"CODE", "code_name", 0}, {"NAME", "name", 0}, {"OUTPUT", "output_type", 0}, {"NODE", "output_node_title", 0}}},
	{Resource: "generator", Verb: "show", Summary: "Show a generator", Method: "GET", Path: "/generators/{id}", Args: []string{"id"}, Result: resultOne},
	{Resource: "generator", Verb: "analyze", Summary: "Infer what a workflow produces; nothing is stored", Method: "POST", Path: "/generators/analyze", Result: resultDoc,
		Body: []field{{"workflow", "workflow", kJSON, "the ComfyUI workflow, @file"}}, Required: []string{"workflow"}},
	{Resource: "generator", Verb: "create", Summary: "Add a generator from a workflow", Method: "POST", Path: "/generators", Result: resultOne, DataFile: true,
		Body: generatorFields, Required: []string{"name", "code_name", "workflow"}},
	{Resource: "generator", Verb: "update", Summary: "Change a generator", Method: "PUT", Path: "/generators/{id}", Args: []string{"id"}, Result: resultOne, Merge: true, DataFile: true, Body: generatorFields},
	{Resource: "generator", Verb: "delete", Summary: "Delete a generator", Method: "DELETE", Path: "/generators/{id}", Args: []string{"id"}, Result: resultNone},

	// fixers
	{Resource: "fixer", Verb: "list", Summary: "List fixers", Method: "GET", Path: "/fixers", Paged: true, Result: resultList,
		Columns: []column{{"ID", "id", 0}, {"CODE", "code_name", 0}, {"NAME", "name", 0}, {"IMAGE IN", "load_image_title", 0}, {"IMAGE OUT", "save_image_title", 0}}},
	{Resource: "fixer", Verb: "show", Summary: "Show a fixer", Method: "GET", Path: "/fixers/{id}", Args: []string{"id"}, Result: resultOne},
	{Resource: "fixer", Verb: "create", Summary: "Add a fixer from a workflow", Method: "POST", Path: "/fixers", Result: resultOne, DataFile: true,
		Body: fixerFields, Required: []string{"name", "code_name", "workflow", "load_image_title", "save_image_title"}},
	{Resource: "fixer", Verb: "update", Summary: "Change a fixer", Method: "PUT", Path: "/fixers/{id}", Args: []string{"id"}, Result: resultOne, Merge: true, DataFile: true, Body: fixerFields},
	{Resource: "fixer", Verb: "delete", Summary: "Delete a fixer", Method: "DELETE", Path: "/fixers/{id}", Args: []string{"id"}, Result: resultNone},

	// groups
	{Resource: "group", Verb: "list", Summary: "List groups", Method: "GET", Path: "/groups", Paged: true, Result: resultList,
		Query:   []field{{"tag", "tag", kString, "only groups with this group tag"}},
		Columns: []column{{"ID", "id", 0}, {"CODE", "code_name", 0}, {"NAME", "name", 0}, {"TAGS", "tags", 0}}},
	{Resource: "group", Verb: "show", Summary: "Show a group", Method: "GET", Path: "/groups/{id}", Args: []string{"id"}, Result: resultOne},
	{Resource: "group", Verb: "create", Summary: "Add a group", Method: "POST", Path: "/groups", Result: resultOne, DataFile: true,
		Body: groupFields, Files: []filePart{{Flag: "thumbnail", Part: "thumbnail", Help: "a thumbnail image"}}, Required: []string{"name", "code_name"}},
	{Resource: "group", Verb: "update", Summary: "Change a group", Method: "PUT", Path: "/groups/{id}", Args: []string{"id"}, Result: resultOne, Merge: true, DataFile: true,
		Body: groupFields, Files: []filePart{{Flag: "thumbnail", Part: "thumbnail", Help: "a new thumbnail image"}}},
	{Resource: "group", Verb: "delete", Summary: "Delete a group with its items and files", Method: "DELETE", Path: "/groups/{id}", Args: []string{"id"}, Result: resultNone},
	{Resource: "group", Verb: "import-text", Summary: "Make a group with one item for each line of a text", Method: "POST", Path: "/groups/import-text", Result: resultOne, DataFile: true,
		Body:     append(append([]field(nil), groupFields[:4]...), field{"text", "text", kText, "the text, one item for each line (or @file)"}),
		Required: []string{"name", "code_name", "text"}},
	{Resource: "group", Verb: "import-civitai", Summary: "Make a group and import LoRAs from Civitai into it", Method: "POST", Path: "/groups/import-civitai", Result: resultOne, DataFile: true,
		Body:     append(append([]field(nil), groupFields[:4]...), append([]field{{"model", "models", kList, "a model as id[:model strength[:clip strength]], repeatable"}}, civitaiFields...)...),
		Required: []string{"name", "code_name", "models", "comfyui_path"}},

	// items
	{Resource: "item", Verb: "list", Summary: "List the items of a group", Method: "GET", Path: "/groups/{id}/items", Args: []string{"group"}, Paged: true, Result: resultList,
		Columns: []column{{"ID", "id", 0}, {"CODE", "code_name", 0}, {"NAME", "name", 0}, {"TAGS", "tags", 0}, {"PROMPT", "positive_prompt", 40}}},
	{Resource: "item", Verb: "show", Summary: "Show an item", Method: "GET", Path: "/items/{id}", Args: []string{"id"}, Result: resultOne},
	{Resource: "item", Verb: "create", Summary: "Add an item to a group", Method: "POST", Path: "/groups/{id}/items", Args: []string{"group"}, Result: resultOne, DataFile: true,
		Body: itemFields, Files: itemFiles, Required: []string{"code_name", "name"}},
	{Resource: "item", Verb: "update", Summary: "Change an item", Method: "PUT", Path: "/items/{id}", Args: []string{"id"}, Result: resultOne, Merge: true, DataFile: true, Body: itemFields, Files: itemFiles},
	{Resource: "item", Verb: "delete", Summary: "Delete an item with its files", Method: "DELETE", Path: "/items/{id}", Args: []string{"id"}, Result: resultNone},
	{Resource: "item", Verb: "add-civitai", Summary: "Add one Civitai LoRA to a group as an item", Method: "POST", Path: "/groups/{id}/items/civitai", Args: []string{"group"}, Result: resultOne,
		Body:     append([]field{{"model", "model_id", kInt, "the Civitai model id"}, {"model-strength", "model_strength", kFloat, "strength for the model (default 1)"}, {"clip-strength", "model_clip", kFloat, "strength for the text encoder (default 1)"}}, civitaiFields...),
		Required: []string{"model_id", "comfyui_path"}},

	// tags
	{Resource: "tag", Verb: "list", Summary: "List the tags in use", Method: "GET", Path: "/tags", Result: resultDoc,
		Query: []field{{"scope", "scope", kString, "group or item"}, {"prefix", "prefix", kString, "only tags that start with this"}}},
	{Resource: "tag", Verb: "rename", Summary: "Rename or merge a tag", Method: "POST", Path: "/tags/rename", Result: resultDoc,
		Body:     []field{{"scope", "scope", kString, "group or item"}, {"from", "from", kString, "the tag now"}, {"to", "to", kString, "the new name"}},
		Required: []string{"scope", "from", "to"}},

	// projects
	{Resource: "project", Verb: "list", Summary: "List projects", Method: "GET", Path: "/projects", Paged: true, Result: resultList,
		Columns: []column{{"ID", "id", 0}, {"NAME", "name", 0}, {"CREATED", "created_at", 0}}},
	{Resource: "project", Verb: "show", Summary: "Show a project", Method: "GET", Path: "/projects/{id}", Args: []string{"id"}, Result: resultOne},
	{Resource: "project", Verb: "create", Summary: "Add a project", Method: "POST", Path: "/projects", Result: resultOne, Body: []field{{"name", "name", kString, "the name"}}, Required: []string{"name"}},
	{Resource: "project", Verb: "update", Summary: "Rename a project", Method: "PUT", Path: "/projects/{id}", Args: []string{"id"}, Result: resultOne, Merge: true, Body: []field{{"name", "name", kString, "the name"}}},
	{Resource: "project", Verb: "delete", Summary: "Delete a project with its commands and jobs", Method: "DELETE", Path: "/projects/{id}", Args: []string{"id"}, Result: resultNone},

	// commands
	{Resource: "command", Verb: "list", Summary: "List the commands of a project with their progress", Method: "GET", Path: "/projects/{id}/commands", Args: []string{"project"}, Paged: true, Result: resultList,
		Columns: []column{{"#", "order", 0}, {"ID", "id", 0}, {"NAME", "name", 0}, {"STATUS", "status", 0}, {"JOBS", "progress", 0}, {"COMMAND", "command_code", 50}}},
	{Resource: "command", Verb: "show", Summary: "Show a command", Method: "GET", Path: "/commands/{id}", Args: []string{"id"}, Result: resultOne},
	{Resource: "command", Verb: "preview", Summary: "Check a command text and see what it would make; nothing is stored", Method: "POST", Path: "/commands/preview", Result: resultDoc, Special: "code",
		Body: []field{{"code", "code", kText, "the command text (or @file)"}}, Required: []string{"code"}},
	{Resource: "command", Verb: "add", Summary: "Add a command to a project", Method: "POST", Path: "/projects/{id}/commands", Args: []string{"project"}, Result: resultOne, Special: "code",
		Body: []field{{"name", "name", kString, "the name"}, {"description", "description", kString, "a description"}, {"code", "code", kText, "the command text (or @file)"},
			{"at", "insert_at", kInt, "the position, from 1 (default: the end)"}}, Required: []string{"code"}},
	{Resource: "command", Verb: "edit", Summary: "Change a command; a new text makes its jobs again", Method: "PUT", Path: "/commands/{id}", Args: []string{"id"}, Result: resultOne, Merge: true, Special: "code",
		Body: []field{{"name", "name", kString, "the name"}, {"description", "description", kString, "a description"}, {"code", "code", kText, "the command text (or @file)"}}},
	{Resource: "command", Verb: "delete", Summary: "Delete a command with its jobs and files", Method: "DELETE", Path: "/commands/{id}", Args: []string{"id"}, Result: resultNone},
	{Resource: "command", Verb: "move", Summary: "Move a command to a position in its project", Method: "POST", Path: "/commands/{id}/move", Args: []string{"id"}, Result: resultOne,
		Body: []field{{"to", "order", kInt, "the new position, from 1"}}, Required: []string{"order"}},
	{Resource: "command", Verb: "renumber", Summary: "Number the commands of a project 1..N", Method: "POST", Path: "/projects/{id}/commands/renumber", Args: []string{"project"}, Result: resultList,
		Columns: []column{{"#", "order", 0}, {"ID", "id", 0}, {"NAME", "name", 0}, {"STATUS", "status", 0}}},
	{Resource: "command", Verb: "run", Summary: "Queue the idle jobs of a command (--wait to follow it to the end)", Method: "POST", Path: "/commands/{id}/run", Args: []string{"id"}, Result: resultOne, Special: "wait"},
	{Resource: "command", Verb: "stop", Summary: "Return the queued jobs of a command to idle", Method: "POST", Path: "/commands/{id}/stop", Args: []string{"id"}, Result: resultOne},
	{Resource: "command", Verb: "recreate", Summary: "Delete the jobs of a command and make them again", Method: "POST", Path: "/commands/{id}/recreate", Args: []string{"id"}, Result: resultOne},
	{Resource: "command", Verb: "download", Summary: "Save the ZIP of the results of a finished command", Method: "GET", Path: "/commands/{id}/download", Args: []string{"id"}, Result: resultBytes},

	// jobs
	{Resource: "job", Verb: "list", Summary: "List the jobs of a command", Method: "GET", Path: "/commands/{id}/jobs", Args: []string{"command"}, Paged: true, Result: resultList,
		Query:   []field{{"status", "status", kString, "idle, queued, processing, error or finished"}},
		Columns: []column{{"ID", "id", 0}, {"STATUS", "status", 0}, {"SERVER", "server_host", 0}, {"SEED", "seed", 0}, {"PROMPT", "positive_prompt", 40}, {"ERROR", "error", 40}}},
	{Resource: "job", Verb: "show", Summary: "Show a job with its error and trace", Method: "GET", Path: "/jobs/{id}", Args: []string{"id"}, Result: resultOne},
	{Resource: "job", Verb: "edit", Summary: "Change the prompts of a job; it starts over", Method: "PUT", Path: "/jobs/{id}", Args: []string{"id"}, Result: resultOne, Merge: true,
		Body: []field{{"positive", "positive_prompt", kText, "the positive prompt (or @file)"}, {"negative", "negative_prompt", kText, "the negative prompt (or @file)"}}},
	{Resource: "job", Verb: "run", Summary: "Queue an idle job", Method: "POST", Path: "/jobs/{id}/run", Args: []string{"id"}, Result: resultOne},
	{Resource: "job", Verb: "stop", Summary: "Return a queued job to idle", Method: "POST", Path: "/jobs/{id}/stop", Args: []string{"id"}, Result: resultOne},

	// REPL
	{Resource: "repl", Verb: "run", Summary: "Run one ad-hoc job without a project", Method: "POST", Path: "/repl/run", Result: resultOne,
		Body: []field{{"on", "server", kString, "the code name of the server to run on"}, {"generator", "generator", kString, "the generator's code name"},
			{"positive", "positive", kText, "text added to the positive prompt (or @file)"}, {"negative", "negative", kText, "text added to the negative prompt (or @file)"},
			{"items", "items", kString, "items as group(item),group(item)"}, {"loras", "loras", kList, "LoRAs as name:model:clip, repeatable"}},
		Required: []string{"server", "generator"}},
	{Resource: "repl", Verb: "last", Summary: "Show the previous ad-hoc job", Method: "GET", Path: "/repl/last", Result: resultOne},

	// events, files, the contract
	{Resource: "events", Verb: "", Summary: "Show the event log (--follow to keep watching)", Method: "GET", Path: "/events", Result: resultDoc, Special: "follow",
		Query: []field{{"after", "after", kInt, "only events after this id"}}},
	{Resource: "file", Verb: "get", Summary: "Save a stored file (results, thumbnails, controlnet, ipadapter, regions, downloads)", Method: "GET", Path: "/files/{kind}/{name...}", Args: []string{"kind", "name"}, Result: resultBytes},
	{Resource: "openapi", Verb: "", Summary: "Print the OpenAPI description of the API", Method: "GET", Path: "/openapi.json", Result: resultDoc},
}

var generatorFields = []field{
	{"name", "name", kString, "the name"},
	{"code-name", "code_name", kString, "the code name used in commands"},
	{"workflow", "workflow", kJSON, "the ComfyUI workflow in API format, @file"},
	{"positive-title", "positive_prompt_title", kString, "title of the node that takes the positive prompt"},
	{"negative-title", "negative_prompt_title", kString, "title of the node that takes the negative prompt"},
	{"output-type", "output_type", kString, "image, video, 3d or unknown (found from the workflow if left out)"},
	{"output-node-title", "output_node_title", kString, "title of the output node"},
	{"output-node-class", "output_node_class_type", kString, "class of the output node"},
	{"random-seed", "has_random_seed", kBool, "give every job a new random seed"},
	{"attributes", "output_attributes", kJSON, "output attributes as JSON (or @file)"},
}

var fixerFields = []field{
	{"name", "name", kString, "the name"},
	{"code-name", "code_name", kString, "the code name used in commands"},
	{"workflow", "workflow", kJSON, "the ComfyUI workflow in API format, @file"},
	{"load-title", "load_image_title", kString, "title of the image-input node"},
	{"save-title", "save_image_title", kString, "title of the image-output node"},
	{"positive", "positive_prompt", kString, "positive text"},
	{"negative", "negative_prompt", kString, "negative text"},
	{"positive-title", "positive_prompt_title", kString, "title of the positive prompt node"},
	{"negative-title", "negative_prompt_title", kString, "title of the negative prompt node"},
}

// excluded are the operations of the API that bosctl does not offer, with the
// reason. A test checks that nothing else is missing.
var excluded = map[string]string{}
