package comfy

import (
	"context"
	"time"

	comfyui "github.com/rm4n0s/comfyui-client-go"
	"github.com/rm4n0s/errors"
)

// Wait implements Client. It waits for the end of a prompt in two ways at once:
// the hub hands over the end event when the event stream carries it, and a poll
// of the history and the queue, at every PollInterval, finds it when the stream
// does not. The poll is what completes a prompt whose events went to a client
// that is gone (SPEC §18.3, step 5): after a dropped connection, after a
// restart of the application, or when no connection was ever up.
func (s *Server) Wait(ctx context.Context, promptID string) (Result, error) {
	w := s.hub.register(promptID)
	defer s.hub.unregister(w)

	failures := 0
	timer := time.NewTimer(0) // the first poll is immediate: the prompt may be over already
	defer timer.Stop()
	for {
		select {
		case ev := <-w.ch:
			return s.finish(ctx, promptID, ev.finished, nil)
		case <-timer.C:
			p := s.poll(ctx, promptID)
			switch {
			case ctx.Err() != nil:
				return Result{}, s.cancelled(ctx, "wait")
			case p.err != nil:
				failures++
				if failures >= s.opts.PollFailures {
					return Result{}, p.err
				}
			case p.lost:
				return Result{}, errors.New("ComfyUIPromptLost", "ComfyUI no longer knows the prompt: it is neither running, queued nor in the history",
					"prompt_id", promptID, "host", s.baseURL)
			case p.finished.outcome != outcomeRunning:
				return s.finish(ctx, promptID, p.finished, p.entry)
			default:
				failures = 0
			}
			timer.Reset(s.opts.PollInterval)
		case <-ctx.Done():
			return Result{}, s.cancelled(ctx, "wait")
		}
	}
}

func (s *Server) cancelled(ctx context.Context, operation string) error {
	return errors.NewErr("ComfyUICallCancelled", ctx.Err()).SetMetadata("operation", operation, "host", s.baseURL)
}

// pollResult is what one look at the server found.
type pollResult struct {
	finished finished
	entry    *comfyui.HistoryEntry
	lost     bool
	err      error
}

// poll looks for the prompt in the history, then in the queue. A prompt that is
// in neither may have finished between the two requests, so the history is read
// once more before the prompt is called lost.
func (s *Server) poll(ctx context.Context, id string) pollResult {
	ctx, cancel := s.call(ctx)
	defer cancel()

	entry, err := s.http.GetHistory(ctx, id)
	if err == nil {
		return pollResult{finished: outcomeOf(entry), entry: entry}
	}
	if !errors.Is(err, comfyui.ErrPromptNotFound) {
		return pollResult{err: s.mapError(ctx, err, "ComfyUIRejected", "read history")}
	}
	q, err := s.http.Queue(ctx)
	if err != nil {
		return pollResult{err: s.mapError(ctx, err, "ComfyUIRejected", "read queue")}
	}
	if contains(q.Running, id) || contains(q.Pending, id) {
		return pollResult{}
	}
	entry, err = s.http.GetHistory(ctx, id)
	switch {
	case err == nil:
		return pollResult{finished: outcomeOf(entry), entry: entry}
	case errors.Is(err, comfyui.ErrPromptNotFound):
		return pollResult{lost: true}
	}
	return pollResult{err: s.mapError(ctx, err, "ComfyUIRejected", "read history")}
}

// finish turns the end of a prompt into the result or the error.
func (s *Server) finish(ctx context.Context, id string, f finished, entry *comfyui.HistoryEntry) (Result, error) {
	switch f.outcome {
	case outcomeFailed:
		return Result{}, failureError(id, f)
	case outcomeInterrupted:
		return Result{}, interruptedError(id, f)
	}
	if entry == nil {
		var err error
		if entry, err = s.historyAfterSuccess(ctx, id); err != nil {
			return Result{}, err
		}
	}
	return resultOf(id, entry), nil
}

// historyAfterSuccess reads the history entry of a prompt whose success event
// has arrived. ComfyUI writes the entry just after it sends the event, so the
// first read can come too early.
func (s *Server) historyAfterSuccess(ctx context.Context, id string) (*comfyui.HistoryEntry, error) {
	for attempt := 0; attempt < 40; attempt++ {
		cctx, cancel := s.call(ctx)
		entry, err := s.http.GetHistory(cctx, id)
		cancel()
		switch {
		case err == nil:
			return entry, nil
		case !errors.Is(err, comfyui.ErrPromptNotFound):
			return nil, s.mapError(ctx, err, "ComfyUIRejected", "read history")
		}
		if !sleep(ctx, 50*time.Millisecond) {
			return nil, s.cancelled(ctx, "read history")
		}
	}
	return nil, errors.New("ComfyUIResultMissing", "ComfyUI reported success but has no history entry for the prompt", "prompt_id", id, "host", s.baseURL)
}

// Pick returns the files a node produced, or ComfyUIResultMissing when it
// produced none.
func (r Result) Pick(nodeID string) ([]OutputFile, error) {
	files := r.Outputs[nodeID]
	if len(files) == 0 {
		return nil, errors.New("ComfyUIResultMissing", "the output node produced no file", "prompt_id", r.PromptID, "node_id", nodeID)
	}
	return files, nil
}
