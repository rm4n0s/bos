package store

import (
	"time"

	"github.com/rm4n0s/bos/internal/domain"
)

// The records below are the rows gorm reads and writes. The domain types stay
// free of storage tags; each record converts to and from its domain type. A
// record never sets a column default, because gorm replaces a zero value by
// the default: the repositories always write every column.

// stamps are the timestamp columns every table has. The repositories set them
// from the clock of the store.
type Stamps struct {
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime:false"`
}

func stampsOf(t domain.Timestamps) Stamps {
	return Stamps{CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
}

func (s Stamps) domain() domain.Timestamps {
	return domain.Timestamps{CreatedAt: s.CreatedAt.UTC(), UpdatedAt: s.UpdatedAt.UTC()}
}

// ---- servers ----

type serverRecord struct {
	ID                int64  `gorm:"column:id;primaryKey"`
	Name              string `gorm:"column:name"`
	Host              string `gorm:"column:host"`
	CodeName          string `gorm:"column:code_name"`
	IsLocal           bool   `gorm:"column:is_local"`
	MaxConcurrentJobs int    `gorm:"column:max_concurrent_jobs"`
	Stamps
}

func (serverRecord) TableName() string { return "servers" }

func serverToRecord(v *domain.Server) serverRecord {
	return serverRecord{ID: v.ID, Name: v.Name, Host: v.Host, CodeName: v.CodeName, IsLocal: v.IsLocal,
		MaxConcurrentJobs: v.MaxConcurrentJobs, Stamps: stampsOf(v.Timestamps)}
}

func (r serverRecord) domain() domain.Server {
	return domain.Server{ID: r.ID, Name: r.Name, Host: r.Host, CodeName: r.CodeName, IsLocal: r.IsLocal,
		MaxConcurrentJobs: r.MaxConcurrentJobs, Timestamps: r.Stamps.domain()}
}

// ---- generators ----

type generatorRecord struct {
	ID                  int64                            `gorm:"column:id;primaryKey"`
	Name                string                           `gorm:"column:name"`
	CodeName            string                           `gorm:"column:code_name"`
	Workflow            string                           `gorm:"column:workflow"`
	PositivePromptTitle string                           `gorm:"column:positive_prompt_title"`
	NegativePromptTitle string                           `gorm:"column:negative_prompt_title"`
	OutputType          string                           `gorm:"column:output_type"`
	OutputAttributes    jsonCol[domain.OutputAttributes] `gorm:"column:output_attributes"`
	OutputNodeClassType string                           `gorm:"column:output_node_class_type"`
	OutputNodeTitle     string                           `gorm:"column:output_node_title"`
	HasRandomSeed       bool                             `gorm:"column:has_random_seed"`
	Stamps
}

func (generatorRecord) TableName() string { return "generators" }

func generatorToRecord(v *domain.Generator) generatorRecord {
	return generatorRecord{ID: v.ID, Name: v.Name, CodeName: v.CodeName, Workflow: string(v.Workflow),
		PositivePromptTitle: v.PositivePromptTitle, NegativePromptTitle: v.NegativePromptTitle,
		OutputType: string(v.OutputType), OutputAttributes: jsonCol[domain.OutputAttributes]{V: v.OutputAttributes},
		OutputNodeClassType: v.OutputNodeClassType, OutputNodeTitle: v.OutputNodeTitle,
		HasRandomSeed: v.HasRandomSeed, Stamps: stampsOf(v.Timestamps)}
}

func (r generatorRecord) domain() domain.Generator {
	return domain.Generator{ID: r.ID, Name: r.Name, CodeName: r.CodeName, Workflow: []byte(r.Workflow),
		PositivePromptTitle: r.PositivePromptTitle, NegativePromptTitle: r.NegativePromptTitle,
		OutputType: domain.OutputType(r.OutputType), OutputAttributes: r.OutputAttributes.V,
		OutputNodeClassType: r.OutputNodeClassType, OutputNodeTitle: r.OutputNodeTitle,
		HasRandomSeed: r.HasRandomSeed, Timestamps: r.Stamps.domain()}
}

// ---- fixers ----

type fixerRecord struct {
	ID                  int64  `gorm:"column:id;primaryKey"`
	Name                string `gorm:"column:name"`
	CodeName            string `gorm:"column:code_name"`
	Workflow            string `gorm:"column:workflow"`
	LoadImageTitle      string `gorm:"column:load_image_title"`
	SaveImageTitle      string `gorm:"column:save_image_title"`
	PositivePrompt      string `gorm:"column:positive_prompt"`
	NegativePrompt      string `gorm:"column:negative_prompt"`
	PositivePromptTitle string `gorm:"column:positive_prompt_title"`
	NegativePromptTitle string `gorm:"column:negative_prompt_title"`
	Stamps
}

func (fixerRecord) TableName() string { return "fixers" }

func fixerToRecord(v *domain.Fixer) fixerRecord {
	return fixerRecord{ID: v.ID, Name: v.Name, CodeName: v.CodeName, Workflow: string(v.Workflow),
		LoadImageTitle: v.LoadImageTitle, SaveImageTitle: v.SaveImageTitle, PositivePrompt: v.PositivePrompt,
		NegativePrompt: v.NegativePrompt, PositivePromptTitle: v.PositivePromptTitle,
		NegativePromptTitle: v.NegativePromptTitle, Stamps: stampsOf(v.Timestamps)}
}

func (r fixerRecord) domain() domain.Fixer {
	return domain.Fixer{ID: r.ID, Name: r.Name, CodeName: r.CodeName, Workflow: []byte(r.Workflow),
		LoadImageTitle: r.LoadImageTitle, SaveImageTitle: r.SaveImageTitle, PositivePrompt: r.PositivePrompt,
		NegativePrompt: r.NegativePrompt, PositivePromptTitle: r.PositivePromptTitle,
		NegativePromptTitle: r.NegativePromptTitle, Timestamps: r.Stamps.domain()}
}

// ---- groups ----

type groupRecord struct {
	ID                   int64  `gorm:"column:id;primaryKey"`
	Name                 string `gorm:"column:name"`
	Description          string `gorm:"column:description"`
	CodeName             string `gorm:"column:code_name"`
	Thumbnail            string `gorm:"column:thumbnail"`
	UseLora              bool   `gorm:"column:use_lora"`
	UseControlnet        bool   `gorm:"column:use_controlnet"`
	UseIPAdapter         bool   `gorm:"column:use_ip_adapter"`
	UseMaskRegion        bool   `gorm:"column:use_mask_region"`
	UseCoordinatesRegion bool   `gorm:"column:use_coordinates_region"`
	UseTypeAttributes    bool   `gorm:"column:use_type_attributes"`
	Stamps
}

func (groupRecord) TableName() string { return "groups" }

func groupToRecord(v *domain.Group) groupRecord {
	return groupRecord{ID: v.ID, Name: v.Name, Description: v.Description, CodeName: v.CodeName,
		Thumbnail: v.Thumbnail, UseLora: v.UseLora, UseControlnet: v.UseControlnet, UseIPAdapter: v.UseIPAdapter,
		UseMaskRegion: v.UseMaskRegion, UseCoordinatesRegion: v.UseCoordinatesRegion,
		UseTypeAttributes: v.UseTypeAttributes, Stamps: stampsOf(v.Timestamps)}
}

// domain converts the record; the caller fills in the tags.
func (r groupRecord) domain() domain.Group {
	return domain.Group{ID: r.ID, Name: r.Name, Description: r.Description, CodeName: r.CodeName,
		Tags: []string{}, Thumbnail: r.Thumbnail, UseLora: r.UseLora, UseControlnet: r.UseControlnet,
		UseIPAdapter: r.UseIPAdapter, UseMaskRegion: r.UseMaskRegion, UseCoordinatesRegion: r.UseCoordinatesRegion,
		UseTypeAttributes: r.UseTypeAttributes, Timestamps: r.Stamps.domain()}
}

// ---- tags ----

type groupTagRecord struct {
	GroupID int64  `gorm:"column:group_id;primaryKey"`
	Tag     string `gorm:"column:tag;primaryKey"`
}

func (groupTagRecord) TableName() string { return "group_tags" }

type itemTagRecord struct {
	ItemID int64  `gorm:"column:item_id;primaryKey"`
	Tag    string `gorm:"column:tag;primaryKey"`
}

func (itemTagRecord) TableName() string { return "item_tags" }

// ---- items ----

type itemRecord struct {
	ID                   int64                               `gorm:"column:id;primaryKey"`
	GroupID              int64                               `gorm:"column:group_id"`
	CodeName             string                              `gorm:"column:code_name"`
	Name                 string                              `gorm:"column:name"`
	PositivePrompt       string                              `gorm:"column:positive_prompt"`
	NegativePrompt       string                              `gorm:"column:negative_prompt"`
	LoRAs                jsonCol[[]domain.LoRA]              `gorm:"column:loras"`
	IPAdapter            jsonOpt[domain.IPAdapter]           `gorm:"column:ip_adapter"`
	ControlNets          jsonCol[[]domain.ControlNet]        `gorm:"column:controlnets"`
	MaskRegion           jsonOpt[domain.MaskRegion]          `gorm:"column:mask_region"`
	CoordinatedRegions   jsonCol[[]domain.CoordinatedRegion] `gorm:"column:coordinated_regions"`
	OutputType           string                              `gorm:"column:output_type"`
	OutputTypeAttributes jsonCol[domain.OutputAttributes]    `gorm:"column:output_type_attributes"`
	Thumbnail            string                              `gorm:"column:thumbnail"`
	Stamps
}

func (itemRecord) TableName() string { return "items" }

func itemToRecord(v *domain.Item) itemRecord {
	return itemRecord{ID: v.ID, GroupID: v.GroupID, CodeName: v.CodeName, Name: v.Name,
		PositivePrompt: v.PositivePrompt, NegativePrompt: v.NegativePrompt,
		LoRAs: jsonCol[[]domain.LoRA]{V: v.LoRAs}, IPAdapter: jsonOpt[domain.IPAdapter]{V: v.IPAdapter},
		ControlNets: jsonCol[[]domain.ControlNet]{V: v.ControlNets}, MaskRegion: jsonOpt[domain.MaskRegion]{V: v.MaskRegion},
		CoordinatedRegions:   jsonCol[[]domain.CoordinatedRegion]{V: v.CoordinatedRegions},
		OutputType:           string(v.OutputType),
		OutputTypeAttributes: jsonCol[domain.OutputAttributes]{V: v.OutputTypeAttributes},
		Thumbnail:            v.Thumbnail, Stamps: stampsOf(v.Timestamps)}
}

// domain converts the record; the caller fills in the tags.
func (r itemRecord) domain() domain.Item {
	return domain.Item{ID: r.ID, GroupID: r.GroupID, CodeName: r.CodeName, Name: r.Name,
		PositivePrompt: r.PositivePrompt, NegativePrompt: r.NegativePrompt, LoRAs: r.LoRAs.V,
		IPAdapter: r.IPAdapter.V, ControlNets: r.ControlNets.V, MaskRegion: r.MaskRegion.V,
		CoordinatedRegions: r.CoordinatedRegions.V, OutputType: domain.OutputType(r.OutputType),
		OutputTypeAttributes: r.OutputTypeAttributes.V, Tags: []string{}, Thumbnail: r.Thumbnail,
		Timestamps: r.Stamps.domain()}
}

// ---- projects ----

type projectRecord struct {
	ID   int64  `gorm:"column:id;primaryKey"`
	Name string `gorm:"column:name"`
	Stamps
}

func (projectRecord) TableName() string { return "projects" }

func projectToRecord(v *domain.Project) projectRecord {
	return projectRecord{ID: v.ID, Name: v.Name, Stamps: stampsOf(v.Timestamps)}
}

func (r projectRecord) domain() domain.Project {
	return domain.Project{ID: r.ID, Name: r.Name, Timestamps: r.Stamps.domain()}
}

// ---- commands ----

type commandRecord struct {
	ID           int64  `gorm:"column:id;primaryKey"`
	ProjectID    int64  `gorm:"column:project_id"`
	OrderNo      int    `gorm:"column:order_no"`
	Status       string `gorm:"column:status"`
	Name         string `gorm:"column:name"`
	Description  string `gorm:"column:description"`
	CommandCode  string `gorm:"column:command_code"`
	CommandJSON  string `gorm:"column:command_json"`
	DownloadFile string `gorm:"column:download_file"`
	Stamps
}

func (commandRecord) TableName() string { return "commands" }

func commandToRecord(v *domain.Command) commandRecord {
	return commandRecord{ID: v.ID, ProjectID: v.ProjectID, OrderNo: v.Order, Status: string(v.Status), Name: v.Name,
		Description: v.Description, CommandCode: v.CommandCode, CommandJSON: string(v.CommandJSON),
		DownloadFile: v.DownloadFile, Stamps: stampsOf(v.Timestamps)}
}

func (r commandRecord) domain() domain.Command {
	return domain.Command{ID: r.ID, ProjectID: r.ProjectID, Order: r.OrderNo, Status: domain.Status(r.Status),
		Name: r.Name, Description: r.Description, CommandCode: r.CommandCode, CommandJSON: []byte(r.CommandJSON),
		DownloadFile: r.DownloadFile, Timestamps: r.Stamps.domain()}
}

// ---- jobs ----

type jobRecord struct {
	ID                int64                            `gorm:"column:id;primaryKey"`
	ProjectID         int64                            `gorm:"column:project_id"`
	CommandID         int64                            `gorm:"column:command_id"`
	Items             jsonCol[[]domain.UsedItem]       `gorm:"column:items"`
	CommandText       string                           `gorm:"column:command_text"`
	Status            string                           `gorm:"column:status"`
	QueueSeq          int64                            `gorm:"column:queue_seq"`
	ServerID          *int64                           `gorm:"column:server_id"`
	ServerHost        string                           `gorm:"column:server_host"`
	PreferredServerID *int64                           `gorm:"column:preferred_server_id"`
	Pinned            bool                             `gorm:"column:pinned"`
	Fixers            jsonCol[[]domain.FixerStep]      `gorm:"column:fixers"`
	FinishedFixers    int                              `gorm:"column:finished_fixers"`
	RepeatIndex       int                              `gorm:"column:repeat_index"`
	RegionIndex       int                              `gorm:"column:region_index"`
	Seed              uintCol                          `gorm:"column:seed"`
	Attempts          int                              `gorm:"column:attempts"`
	GeneratorCodeName string                           `gorm:"column:generator_code_name"`
	OutputType        string                           `gorm:"column:output_type"`
	OutputAttributes  jsonCol[domain.OutputAttributes] `gorm:"column:output_attributes"`
	PromptID          string                           `gorm:"column:prompt_id"`
	PositivePrompt    string                           `gorm:"column:positive_prompt"`
	NegativePrompt    string                           `gorm:"column:negative_prompt"`
	ControlNets       jsonCol[[]domain.ControlNet]     `gorm:"column:controlnets"`
	RegionPrompts     jsonCol[[]domain.RegionPrompt]   `gorm:"column:region_prompts"`
	IPAdapters        jsonCol[[]domain.IPAdapter]      `gorm:"column:ip_adapters"`
	LoRAs             jsonCol[[]domain.LoRA]           `gorm:"column:loras"`
	ResultPath        string                           `gorm:"column:result_path"`
	Error             string                           `gorm:"column:error"`
	ErrorTrace        string                           `gorm:"column:error_trace"`
	Warnings          jsonCol[[]string]                `gorm:"column:warnings"`
	IsGenerated       bool                             `gorm:"column:is_generated"`
	IsFixed           bool                             `gorm:"column:is_fixed"`
	Stamps
}

func (jobRecord) TableName() string { return "jobs" }

func jobToRecord(j *domain.Job) jobRecord {
	return jobRecord{ID: j.ID, ProjectID: j.ProjectID, CommandID: j.CommandID,
		Items: jsonCol[[]domain.UsedItem]{V: j.Items}, CommandText: j.CommandText, Status: string(j.Status),
		QueueSeq: j.QueueSeq, ServerID: j.ServerID, ServerHost: j.ServerHost, PreferredServerID: j.PreferredServerID,
		Pinned: j.Pinned, Fixers: jsonCol[[]domain.FixerStep]{V: j.Fixers}, FinishedFixers: j.FinishedFixers,
		RepeatIndex: j.RepeatIndex, RegionIndex: j.RegionIndex, Seed: uintCol{V: j.Seed}, Attempts: j.Attempts,
		GeneratorCodeName: j.GeneratorCodeName, OutputType: string(j.OutputType),
		OutputAttributes: jsonCol[domain.OutputAttributes]{V: j.OutputAttributes}, PromptID: j.PromptID,
		PositivePrompt: j.PositivePrompt, NegativePrompt: j.NegativePrompt,
		ControlNets:   jsonCol[[]domain.ControlNet]{V: j.ControlNets},
		RegionPrompts: jsonCol[[]domain.RegionPrompt]{V: j.RegionPrompts},
		IPAdapters:    jsonCol[[]domain.IPAdapter]{V: j.IPAdapters}, LoRAs: jsonCol[[]domain.LoRA]{V: j.LoRAs},
		ResultPath: j.ResultPath, Error: j.Error, ErrorTrace: j.ErrorTrace, Warnings: jsonCol[[]string]{V: j.Warnings},
		IsGenerated: j.IsGenerated, IsFixed: j.IsFixed, Stamps: stampsOf(j.Timestamps)}
}

func (r jobRecord) domain() domain.Job {
	return domain.Job{ID: r.ID, ProjectID: r.ProjectID, CommandID: r.CommandID, Items: r.Items.V,
		CommandText: r.CommandText, Status: domain.Status(r.Status), QueueSeq: r.QueueSeq, ServerID: r.ServerID,
		ServerHost: r.ServerHost, PreferredServerID: r.PreferredServerID, Pinned: r.Pinned, Fixers: r.Fixers.V,
		FinishedFixers: r.FinishedFixers, RepeatIndex: r.RepeatIndex, RegionIndex: r.RegionIndex, Seed: r.Seed.V,
		Attempts: r.Attempts, GeneratorCodeName: r.GeneratorCodeName, OutputType: domain.OutputType(r.OutputType),
		OutputAttributes: r.OutputAttributes.V, PromptID: r.PromptID, PositivePrompt: r.PositivePrompt,
		NegativePrompt: r.NegativePrompt, ControlNets: r.ControlNets.V, RegionPrompts: r.RegionPrompts.V,
		IPAdapters: r.IPAdapters.V, LoRAs: r.LoRAs.V, ResultPath: r.ResultPath, Error: r.Error,
		ErrorTrace: r.ErrorTrace, Warnings: r.Warnings.V, IsGenerated: r.IsGenerated, IsFixed: r.IsFixed,
		Timestamps: r.Stamps.domain()}
}
