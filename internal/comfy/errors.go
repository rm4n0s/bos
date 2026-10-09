package comfy

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"

	comfyui "github.com/rm4n0s/comfyui-client-go"
	"github.com/rm4n0s/errors"
)

// mapError turns an error of the client library into an application error at
// the adapter boundary (SPEC §17.3). A cancelled or expired context of the
// caller is ComfyUICallCancelled; a failure of the network or a 5xx answer is
// ServerUnreachable; anything else the server said is failTag.
func (s *Server) mapError(ctx context.Context, err error, failTag, operation string) error {
	if _, ok := errors.FromError(err); ok {
		return err
	}
	if ctx.Err() != nil {
		return errors.NewErr("ComfyUICallCancelled", ctx.Err()).SetMetadata("operation", operation, "host", s.baseURL)
	}
	var status *comfyui.StatusError
	if errors.As(err, &status) {
		if unavailable(status.Status) {
			return errors.NewErr("ServerUnreachable", err).SetMetadata("operation", operation, "host", s.baseURL, "status", status.Status)
		}
		return errors.NewErr(failTag, err).SetMetadata("operation", operation, "host", s.baseURL, "status", status.Status, "body", status.Body)
	}
	if isNetworkError(err) {
		return errors.NewErr("ServerUnreachable", err).SetMetadata("operation", operation, "host", s.baseURL)
	}
	return errors.NewErr(failTag, err).SetMetadata("operation", operation, "host", s.baseURL)
}

// unavailable reports the statuses a proxy or a server that is starting up
// answers with: ComfyUI itself is not there. Any other status is ComfyUI's own
// answer, even a 500.
func unavailable(status int) bool {
	return status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

// isNetworkError reports failures to talk to the server at all, as opposed to
// answers from it.
func isNetworkError(err error) bool {
	var nerr interface{ Timeout() bool }
	if errors.As(err, &nerr) {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded) || isConnError(err)
}

// warningsOf turns the node errors of an accepted prompt into warnings, ordered
// by node id.
func warningsOf(nodeErrors map[string]any) []NodeWarning {
	ids := make([]string, 0, len(nodeErrors))
	for id := range nodeErrors {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]NodeWarning, len(ids))
	for i, id := range ids {
		b, err := json.Marshal(nodeErrors[id])
		msg := string(b)
		if err != nil {
			msg = "unreadable node error"
		}
		out[i] = NodeWarning{NodeID: id, Message: msg}
	}
	return out
}
