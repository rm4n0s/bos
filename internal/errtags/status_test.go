package errtags

import (
	"net/http"
	"testing"
)

// TestStatusTableCoversTheCatalogue fails when a tag of the catalogue has no
// HTTP status, or the table names a tag that is not in the catalogue.
func TestStatusTableCoversTheCatalogue(t *testing.T) {
	known := map[string]bool{}
	for _, tag := range All() {
		known[tag.Name] = true
		if !Known(tag.Name) {
			t.Errorf("the tag %s has no HTTP status", tag.Name)
		}
	}
	for tag := range statuses {
		if !known[tag] {
			t.Errorf("the status table names %s, which is not in the catalogue", tag)
		}
	}
}

func TestStatus(t *testing.T) {
	tests := map[string]int{
		"InvalidRequest": http.StatusBadRequest, "GroupNotFound": http.StatusNotFound, "ServerCodeNameTaken": http.StatusConflict,
		"CommandValidationFailed": http.StatusUnprocessableEntity, "UploadTooLarge": http.StatusRequestEntityTooLarge,
		"StoreFailure": http.StatusInternalServerError, "NoSuchTag": http.StatusInternalServerError,
	}
	for tag, want := range tests {
		if got := Status(tag); got != want {
			t.Errorf("Status(%s) = %d, want %d", tag, got, want)
		}
	}
}
