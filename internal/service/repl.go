package service

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// replItems is the shape of the item list of a REPL run (SPEC §8.6).
var replItems = regexp.MustCompile(`^(\w+\(\w+\))(,\w+\(\w+\))*$`)

// replItemRef picks the group and the item out of one `group(item)`.
var replItemRef = regexp.MustCompile(`^(\w+)\((\w+)\)$`)

// ReplService runs one ad-hoc job without a project (SPEC §8.6).
type ReplService struct{ base }

// ReplInput is what a client sends to run the REPL.
type ReplInput struct {
	// Server and Generator are code names.
	Server    string
	Generator string
	// Positive and Negative are appended to the prompts of the items.
	Positive string
	Negative string
	// Items is written `group(item),group(item)`; it may be empty.
	Items string
	LoRAs []domain.LoRA
}

// replResult is the fixed name of the result of the REPL job.
const replResult = "repl"

// Run replaces the previous REPL job and its result with a new one and queues
// it on the chosen server. Item prompts, LoRAs, ControlNets and IP-Adapters
// are concatenated in the order written, then the user's text is appended.
func (s *ReplService) Run(ctx context.Context, in ReplInput) (*domain.Job, error) {
	items := strings.TrimSpace(in.Items)
	if items != "" && !replItems.MatchString(items) {
		return nil, errors.New("ReplInvalid", "items must be written group(item),group(item)", "items", in.Items)
	}
	for _, l := range in.LoRAs {
		if strings.TrimSpace(l.Name) == "" {
			return nil, errors.New("ReplInvalid", "every LoRA needs a name")
		}
	}
	r := s.Store.Repos()
	server, err := r.Servers().GetByCode(ctx, in.Server)
	if err != nil {
		return nil, err
	}
	gen, err := r.Generators().GetByCode(ctx, in.Generator)
	if err != nil {
		return nil, err
	}
	if prev, err := r.Jobs().GetRepl(ctx); err == nil && prev.Status == domain.StatusProcessing {
		return nil, errors.New("JobBusy", "the previous REPL job is running; wait for it to end", "job_id", prev.ID)
	}

	job := &domain.Job{
		Status: domain.StatusIdle, Pinned: true, PreferredServerID: &server.ID, RegionIndex: -1,
		GeneratorCodeName: gen.CodeName, OutputType: gen.OutputType, OutputAttributes: cloneAttrs(gen.OutputAttributes),
		CommandText: "repl " + in.Generator + " on " + in.Server,
	}
	var positive, negative []string
	if items != "" {
		job.CommandText += " " + items
		for _, ref := range strings.Split(items, ",") {
			m := replItemRef.FindStringSubmatch(ref)
			group, err := r.Groups().GetByCode(ctx, m[1])
			if err != nil {
				return nil, err
			}
			item, err := s.item(ctx, r, group.ID, m[1], m[2])
			if err != nil {
				return nil, err
			}
			job.Items = append(job.Items, domain.UsedItem{GroupID: group.ID, GroupCode: group.CodeName, ItemID: item.ID, ItemCode: item.CodeName})
			positive = append(positive, item.PositivePrompt)
			negative = append(negative, item.NegativePrompt)
			job.LoRAs = append(job.LoRAs, item.LoRAs...)
			job.ControlNets = append(job.ControlNets, item.ControlNets...)
			if item.IPAdapter != nil {
				job.IPAdapters = append(job.IPAdapters, *item.IPAdapter)
			}
			// The last item that defines both a type and attributes wins (SPEC §7.1).
			if item.OutputType != "" && len(item.OutputTypeAttributes) > 0 {
				job.OutputType, job.OutputAttributes = item.OutputType, cloneAttrs(item.OutputTypeAttributes)
			}
		}
	}
	job.PositivePrompt = joinPrompt(append(positive, in.Positive))
	job.NegativePrompt = joinPrompt(append(negative, in.Negative))
	job.LoRAs = append(job.LoRAs, in.LoRAs...)
	if seed, ok := job.OutputAttributes["seed"]; ok && seed.Kind == domain.AttrInt && !gen.HasRandomSeed {
		n := seed.Int
		job.Seed = &n
	}
	job.ResultPath = filepath.Join(s.Dirs.Results, replResult, replResult+"."+fileExtension(string(job.OutputType), job.OutputAttributes))

	stale, err := r.Jobs().ReplaceRepl(ctx, job)
	if err != nil {
		return nil, err
	}
	// The result has a fixed name, so the old job's file is the new job's
	// path; the new job has not run yet, so nothing of it is lost.
	s.removeFiles(stale)
	if _, err := r.Jobs().QueueJob(ctx, job.ID); err != nil {
		return nil, err
	}
	s.wake()
	return r.Jobs().Get(ctx, job.ID)
}

// Last returns the previous REPL job, or JobNotFound.
func (s *ReplService) Last(ctx context.Context) (*domain.Job, error) {
	return s.Store.Repos().Jobs().GetRepl(ctx)
}

// item finds an item of a group by its code name.
func (s *ReplService) item(ctx context.Context, r Repos, group domain.ID, groupCode, code string) (*domain.Item, error) {
	all, err := r.Items().AllByGroup(ctx, group)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].CodeName == code {
			return &all[i], nil
		}
	}
	return nil, errors.New("ItemNotFound", "the group has no such item", "group", groupCode, "item", code)
}

// joinPrompt joins the non-empty parts with one space.
func joinPrompt(parts []string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

func cloneAttrs(a domain.OutputAttributes) domain.OutputAttributes {
	out := make(domain.OutputAttributes, len(a))
	for k, v := range a {
		out[k] = v
	}
	return out
}
