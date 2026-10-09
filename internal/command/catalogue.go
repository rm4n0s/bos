package command

// The types below are the command language's read-only view of the catalogue.
// The package is pure and imports nothing from the project, so the service
// builds these from the domain entities (SPEC §14.3).

// Output types of a generator or an item override (SPEC §3.2).
const (
	OutputImage   = "image"
	OutputVideo   = "video"
	Output3D      = "3d"
	OutputUnknown = "unknown"
)

// Catalogue is what the validator and the expander read. Items are returned in
// creation order (ascending id), so identical data always yields the same job
// order (SPEC §6.4).
type Catalogue interface {
	HasServer(code string) bool
	HasFixer(code string) bool
	Generator(code string) (Generator, bool)
	HasGroup(code string) bool
	// Items returns the items of a group, or nil when the group does not exist.
	Items(group string) []Item
}

// Generator is the part of a generator the command language needs.
type Generator struct {
	OutputType    string
	HasRandomSeed bool
	// Attrs are the generator's default output attributes (SPEC §3.2).
	Attrs map[string]Value
}

// Item is one catalogue item with everything the expander copies into a job.
type Item struct {
	ID    int64
	Group string
	Code  string
	// Tags are the item tags, lower case (SPEC §3.4).
	Tags     []string
	Positive string
	Negative string

	LoRAs       []LoRA
	ControlNets []ControlNet
	IPAdapters  []IPAdapter
	// MaskFiles and Coordinates define the region keywords of the item
	// (SPEC §6.6); a keyword has a mask or coordinates, not both.
	MaskFiles   []MaskFile
	Coordinates []Coordinates
	// Output overrides the generator's output type and attributes; nil unless
	// the item defines both (SPEC §7.1).
	Output *OutputSpec
}

// LoRA is a LoRA entry of an item.
type LoRA struct {
	Name          string
	StrengthModel float64
	StrengthClip  float64
}

// ControlNet is a ControlNet entry of an item. The expander only carries it.
type ControlNet struct {
	Type         string
	ImagePath    string
	IsReference  bool
	ModelPattern string
	Strength     float64
}

// IPAdapter is the IP-Adapter entry of an item. The expander only carries it.
type IPAdapter struct {
	ImageFile       string
	Weight          float64
	WeightType      string
	StartAt         float64
	EndAt           float64
	ClipVisionModel string
	ModelName       string
}

// MaskFile is a region keyword defined by a generated mask image.
type MaskFile struct {
	Keyword string
	File    string
}

// Coordinates is a region keyword defined by a rectangle.
type Coordinates struct {
	Keyword string
	Width   int
	Height  int
	X       int
	Y       int
}

// OutputSpec is an output type with its attributes.
type OutputSpec struct {
	Type  string
	Attrs map[string]Value
}

// ItemRef names an original item that contributes to a job.
type ItemRef struct {
	ID    int64
	Group string
	Code  string
}

// regionKeywords returns the keywords an item defines, masks first.
func (it *Item) regionKeywords() []string {
	var kws []string
	for _, m := range it.MaskFiles {
		kws = append(kws, m.Keyword)
	}
	for _, c := range it.Coordinates {
		kws = append(kws, c.Keyword)
	}
	return kws
}
