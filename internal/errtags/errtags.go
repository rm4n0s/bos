// Package errtags is the catalogue of error tags (SPEC §17.2). Every failure of
// the application carries one of these tags in its github.com/rm4n0s/errors
// error, and the REST layer maps each to an HTTP status (SPEC §15.1).
//
// The package only names the tags. A test keeps it honest: every tag the code
// raises is listed, every listed tag is raised and has a route test, and a tag
// marked Pending is not raised yet.
package errtags

import "sort"

// Tag is one entry of the catalogue.
type Tag struct {
	Name  string
	Group string
	// Pending is true for a tag the specification lists that no code raises
	// yet; a later step removes the flag when it adds the failure.
	Pending bool
}

// Groups of SPEC §17.2.
const (
	Request        = "request"
	NotFound       = "not found"
	Naming         = "naming"
	Uniqueness     = "uniqueness"
	CommandText    = "command language"
	State          = "state"
	WorkflowGroup  = "workflow"
	ComfyUI        = "comfyui"
	Civitai        = "civitai"
	Infrastructure = "infrastructure"
	Validation     = "validation"
)

// All returns the catalogue sorted by name.
func All() []Tag {
	tags := []Tag{
		{Name: "InvalidRequest", Group: Request},
		{Name: "UploadTooLarge", Group: Request},

		{Name: "ServerNotFound", Group: NotFound},
		{Name: "GeneratorNotFound", Group: NotFound},
		{Name: "FixerNotFound", Group: NotFound},
		{Name: "GroupNotFound", Group: NotFound},
		{Name: "ItemNotFound", Group: NotFound},
		{Name: "ProjectNotFound", Group: NotFound},
		{Name: "CommandNotFound", Group: NotFound},
		{Name: "JobNotFound", Group: NotFound},
		{Name: "FileNotFound", Group: NotFound},
		{Name: "TagNotFound", Group: NotFound},

		{Name: "CodeNameInvalid", Group: Naming},
		{Name: "CodeNameReserved", Group: Naming},
		{Name: "TagInvalid", Group: Naming},

		{Name: "ServerCodeNameTaken", Group: Uniqueness},
		{Name: "GeneratorCodeNameTaken", Group: Uniqueness},
		{Name: "FixerCodeNameTaken", Group: Uniqueness},
		{Name: "GroupCodeNameTaken", Group: Uniqueness},
		{Name: "ItemCodeNameTaken", Group: Uniqueness},

		// Invalid values of the entities' own fields.
		{Name: "NameInvalid", Group: Validation},
		{Name: "OutputTypeInvalid", Group: Validation},
		{Name: "ServerMaxJobsInvalid", Group: Validation},
		{Name: "ServerHostInvalid", Group: Validation},
		{Name: "FixerTitleRequired", Group: Validation},
		{Name: "ItemControlNetInvalid", Group: Validation},
		{Name: "ItemRegionInvalid", Group: Validation},

		{Name: "CommandLexError", Group: CommandText},
		{Name: "CommandSyntaxError", Group: CommandText},
		{Name: "CommandRestrictionViolated", Group: CommandText},
		{Name: "CommandValidationFailed", Group: CommandText},
		{Name: "CommandJSONInvalid", Group: CommandText},
		// The semantic problems that a CommandValidationFailed lists.
		{Name: "CommandSelectionEmpty", Group: CommandText},
		{Name: "CommandWhereEmpty", Group: CommandText},
		{Name: "CommandRegionMismatch", Group: CommandText},
		{Name: "CommandWhereGroupMissing", Group: CommandText},
		{Name: "CommandSettingInvalid", Group: CommandText},
		{Name: "CommandKeyDuplicated", Group: CommandText},
		{Name: "CommandTooLarge", Group: CommandText},

		{Name: "CommandBusy", Group: State},
		{Name: "CommandNotRunnable", Group: State},
		{Name: "CommandNotFinished", Group: State},
		{Name: "JobNotRunnable", Group: State},
		{Name: "JobBusy", Group: State},
		{Name: "ReplInvalid", Group: State},

		{Name: "WorkflowInvalid", Group: WorkflowGroup},
		{Name: "WorkflowTitleDuplicated", Group: WorkflowGroup},
		{Name: "WorkflowTitleNotFound", Group: WorkflowGroup},
		{Name: "OutputNodeNotFound", Group: WorkflowGroup},
		{Name: "NoSamplerInWorkflow", Group: WorkflowGroup},
		{Name: "InjectorFailed", Group: WorkflowGroup},
		{Name: "RegionInvalid", Group: WorkflowGroup},
		{Name: "MaskGenerationFailed", Group: WorkflowGroup},

		{Name: "ServerUnreachable", Group: ComfyUI},
		{Name: "ComfyUIRejected", Group: ComfyUI},
		{Name: "ComfyUIExecutionFailed", Group: ComfyUI},
		{Name: "ComfyUIUploadFailed", Group: ComfyUI},
		{Name: "ComfyUIResultMissing", Group: ComfyUI},
		{Name: "ComfyUIInterrupted", Group: ComfyUI},
		{Name: "ComfyUIPromptLost", Group: ComfyUI},
		{Name: "ComfyUICallCancelled", Group: ComfyUI},

		{Name: "CivitaiNotConfigured", Group: Civitai, Pending: true},
		{Name: "CivitaiModelNotFound", Group: Civitai, Pending: true},
		{Name: "CivitaiRateLimited", Group: Civitai, Pending: true},
		{Name: "CivitaiDownloadFailed", Group: Civitai, Pending: true},
		{Name: "CivitaiMetadataInvalid", Group: Civitai, Pending: true},

		{Name: "StoreFailure", Group: Infrastructure},
		{Name: "MigrationFailed", Group: Infrastructure},
		{Name: "FileStoreFailure", Group: Infrastructure},
		{Name: "FileOutsideStorage", Group: Infrastructure},
		{Name: "ConfigInvalid", Group: Infrastructure},
		{Name: "ConfigNotReadable", Group: Infrastructure},
		{Name: "ListenFailed", Group: Infrastructure},
		{Name: "ServeFailed", Group: Infrastructure},
		{Name: "ShutdownFailed", Group: Infrastructure},
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i].Name < tags[j].Name })
	return tags
}
