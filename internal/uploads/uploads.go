// Package uploads holds what the REST API and the web interface share when a
// request carries files: which parts are accepted and where they go, the check
// of what a file really is, and the rule that clients upload files but never
// name them (SPEC §15.1, §16.3).
package uploads

import (
	"bufio"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/files"
	"github.com/rm4n0s/bos/internal/filesrv"
	"github.com/rm4n0s/errors"
)

// Image types of SPEC §10 and decision D6: thumbnails are PNG, JPEG or GIF;
// reference images may also be WebP. A mask reference must be PNG, because a
// lossy format would add stray colours (SPEC §7.3).
var (
	ThumbnailTypes = []string{"image/png", "image/jpeg", "image/gif"}
	ReferenceTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}
	MaskTypes      = []string{"image/png"}
)

// Kind says where an uploaded part is stored and what it may be.
type Kind struct {
	Dir   string
	MIMEs []string
}

// GroupKinds names the file parts of a group: its thumbnail.
func GroupKinds(d filesrv.Dirs) func(field string) (Kind, bool) {
	return func(field string) (Kind, bool) {
		if field == "thumbnail" {
			return Kind{Dir: d.Thumbnails, MIMEs: ThumbnailTypes}, true
		}
		return Kind{}, false
	}
}

// ItemKinds names the file parts of an item: thumbnail, ip_adapter_image,
// mask_reference and controlnet_<n>.
func ItemKinds(d filesrv.Dirs) func(field string) (Kind, bool) {
	return func(field string) (Kind, bool) {
		switch field {
		case "thumbnail":
			return Kind{Dir: d.Thumbnails, MIMEs: ThumbnailTypes}, true
		case "ip_adapter_image":
			return Kind{Dir: d.IPAdapter, MIMEs: ReferenceTypes}, true
		case "mask_reference":
			return Kind{Dir: d.Regions, MIMEs: MaskTypes}, true
		}
		if _, ok := ControlNetIndex(field); ok {
			return Kind{Dir: d.ControlNet, MIMEs: ReferenceTypes}, true
		}
		return Kind{}, false
	}
}

// ControlNetIndex reads n out of the form field controlnet_<n>.
func ControlNetIndex(field string) (int, bool) {
	rest, ok := strings.CutPrefix(field, "controlnet_")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	return n, err == nil && n >= 0 && strconv.Itoa(n) == rest
}

// Set is the files one request stored, by form field.
type Set struct{ files map[string]string }

// NewSet returns an empty set.
func NewSet() *Set { return &Set{files: map[string]string{}} }

// Add records the file stored for a field.
func (s *Set) Add(field, path string) { s.files[field] = path }

// Path returns the file stored for a field.
func (s *Set) Path(field string) (string, bool) { p, ok := s.files[field]; return p, ok }

// Has reports whether a field has a file.
func (s *Set) Has(field string) bool { _, ok := s.files[field]; return ok }

// Fields returns the fields that have files, sorted.
func (s *Set) Fields() []string {
	names := make([]string, 0, len(s.files))
	for f := range s.files {
		names = append(names, f)
	}
	sort.Strings(names)
	return names
}

// Paths returns the stored paths, in field order.
func (s *Set) Paths() []string {
	names := s.Fields()
	out := make([]string, len(names))
	for i, f := range names {
		out[i] = s.files[f]
	}
	return out
}

// Contains reports whether path was stored by this request.
func (s *Set) Contains(path string) bool { return slices.Contains(s.Paths(), path) }

// Discard removes the files of a request that did not succeed. A file that
// cannot be removed is logged: the request has already failed.
func Discard(store *files.Store, s *Set, logger *slog.Logger) {
	if s == nil {
		return
	}
	for _, p := range s.Paths() {
		if err := store.Remove(p); err != nil {
			logger.Warn("could not remove an uploaded file", "path", p, "error", err.Error())
		}
	}
}

// Save stores one uploaded file after checking, from its first bytes, that it
// is of an accepted type. The name the client gave is only used to make a
// readable file name. A file over limit bytes (0 for none) is UploadTooLarge.
func Save(store *files.Store, field string, kind Kind, filename string, r io.Reader, limit int64) (string, error) {
	br := bufio.NewReaderSize(r, 512)
	head, _ := br.Peek(512) // a short file is fine; a read error shows in the copy
	if got := DetectType(head); !slices.Contains(kind.MIMEs, got) {
		return "", errors.New("InvalidRequest", "the file is not of an accepted type", "part", field, "type", got, "accepted", kind.MIMEs)
	}
	return store.SaveUpload(kind.Dir, filename, br, limit)
}

// DetectType sniffs the content type of a file, ignoring parameters.
func DetectType(head []byte) string {
	t, _, err := mime.ParseMediaType(http.DetectContentType(head))
	if err != nil {
		return "application/octet-stream"
	}
	return t
}

// OwnedPath checks that a file path a client sent is one the request stored or
// one the entity already had: clients upload files, they do not name them.
func OwnedPath(path string, s *Set, existing []string) error {
	if path == "" || s.Contains(path) || slices.Contains(existing, path) {
		return nil
	}
	return errors.New("InvalidRequest", "file paths cannot be set; upload the file instead")
}

// ApplyItem puts the stored files into the item, and checks that every other
// file path in it is one the item already had. The masks and their folder are
// made by the server, never named by a client.
func ApplyItem(in *domain.Item, s *Set, existing []string) error {
	if p, ok := s.Path("thumbnail"); ok {
		in.Thumbnail = p
	}
	if p, ok := s.Path("ip_adapter_image"); ok {
		if in.IPAdapter == nil {
			return errors.New("InvalidRequest", "ip_adapter_image needs an IP-Adapter in the item")
		}
		in.IPAdapter.ImageFile = p
	}
	if p, ok := s.Path("mask_reference"); ok {
		if in.MaskRegion == nil {
			return errors.New("InvalidRequest", "mask_reference needs a mask region in the item")
		}
		in.MaskRegion.ReferencePath = p
	}
	for _, field := range s.Fields() {
		if n, ok := ControlNetIndex(field); ok {
			if n >= len(in.ControlNets) {
				return errors.New("InvalidRequest", "the file part has no matching entry in controlnets", "part", field)
			}
			p, _ := s.Path(field)
			in.ControlNets[n].ImagePath = p
		}
	}
	if in.MaskRegion != nil {
		in.MaskRegion.FolderPath, in.MaskRegion.MaskFiles = "", nil
	}
	paths := []string{in.Thumbnail}
	if in.IPAdapter != nil {
		paths = append(paths, in.IPAdapter.ImageFile)
	}
	if in.MaskRegion != nil {
		paths = append(paths, in.MaskRegion.ReferencePath)
	}
	for _, c := range in.ControlNets {
		paths = append(paths, c.ImagePath)
	}
	for _, p := range paths {
		if err := OwnedPath(p, s, existing); err != nil {
			return err
		}
	}
	return nil
}
