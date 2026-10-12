// Package domain holds the entities, enumerations and value types of SPEC §3.
// It does no I/O and imports nothing from the project.
//
// Identifiers are unique integers assigned by the store. Relationships are by
// identifier and there are no automatic cascades (SPEC §3).
package domain

import "time"

// ID is the identifier of an entity.
type ID = int64

// ReplID is the project and command identifier of the reserved REPL job
// (SPEC §3.9).
const ReplID ID = -1

// Timestamps are carried by every entity.
type Timestamps struct {
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Status is the state of a command or a job (SPEC §3.10).
type Status string

// The statuses.
const (
	StatusIdle       Status = "idle"
	StatusQueued     Status = "queued"
	StatusProcessing Status = "processing"
	StatusError      Status = "error"
	StatusFinished   Status = "finished"
)

// Valid reports whether s is one of the five statuses.
func (s Status) Valid() bool {
	switch s {
	case StatusIdle, StatusQueued, StatusProcessing, StatusError, StatusFinished:
		return true
	}
	return false
}

// NotificationType is the kind of a job notification (SPEC §3.10).
type NotificationType string

// The notification types.
const (
	NotifyFinished   NotificationType = "finished"
	NotifyError      NotificationType = "error"
	NotifyProcessing NotificationType = "processing"
)

// OutputType is what a generator produces (SPEC §3.2).
type OutputType string

// The output types.
const (
	OutputImage   OutputType = "image"
	OutputVideo   OutputType = "video"
	Output3D      OutputType = "3d"
	OutputUnknown OutputType = "unknown"
)

// Valid reports whether t is one of the four output types.
func (t OutputType) Valid() bool {
	switch t {
	case OutputImage, OutputVideo, Output3D, OutputUnknown:
		return true
	}
	return false
}

// Server is a ComfyUI server (SPEC §3.1).
type Server struct {
	ID                ID     `json:"id"`
	Name              string `json:"name"`
	Host              string `json:"host"`
	CodeName          string `json:"code_name"`
	IsLocal           bool   `json:"is_local"`
	MaxConcurrentJobs int    `json:"max_concurrent_jobs"`
	Timestamps
}

// Generator is a workflow that produces the primary output (SPEC §3.2).
type Generator struct {
	ID       ID     `json:"id"`
	Name     string `json:"name"`
	CodeName string `json:"code_name"`
	// Workflow is the ComfyUI API-format graph, kept as the JSON it arrived as.
	Workflow            []byte           `json:"workflow"`
	PositivePromptTitle string           `json:"positive_prompt_title"`
	NegativePromptTitle string           `json:"negative_prompt_title"`
	OutputType          OutputType       `json:"output_type"`
	OutputAttributes    OutputAttributes `json:"output_attributes"`
	OutputNodeClassType string           `json:"output_node_class_type"`
	OutputNodeTitle     string           `json:"output_node_title"`
	HasRandomSeed       bool             `json:"has_random_seed"`
	Timestamps
}

// Fixer is a workflow that improves an existing image (SPEC §3.3).
type Fixer struct {
	ID                  ID     `json:"id"`
	Name                string `json:"name"`
	CodeName            string `json:"code_name"`
	Workflow            []byte `json:"workflow"`
	LoadImageTitle      string `json:"load_image_title"`
	SaveImageTitle      string `json:"save_image_title"`
	PositivePrompt      string `json:"positive_prompt"`
	NegativePrompt      string `json:"negative_prompt"`
	PositivePromptTitle string `json:"positive_prompt_title"`
	NegativePromptTitle string `json:"negative_prompt_title"`
	Timestamps
}

// Group is a named collection of items that share a role (SPEC §3.5).
type Group struct {
	ID          ID     `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CodeName    string `json:"code_name"`
	// Tags are group tags: they organise the user interface and are invisible to
	// the command language (SPEC §3.4).
	Tags      []string `json:"tags"`
	Thumbnail string   `json:"thumbnail"`

	UseControlnet        bool `json:"use_controlnet"`
	UseIPAdapter         bool `json:"use_ip_adapter"`
	UseMaskRegion        bool `json:"use_mask_region"`
	UseCoordinatesRegion bool `json:"use_coordinates_region"`
	UseTypeAttributes    bool `json:"use_type_attributes"`
	Timestamps
}

// LoRA is a LoRA entry of an item or a job.
type LoRA struct {
	Name          string  `json:"name"`
	StrengthModel float64 `json:"strength_model"`
	StrengthClip  float64 `json:"strength_clip"`
}

// LoRAUse says which LoRAs one item carries. The LoRA pages read it for every
// item that carries at least one.
type LoRAUse struct {
	ItemID    ID
	ItemCode  string
	ItemName  string
	GroupID   ID
	GroupCode string
	LoRAs     []LoRA
}

// IPAdapter is the IP-Adapter configuration of an item or a job.
type IPAdapter struct {
	ImageFile       string  `json:"image_file"`
	Weight          float64 `json:"weight"`
	WeightType      string  `json:"weight_type"`
	StartAt         float64 `json:"start_at"`
	EndAt           float64 `json:"end_at"`
	ClipVisionModel string  `json:"clip_vision_model"`
	ModelName       string  `json:"model_name"`
}

// ControlNet is a ControlNet configuration of an item or a job. Type is one of
// openpose, dwpose, tile, canny, midas, depth, lineart, softedge, scribble,
// normal.
type ControlNet struct {
	Type         string  `json:"type"`
	ImagePath    string  `json:"image_path"`
	IsReference  bool    `json:"is_reference"`
	ModelPattern string  `json:"model_pattern"`
	Strength     float64 `json:"strength"`
}

// MaskRegion is the mask-based region definition of an item (SPEC §3.6).
type MaskRegion struct {
	ReferencePath string `json:"reference_path"`
	FolderPath    string `json:"folder_path"`
	// MaskFiles maps a region keyword to its generated mask image.
	MaskFiles map[string]string `json:"mask_files"`
}

// CoordinatedRegion is a rectangle region of an item (SPEC §3.6).
type CoordinatedRegion struct {
	Keyword string `json:"keyword"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
}

// Item is one selectable value inside a group (SPEC §3.6).
type Item struct {
	ID                 ID                  `json:"id"`
	GroupID            ID                  `json:"group_id"`
	CodeName           string              `json:"code_name"`
	Name               string              `json:"name"`
	PositivePrompt     string              `json:"positive_prompt"`
	NegativePrompt     string              `json:"negative_prompt"`
	LoRAs              []LoRA              `json:"loras"`
	IPAdapter          *IPAdapter          `json:"ip_adapter"`
	ControlNets        []ControlNet        `json:"controlnets"`
	MaskRegion         *MaskRegion         `json:"mask_region_images"`
	CoordinatedRegions []CoordinatedRegion `json:"coordinated_regions"`
	// OutputType and OutputTypeAttributes override the generator's output
	// settings; both are empty unless the item defines them.
	OutputType           OutputType       `json:"output_type"`
	OutputTypeAttributes OutputAttributes `json:"output_type_attributes"`
	// Tags are item tags: the command language selects by them (SPEC §3.4).
	Tags      []string `json:"tags"`
	Thumbnail string   `json:"thumbnail"`
	Timestamps
}

// Project is a container of ordered commands (SPEC §3.7).
type Project struct {
	ID   ID     `json:"id"`
	Name string `json:"name"`
	Timestamps
}

// Command is one line of the command language with its jobs (SPEC §3.8).
type Command struct {
	ID        ID `json:"id"`
	ProjectID ID `json:"project_id"`
	// Order is 1-based and contiguous within the project.
	Order       int    `json:"order"`
	Status      Status `json:"status"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CommandCode string `json:"command_code"`
	// CommandJSON is the parsed form of the command (SPEC §6.10).
	CommandJSON  []byte `json:"command_json"`
	DownloadFile string `json:"download_file"`
	Timestamps
}

// CommandSummary is a command with the progress of its jobs.
type CommandSummary struct {
	Command
	FinishedJobs int `json:"finished_jobs"`
	TotalJobs    int `json:"total_jobs"`
}

// UsedItem names an item that contributed to a job.
type UsedItem struct {
	GroupID   ID     `json:"group_id"`
	GroupCode string `json:"group_code"`
	ItemID    ID     `json:"item_id"`
	ItemCode  string `json:"item_code"`
}

// FixerStep is one fixer of a job, with the server it runs on when the command
// names one (`> fixer on server`); otherwise it runs where the image was made.
type FixerStep struct {
	FixerID    ID     `json:"fixer_id"`
	FixerCode  string `json:"fixer_code"`
	ServerID   ID     `json:"server_id,omitempty"`
	ServerCode string `json:"server_code,omitempty"`
}

// RegionPrompt is the prompt for one region of a job (SPEC §6.6).
type RegionPrompt struct {
	Keyword     string             `json:"keyword"`
	MaskFile    string             `json:"mask_file,omitempty"`
	Coordinates *CoordinatedRegion `json:"coordinates,omitempty"`
	Prompt      string             `json:"prompt"`
	LoRAs       []LoRA             `json:"loras,omitempty"`
}

// Job is one concrete generation. It is a snapshot: it copies what it needs,
// so later edits to groups or items do not affect it (SPEC §3.9).
type Job struct {
	ID ID `json:"id"`
	// ProjectID and CommandID are ReplID for the reserved REPL job.
	ProjectID   ID         `json:"project_id"`
	CommandID   ID         `json:"command_id"`
	Items       []UsedItem `json:"items"`
	CommandText string     `json:"command_text"`
	Status      Status     `json:"status"`
	// QueueSeq orders queued jobs across commands: the command run first goes
	// first. Jobs of one command share it and run in id order.
	QueueSeq int64 `json:"queue_seq"`

	// ServerID is the server that runs or ran the generation; nil until a job
	// starts. PreferredServerID is the server a static spread chose at creation.
	ServerID          *ID    `json:"server_id"`
	ServerHost        string `json:"server_host"`
	PreferredServerID *ID    `json:"preferred_server_id"`
	Pinned            bool   `json:"pinned"`

	Fixers         []FixerStep `json:"fixers"`
	FinishedFixers int         `json:"finished_fixers"`
	RepeatIndex    int         `json:"repeat_index"`
	// RegionIndex is the index of the region-prompt set, or -1 without a region.
	RegionIndex int `json:"region_index"`
	// Seed is the effective seed once known (SPEC §6.8).
	Seed     *uint64 `json:"seed"`
	Attempts int     `json:"attempts"`

	GeneratorCodeName string           `json:"generator_code_name"`
	OutputType        OutputType       `json:"output_type"`
	OutputAttributes  OutputAttributes `json:"output_attributes"`
	PromptID          string           `json:"prompt_id"`

	PositivePrompt string         `json:"positive_prompt"`
	NegativePrompt string         `json:"negative_prompt"`
	ControlNets    []ControlNet   `json:"controlnets"`
	RegionPrompts  []RegionPrompt `json:"region_prompts"`
	IPAdapters     []IPAdapter    `json:"ip_adapters"`
	LoRAs          []LoRA         `json:"loras"`

	ResultPath string `json:"result_path"`
	Error      string `json:"error"`
	ErrorTrace string `json:"error_trace"`
	// Warnings are node errors that ComfyUI reported for a prompt it accepted
	// (SPEC §18.7).
	Warnings []string `json:"warnings"`

	IsGenerated bool `json:"is_generated"`
	IsFixed     bool `json:"is_fixed"`
	Timestamps
}

// Page asks for one page of a list (SPEC §4, §15.1).
type Page struct {
	Number int
	Size   int
}

// Page limits of SPEC §15.1.
const (
	DefaultPageSize = 20
	MaxPageSize     = 200
)

// Normalised returns p with defaults applied and the size capped.
func (p Page) Normalised() Page {
	if p.Number < 1 {
		p.Number = 1
	}
	if p.Size < 1 {
		p.Size = DefaultPageSize
	}
	if p.Size > MaxPageSize {
		p.Size = MaxPageSize
	}
	return p
}

// Offset is the number of rows before the page.
func (p Page) Offset() int {
	p = p.Normalised()
	return (p.Number - 1) * p.Size
}

// Paged is one page of a list.
type Paged[T any] struct {
	Items      []T `json:"items"`
	Total      int `json:"total"`
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	TotalPages int `json:"total_pages"`
}

// NewPaged builds the page envelope of SPEC §15.1. A nil items slice becomes an
// empty list, so it serialises as [] and not null.
func NewPaged[T any](items []T, total int, p Page) Paged[T] {
	p = p.Normalised()
	if items == nil {
		items = []T{}
	}
	pages := (total + p.Size - 1) / p.Size
	return Paged[T]{Items: items, Total: total, Page: p.Number, PageSize: p.Size, TotalPages: pages}
}

// TagScope says which vocabulary a tag belongs to (SPEC §3.4).
type TagScope string

// The two tag vocabularies.
const (
	TagScopeGroup TagScope = "group"
	TagScopeItem  TagScope = "item"
)

// TagCount is a tag with the number of groups or items that carry it.
type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// Files lists the files and folders an item owns: its thumbnail, IP-Adapter
// image, mask reference image and mask folder, and ControlNet images
// (SPEC §7.4). Empty and repeated paths are left out.
func (it *Item) Files() []string {
	files := []string{it.Thumbnail}
	if it.IPAdapter != nil {
		files = append(files, it.IPAdapter.ImageFile)
	}
	if it.MaskRegion != nil {
		files = append(files, it.MaskRegion.ReferencePath, it.MaskRegion.FolderPath)
	}
	for _, c := range it.ControlNets {
		files = append(files, c.ImagePath)
	}
	seen := make(map[string]bool, len(files))
	out := make([]string, 0, len(files))
	for _, f := range files {
		if f != "" && !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

// CommandStatusOf is the status of a command, given how many of its jobs are in
// each status (SPEC §7.1, §8.2): finished when all its jobs are; processing
// when one is running; queued when one waits; error when one failed and none is
// still moving; idle otherwise. A command without jobs is idle.
func CommandStatusOf(jobs map[Status]int) Status {
	total := 0
	for _, n := range jobs {
		total += n
	}
	switch {
	case total > 0 && jobs[StatusFinished] == total:
		return StatusFinished
	case jobs[StatusProcessing] > 0:
		return StatusProcessing
	case jobs[StatusQueued] > 0:
		return StatusQueued
	case jobs[StatusError] > 0:
		return StatusError
	}
	return StatusIdle
}
