package store

import (
	"time"

	"gorm.io/gorm"
)

// The structs below are the schema of migration 1 as it was written, and they
// must never change: a later change to the schema is a new migration with its
// own record of what it changes. They are not the records the repositories use
// (models.go), which follow the latest schema.
//
// There are no ON DELETE CASCADE clauses: a parent is deleted together with its
// dependants by the repositories, as SPEC §3 and §7.4 require, and a foreign
// key stops anything that is forgotten. Jobs deliberately have no foreign keys:
// the REPL job uses project and command -1, and a job keeps the server host it
// ran on even if the server is deleted. order_no is not unique: inserting a
// command shifts its successors, and SQLite checks a unique index row by row.

type v1Server struct {
	ID                int64     `gorm:"column:id;primaryKey;autoIncrement"`
	Name              string    `gorm:"column:name;not null"`
	Host              string    `gorm:"column:host;not null"`
	CodeName          string    `gorm:"column:code_name;not null;uniqueIndex:servers_code_name"`
	IsLocal           bool      `gorm:"column:is_local;not null"`
	MaxConcurrentJobs int       `gorm:"column:max_concurrent_jobs;not null;check:max_concurrent_jobs >= 1"`
	CreatedAt         time.Time `gorm:"column:created_at;not null"`
	UpdatedAt         time.Time `gorm:"column:updated_at;not null"`
}

func (v1Server) TableName() string { return "servers" }

type v1Generator struct {
	ID                  int64     `gorm:"column:id;primaryKey;autoIncrement"`
	Name                string    `gorm:"column:name;not null"`
	CodeName            string    `gorm:"column:code_name;not null;uniqueIndex:generators_code_name"`
	Workflow            string    `gorm:"column:workflow;not null"`
	PositivePromptTitle string    `gorm:"column:positive_prompt_title;not null"`
	NegativePromptTitle string    `gorm:"column:negative_prompt_title;not null"`
	OutputType          string    `gorm:"column:output_type;not null"`
	OutputAttributes    string    `gorm:"column:output_attributes;type:text;not null"`
	OutputNodeClassType string    `gorm:"column:output_node_class_type;not null"`
	OutputNodeTitle     string    `gorm:"column:output_node_title;not null"`
	HasRandomSeed       bool      `gorm:"column:has_random_seed;not null"`
	CreatedAt           time.Time `gorm:"column:created_at;not null"`
	UpdatedAt           time.Time `gorm:"column:updated_at;not null"`
}

func (v1Generator) TableName() string { return "generators" }

type v1Fixer struct {
	ID                  int64     `gorm:"column:id;primaryKey;autoIncrement"`
	Name                string    `gorm:"column:name;not null"`
	CodeName            string    `gorm:"column:code_name;not null;uniqueIndex:fixers_code_name"`
	Workflow            string    `gorm:"column:workflow;not null"`
	LoadImageTitle      string    `gorm:"column:load_image_title;not null"`
	SaveImageTitle      string    `gorm:"column:save_image_title;not null"`
	PositivePrompt      string    `gorm:"column:positive_prompt;not null"`
	NegativePrompt      string    `gorm:"column:negative_prompt;not null"`
	PositivePromptTitle string    `gorm:"column:positive_prompt_title;not null"`
	NegativePromptTitle string    `gorm:"column:negative_prompt_title;not null"`
	CreatedAt           time.Time `gorm:"column:created_at;not null"`
	UpdatedAt           time.Time `gorm:"column:updated_at;not null"`
}

func (v1Fixer) TableName() string { return "fixers" }

type v1Group struct {
	ID                   int64        `gorm:"column:id;primaryKey;autoIncrement"`
	Name                 string       `gorm:"column:name;not null"`
	Description          string       `gorm:"column:description;not null"`
	CodeName             string       `gorm:"column:code_name;not null;uniqueIndex:groups_code_name"`
	Thumbnail            string       `gorm:"column:thumbnail;not null"`
	UseLora              bool         `gorm:"column:use_lora;not null"`
	UseControlnet        bool         `gorm:"column:use_controlnet;not null"`
	UseIPAdapter         bool         `gorm:"column:use_ip_adapter;not null"`
	UseMaskRegion        bool         `gorm:"column:use_mask_region;not null"`
	UseCoordinatesRegion bool         `gorm:"column:use_coordinates_region;not null"`
	UseTypeAttributes    bool         `gorm:"column:use_type_attributes;not null"`
	CreatedAt            time.Time    `gorm:"column:created_at;not null"`
	UpdatedAt            time.Time    `gorm:"column:updated_at;not null"`
	Tags                 []v1GroupTag `gorm:"foreignKey:GroupID"`
	Items                []v1Item     `gorm:"foreignKey:GroupID"`
}

func (v1Group) TableName() string { return "groups" }

type v1GroupTag struct {
	GroupID int64  `gorm:"column:group_id;primaryKey;autoIncrement:false;not null"`
	Tag     string `gorm:"column:tag;primaryKey;not null;index:group_tags_by_tag"`
}

func (v1GroupTag) TableName() string { return "group_tags" }

type v1Item struct {
	ID                   int64       `gorm:"column:id;primaryKey;autoIncrement"`
	GroupID              int64       `gorm:"column:group_id;not null;uniqueIndex:items_group_code,priority:1"`
	CodeName             string      `gorm:"column:code_name;not null;uniqueIndex:items_group_code,priority:2"`
	Name                 string      `gorm:"column:name;not null"`
	PositivePrompt       string      `gorm:"column:positive_prompt;not null"`
	NegativePrompt       string      `gorm:"column:negative_prompt;not null"`
	LoRAs                string      `gorm:"column:loras;type:text;not null"`
	IPAdapter            *string     `gorm:"column:ip_adapter;type:text"`
	ControlNets          string      `gorm:"column:controlnets;type:text;not null"`
	MaskRegion           *string     `gorm:"column:mask_region;type:text"`
	CoordinatedRegions   string      `gorm:"column:coordinated_regions;type:text;not null"`
	OutputType           string      `gorm:"column:output_type;not null"`
	OutputTypeAttributes string      `gorm:"column:output_type_attributes;type:text;not null"`
	Thumbnail            string      `gorm:"column:thumbnail;not null"`
	CreatedAt            time.Time   `gorm:"column:created_at;not null"`
	UpdatedAt            time.Time   `gorm:"column:updated_at;not null"`
	Tags                 []v1ItemTag `gorm:"foreignKey:ItemID"`
}

func (v1Item) TableName() string { return "items" }

type v1ItemTag struct {
	ItemID int64  `gorm:"column:item_id;primaryKey;autoIncrement:false;not null"`
	Tag    string `gorm:"column:tag;primaryKey;not null;index:item_tags_by_tag"`
}

func (v1ItemTag) TableName() string { return "item_tags" }

type v1Project struct {
	ID        int64       `gorm:"column:id;primaryKey;autoIncrement"`
	Name      string      `gorm:"column:name;not null"`
	CreatedAt time.Time   `gorm:"column:created_at;not null"`
	UpdatedAt time.Time   `gorm:"column:updated_at;not null"`
	Commands  []v1Command `gorm:"foreignKey:ProjectID"`
}

func (v1Project) TableName() string { return "projects" }

type v1Command struct {
	ID           int64     `gorm:"column:id;primaryKey;autoIncrement"`
	ProjectID    int64     `gorm:"column:project_id;not null;index:commands_by_project,priority:1"`
	OrderNo      int       `gorm:"column:order_no;not null;index:commands_by_project,priority:2"`
	Status       string    `gorm:"column:status;not null"`
	Name         string    `gorm:"column:name;not null"`
	Description  string    `gorm:"column:description;not null"`
	CommandCode  string    `gorm:"column:command_code;not null"`
	CommandJSON  string    `gorm:"column:command_json;type:text;not null"`
	DownloadFile string    `gorm:"column:download_file;not null"`
	CreatedAt    time.Time `gorm:"column:created_at;not null"`
	UpdatedAt    time.Time `gorm:"column:updated_at;not null"`
}

func (v1Command) TableName() string { return "commands" }

type v1Job struct {
	ID                int64     `gorm:"column:id;primaryKey;autoIncrement;index:jobs_by_command,priority:2;index:jobs_by_queue,priority:3"`
	ProjectID         int64     `gorm:"column:project_id;not null"`
	CommandID         int64     `gorm:"column:command_id;not null;index:jobs_by_command,priority:1"`
	Items             string    `gorm:"column:items;type:text;not null"`
	CommandText       string    `gorm:"column:command_text;not null"`
	Status            string    `gorm:"column:status;not null;index:jobs_by_queue,priority:1"`
	QueueSeq          int64     `gorm:"column:queue_seq;not null;index:jobs_by_queue,priority:2"`
	ServerID          *int64    `gorm:"column:server_id"`
	ServerHost        string    `gorm:"column:server_host;not null"`
	PreferredServerID *int64    `gorm:"column:preferred_server_id"`
	Pinned            bool      `gorm:"column:pinned;not null"`
	Fixers            string    `gorm:"column:fixers;type:text;not null"`
	FinishedFixers    int       `gorm:"column:finished_fixers;not null"`
	RepeatIndex       int       `gorm:"column:repeat_index;not null"`
	RegionIndex       int       `gorm:"column:region_index;not null"`
	Seed              *string   `gorm:"column:seed;type:text"`
	Attempts          int       `gorm:"column:attempts;not null"`
	GeneratorCodeName string    `gorm:"column:generator_code_name;not null"`
	OutputType        string    `gorm:"column:output_type;not null"`
	OutputAttributes  string    `gorm:"column:output_attributes;type:text;not null"`
	PromptID          string    `gorm:"column:prompt_id;not null"`
	PositivePrompt    string    `gorm:"column:positive_prompt;not null"`
	NegativePrompt    string    `gorm:"column:negative_prompt;not null"`
	ControlNets       string    `gorm:"column:controlnets;type:text;not null"`
	RegionPrompts     string    `gorm:"column:region_prompts;type:text;not null"`
	IPAdapters        string    `gorm:"column:ip_adapters;type:text;not null"`
	LoRAs             string    `gorm:"column:loras;type:text;not null"`
	ResultPath        string    `gorm:"column:result_path;not null"`
	Error             string    `gorm:"column:error;not null"`
	ErrorTrace        string    `gorm:"column:error_trace;not null"`
	Warnings          string    `gorm:"column:warnings;type:text;not null"`
	IsGenerated       bool      `gorm:"column:is_generated;not null"`
	IsFixed           bool      `gorm:"column:is_fixed;not null"`
	CreatedAt         time.Time `gorm:"column:created_at;not null"`
	UpdatedAt         time.Time `gorm:"column:updated_at;not null"`
}

func (v1Job) TableName() string { return "jobs" }

// migrateV1 creates the initial schema (SPEC §3, §10).
func migrateV1(tx *gorm.DB) error {
	return tx.Migrator().CreateTable(
		&v1Server{}, &v1Generator{}, &v1Fixer{}, &v1Group{}, &v1GroupTag{}, &v1Item{}, &v1ItemTag{},
		&v1Project{}, &v1Command{}, &v1Job{},
	)
}
