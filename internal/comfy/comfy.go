// Package comfy is the application's view of a ComfyUI server (SPEC §18): the
// Client port the scheduler and the services depend on, and its adapter over
// comfyui-client-go with the event hub that routes WebSocket events to the jobs
// waiting for them.
//
// No error of the client library crosses this package: every one is wrapped
// with a tag of SPEC §17.2 (ServerUnreachable, ComfyUIRejected,
// ComfyUIExecutionFailed, ComfyUIUploadFailed, ComfyUIResultMissing, …).
package comfy

import (
	"context"
	"io"

	"github.com/rm4n0s/bos/internal/workflow"
)

// Client is one ComfyUI server (SPEC §18.2).
type Client interface {
	// Ping succeeds only if ComfyUI answers; it is the cheap health probe.
	Ping(ctx context.Context) error
	// QueuePrompt submits a workflow. A prompt that ComfyUI accepted with node
	// errors (it only skips some outputs) comes back with its id and the errors
	// as warnings; a rejected one fails with ComfyUIRejected.
	QueuePrompt(ctx context.Context, wf workflow.Graph) (promptID string, warnings []NodeWarning, err error)
	// Wait blocks until the prompt has finished: it returns the output files, or
	// an error that says why the prompt failed.
	Wait(ctx context.Context, promptID string) (Result, error)
	// Fetch streams an output file.
	Fetch(ctx context.Context, f OutputFile) (io.ReadCloser, error)
	// Upload stores an input file on the server under a name derived from its
	// content and returns the value for a LoadImage input (SPEC §18.6).
	Upload(ctx context.Context, name string, r io.Reader) (inputValue string, err error)
	// ListModels lists the installed models of a folder, such as "loras".
	ListModels(ctx context.Context, folder string) ([]string, error)
	// Cancel removes a pending prompt from the queue or interrupts a running one.
	Cancel(ctx context.Context, promptID string) error
	Close() error
}

// NodeWarning is a node error that ComfyUI reported for a prompt it accepted.
type NodeWarning struct {
	NodeID  string
	Message string
}

// OutputFile is a file a node produced.
type OutputFile struct {
	NodeID    string
	Key       string // the output key, such as "images" or "gifs"
	Filename  string
	Subfolder string
	Type      string // "output", "temp" or "input"
}

// Result is the outcome of a prompt that ran to the end.
type Result struct {
	PromptID string
	// Outputs holds the files of each node that produced any, by node id.
	Outputs map[string][]OutputFile
}

// Files returns the files of one node.
func (r Result) Files(nodeID string) []OutputFile { return r.Outputs[nodeID] }
