package ui

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/bos/internal/errtags"
	"github.com/rm4n0s/errors"
	"github.com/starfederation/datastar-go/datastar"
)

// stream answers a request with server-sent events (SPEC §16.1, rule 3): HTML
// patches for elements and JSON patches for the signals.
type stream struct {
	*datastar.ServerSentEventGenerator
	u *UI
	// eventID, when set, is attached to the patches of a live stream so that a
	// reconnecting browser can say where it stopped.
	eventID string
}

func (u *UI) newStream(w http.ResponseWriter, r *http.Request) *stream {
	return &stream{ServerSentEventGenerator: datastar.NewSSE(w, r), u: u}
}

// render renders a fragment and patches it into the page. The fragment's
// top-level element carries the id that says what it replaces.
func (s *stream) render(name string, data any, opts ...datastar.PatchElementOption) error {
	html, err := s.u.fragment(name, data)
	if err != nil {
		return err
	}
	if s.eventID != "" {
		opts = append(opts, datastar.WithPatchElementsEventID(s.eventID))
	}
	return s.PatchElements(html, opts...)
}

// into renders a fragment as the content of the element with the given id.
func (s *stream) into(id, name string, data any) error {
	return s.render(name, data, datastar.WithSelectorID(id), datastar.WithModeInner())
}

// set patches signals; a nil value removes one.
func (s *stream) set(signals map[string]any) error { return s.MarshalAndPatchSignals(signals) }

// toast adds a transient message to the page (SPEC §16.4, §16.5).
func (s *stream) toast(kind, message string) error {
	return s.render("toast", toastData{Kind: kind, Message: message},
		datastar.WithSelectorID("toasts"), datastar.WithModeAppend())
}

type toastData struct {
	ID      string
	Kind    string
	Message string
}

// readSignals decodes the signals Datastar sent with the request.
func readSignals(r *http.Request, into any) error {
	if err := datastar.ReadSignals(r, into); err != nil {
		return errors.New("InvalidRequest", "the request carries no readable signals", "reason", err.Error())
	}
	return nil
}

// act builds a handler for an action: it reads the signals into a value of
// type S, opens the event stream, and runs fn. A failure fn returns is shown
// in the element errID, or as a toast when only the log can explain it
// (SPEC §16.5). The answer is always 200: Datastar's own handling of HTTP
// errors is not relied on for messages the user reads.
func act[S any](u *UI, errID string, fn func(r *http.Request, in *S, s *stream) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in S
		if err := readSignals(r, &in); err != nil {
			http.Error(w, "the request is not valid", http.StatusBadRequest)
			return
		}
		s := u.newStream(w, r)
		if err := fn(r, &in, s); err != nil {
			u.report(s, r, errID, err)
		}
	}
}

// view builds a handler for a read-only fragment (a table page, a form to
// edit): same as act, for GET requests.
func view[S any](u *UI, fn func(r *http.Request, in *S, s *stream) error) http.HandlerFunc {
	return act(u, "", fn)
}

// report shows a failure to the user.
func (u *UI) report(s *stream, r *http.Request, errID string, err error) {
	message, userFault := describe(err)
	if !userFault {
		logFailure(u, r, err)
		message = "Something went wrong on the server. Look at its log."
	}
	if userFault && errID != "" {
		if perr := s.into(errID, "error_text", message); perr == nil {
			return
		}
	}
	if terr := s.toast("error", message); terr != nil {
		u.Logger.Warn("could not report an error to the page", "error", terr.Error())
	}
}

// describe turns an error into the sentence the user reads, and says whether
// the user could have caused it. Failures of the storage, the files or the
// program itself are not the user's.
func describe(err error) (message string, userFault bool) {
	appErr, ok := errors.FromError(err)
	if !ok {
		return "", false
	}
	if errtags.Status(appErr.Tag) >= http.StatusInternalServerError && appErr.Tag != "ServerUnreachable" {
		return "", false
	}
	message = appErr.Message
	if list, ok := appErr.Metadata["errors"]; ok {
		if b, err := json.Marshal(list); err == nil {
			var problems []struct {
				Rule    string `json:"rule"`
				Message string `json:"message"`
			}
			if json.Unmarshal(b, &problems) == nil {
				for _, p := range problems {
					message += "; " + p.Message
				}
			}
		}
	}
	return message, true
}

func logFailure(u *UI, r *http.Request, err error) {
	if appErr, ok := errors.FromError(err); ok {
		if j, jerr := appErr.ToJson(); jerr == nil {
			u.Logger.Warn("request failed", "method", r.Method, "path", r.URL.Path, "error", j)
			return
		}
	}
	u.Logger.Warn("request failed", "method", r.Method, "path", r.URL.Path, "error", err.Error())
}

// flexInt reads a signal that holds a number, a number written as text, or
// nothing: a number input that is cleared sends an empty string.
type flexInt int64

func (f *flexInt) UnmarshalJSON(b []byte) error {
	var n json.Number
	if string(b) == `""` || string(b) == "null" {
		*f = 0
		return nil
	}
	s := string(b)
	if len(s) > 1 && s[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		s = str
	}
	n = json.Number(s)
	if i, err := n.Int64(); err == nil {
		*f = flexInt(i)
		return nil
	}
	fl, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*f = flexInt(fl)
	return nil
}

// flexFloat is flexInt for decimals.
type flexFloat float64

func (f *flexFloat) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == `""` || s == "null" {
		*f = 0
		return nil
	}
	if len(s) > 1 && s[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		s = str
	}
	fl, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*f = flexFloat(fl)
	return nil
}

// pathID reads the {name} segment of the path as an identifier.
func pathID(r *http.Request, name string) (domain.ID, error) {
	raw := r.PathValue(name)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, errors.New("InvalidRequest", "the "+name+" in the path is not a positive integer", "value", raw)
	}
	return id, nil
}

// pageParam reads the page query parameter of a table fragment. The size is the
// default; the interface does not offer another.
func pageParam(r *http.Request) (domain.Page, error) {
	raw := r.URL.Query().Get("page")
	if raw == "" {
		return domain.Page{Number: 1}.Normalised(), nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return domain.Page{}, errors.New("InvalidRequest", "the page must be a positive integer", "value", raw)
	}
	return domain.Page{Number: n}.Normalised(), nil
}
