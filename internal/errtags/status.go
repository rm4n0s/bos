package errtags

import "net/http"

// Status is the HTTP status of an error tag (SPEC §17.2); a tag the table does
// not know is a 500. A test fails if a tag of the catalogue has no entry. The
// REST API answers with it, and the web interface uses it to tell what the user
// can fix (below 500) from what only the log can explain.
func Status(tag string) int {
	if s, ok := statuses[tag]; ok {
		return s
	}
	return http.StatusInternalServerError
}

// Known reports whether the table has an entry for the tag.
func Known(tag string) bool { _, ok := statuses[tag]; return ok }

var statuses = map[string]int{
	"InvalidRequest": http.StatusBadRequest,
	"UploadTooLarge": http.StatusRequestEntityTooLarge,

	"ServerNotFound": http.StatusNotFound, "GeneratorNotFound": http.StatusNotFound, "FixerNotFound": http.StatusNotFound,
	"GroupNotFound": http.StatusNotFound, "ItemNotFound": http.StatusNotFound, "ProjectNotFound": http.StatusNotFound,
	"CommandNotFound": http.StatusNotFound, "JobNotFound": http.StatusNotFound, "FileNotFound": http.StatusNotFound,
	"TagNotFound": http.StatusNotFound,

	"CodeNameInvalid": http.StatusUnprocessableEntity, "CodeNameReserved": http.StatusUnprocessableEntity,
	"TagInvalid": http.StatusUnprocessableEntity,

	"ServerCodeNameTaken": http.StatusConflict, "GeneratorCodeNameTaken": http.StatusConflict,
	"FixerCodeNameTaken": http.StatusConflict, "GroupCodeNameTaken": http.StatusConflict,
	"ItemCodeNameTaken": http.StatusConflict,

	"NameInvalid": http.StatusUnprocessableEntity, "OutputTypeInvalid": http.StatusUnprocessableEntity,
	"ServerMaxJobsInvalid": http.StatusUnprocessableEntity, "ServerHostInvalid": http.StatusUnprocessableEntity, "ServerComfyPathInvalid": http.StatusUnprocessableEntity,
	"FixerTitleRequired": http.StatusUnprocessableEntity, "GeneratorTitleRequired": http.StatusUnprocessableEntity, "ItemControlNetInvalid": http.StatusUnprocessableEntity,
	"ItemRegionInvalid": http.StatusUnprocessableEntity,

	"CommandLexError": http.StatusUnprocessableEntity, "CommandSyntaxError": http.StatusUnprocessableEntity,
	"CommandRestrictionViolated": http.StatusUnprocessableEntity, "CommandValidationFailed": http.StatusUnprocessableEntity,
	"CommandJSONInvalid":    http.StatusInternalServerError,
	"CommandSelectionEmpty": http.StatusUnprocessableEntity, "CommandWhereEmpty": http.StatusUnprocessableEntity,
	"CommandRegionMismatch": http.StatusUnprocessableEntity, "CommandWhereGroupMissing": http.StatusUnprocessableEntity,
	"CommandSettingInvalid": http.StatusUnprocessableEntity, "CommandKeyDuplicated": http.StatusUnprocessableEntity,
	"CommandTooLarge": http.StatusUnprocessableEntity,

	"CommandBusy": http.StatusConflict, "CommandNotRunnable": http.StatusConflict, "CommandNotFinished": http.StatusConflict,
	"JobNotRunnable": http.StatusConflict, "JobBusy": http.StatusConflict, "ReplInvalid": http.StatusUnprocessableEntity,

	"WorkflowInvalid": http.StatusUnprocessableEntity, "WorkflowTitleDuplicated": http.StatusUnprocessableEntity,
	"WorkflowTitleNotFound": http.StatusUnprocessableEntity, "OutputNodeNotFound": http.StatusUnprocessableEntity,
	"NoSamplerInWorkflow": http.StatusUnprocessableEntity, "InjectorFailed": http.StatusUnprocessableEntity,
	"RegionInvalid": http.StatusUnprocessableEntity, "MaskGenerationFailed": http.StatusUnprocessableEntity,

	"ServerUnreachable": http.StatusServiceUnavailable, "ComfyUIRejected": http.StatusBadGateway,
	"ComfyUIExecutionFailed": http.StatusBadGateway, "ComfyUIUploadFailed": http.StatusBadGateway,
	"ComfyUIResultMissing": http.StatusBadGateway, "ComfyUIInterrupted": http.StatusBadGateway,
	"ComfyUIPromptLost": http.StatusBadGateway, "ComfyUICallCancelled": http.StatusServiceUnavailable,

	"CivitaiNotConfigured": http.StatusConflict, "CivitaiModelNotFound": http.StatusNotFound,
	"CivitaiRateLimited": http.StatusServiceUnavailable, "CivitaiDownloadFailed": http.StatusBadGateway,
	"CivitaiMetadataInvalid": http.StatusBadGateway,

	"StoreFailure": http.StatusInternalServerError, "MigrationFailed": http.StatusInternalServerError,
	"FileStoreFailure": http.StatusInternalServerError, "FileOutsideStorage": http.StatusInternalServerError,
	"TemplateInvalid": http.StatusInternalServerError, "ConfigInvalid": http.StatusUnprocessableEntity,
	"ListenFailed": http.StatusInternalServerError, "ServeFailed": http.StatusInternalServerError,
	"ShutdownFailed": http.StatusInternalServerError,
}
