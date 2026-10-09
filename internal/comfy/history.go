package comfy

import (
	"encoding/json"
	"sort"
	"strings"

	comfyui "github.com/rm4n0s/comfyui-client-go"
	"github.com/rm4n0s/errors"
)

// outcome says how a prompt ended.
type outcome int

const (
	outcomeRunning outcome = iota
	outcomeSuccess
	outcomeFailed
	outcomeInterrupted
)

// finished is what a history entry or an event says about a prompt.
type finished struct {
	outcome outcome
	// failure is set when outcome is outcomeFailed and the details are known.
	failure *comfyui.ExecutionErrorData
	// interrupted is set when outcome is outcomeInterrupted and the details are known.
	interrupted *comfyui.ExecutionInterruptedData
}

// outcomeOf reads how a history entry ended. A prompt that is not in the
// history has not finished.
func outcomeOf(e *comfyui.HistoryEntry) finished {
	for _, m := range e.Status.Messages {
		pair, ok := m.([]any)
		if !ok || len(pair) != 2 {
			continue
		}
		kind, _ := pair[0].(string)
		switch comfyui.EventType(kind) {
		case comfyui.EventExecutionError:
			var d comfyui.ExecutionErrorData
			if remarshal(pair[1], &d) {
				return finished{outcome: outcomeFailed, failure: &d}
			}
			return finished{outcome: outcomeFailed}
		case comfyui.EventExecutionInterrupted:
			var d comfyui.ExecutionInterruptedData
			if remarshal(pair[1], &d) {
				return finished{outcome: outcomeInterrupted, interrupted: &d}
			}
			return finished{outcome: outcomeInterrupted}
		}
	}
	switch {
	case e.Status.StatusStr == "error":
		return finished{outcome: outcomeFailed}
	case e.Status.Completed || e.Status.StatusStr == "success":
		return finished{outcome: outcomeSuccess}
	}
	return finished{outcome: outcomeRunning}
}

func remarshal(v any, into any) bool {
	b, err := json.Marshal(v)
	return err == nil && json.Unmarshal(b, into) == nil
}

// resultOf collects the output files of a finished prompt, by node.
func resultOf(promptID string, e *comfyui.HistoryEntry) Result {
	r := Result{PromptID: promptID, Outputs: map[string][]OutputFile{}}
	ids := make([]string, 0, len(e.Outputs))
	for id := range e.Outputs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		for _, f := range e.Outputs[id].Files() {
			r.Outputs[id] = append(r.Outputs[id], OutputFile{NodeID: id, Key: f.Key, Filename: f.Filename, Subfolder: f.Subfolder, Type: f.Type})
		}
	}
	return r
}

// failureError builds the error of a failed prompt: ComfyUIExecutionFailed with
// the node, exception and traceback in its metadata.
func failureError(promptID string, f finished) *errors.Error {
	if f.failure == nil {
		return errors.New("ComfyUIExecutionFailed", "ComfyUI reported that the prompt failed", "prompt_id", promptID)
	}
	d := f.failure
	msg := strings.TrimSpace(d.ExceptionMessage)
	if msg == "" {
		msg = "ComfyUI reported that the prompt failed"
	}
	return errors.New("ComfyUIExecutionFailed", msg, "prompt_id", promptID, "node_id", d.NodeID, "node_type", d.NodeType,
		"exception_type", d.ExceptionType, "exception_message", d.ExceptionMessage, "traceback", d.Traceback)
}

func interruptedError(promptID string, f finished) *errors.Error {
	if f.interrupted == nil {
		return errors.New("ComfyUIInterrupted", "the prompt was interrupted", "prompt_id", promptID)
	}
	return errors.New("ComfyUIInterrupted", "the prompt was interrupted", "prompt_id", promptID,
		"node_id", f.interrupted.NodeID, "node_type", f.interrupted.NodeType)
}
