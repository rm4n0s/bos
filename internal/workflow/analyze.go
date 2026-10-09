package workflow

import (
	"encoding/json"
	"math"
	"slices"
	"strings"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

// outputNode describes a recognised output node class (SPEC §3.2).
type outputNode struct {
	class    string
	typ      domain.OutputType
	fileType string // empty when it comes from the node's inputs
}

// outputNodes lists the recognised output nodes in the order they win when a
// workflow has several: video and 3D before images, and SaveImage before
// PreviewImage, so a video workflow that also previews a frame is still a video.
var outputNodes = []outputNode{
	{"VHS_VideoCombine", domain.OutputVideo, ""},
	{"SaveGLB", domain.Output3D, "glb"},
	{"Save3DModel", domain.Output3D, ""},
	{"SaveImage", domain.OutputImage, "png"},
	{"PreviewImage", domain.OutputImage, "png"},
	{"SaveAnimatedWEBP", domain.OutputImage, "webp"},
	{"SaveAnimatedPNG", domain.OutputImage, "apng"},
	{"Image Save", domain.OutputImage, ""},
	{"SaveImageExtended", domain.OutputImage, ""},
	{"JWImageSave", domain.OutputImage, ""},
}

// dimensionNodes are the nodes that carry the size, in priority order.
var dimensionNodes = []string{
	"EmptyLatentImage", "EmptySD3LatentImage", "EmptyLatentImageSD3",
	"EmptyMochiLatentVideo", "EmptyHunyuanLatentVideo", "EmptyLTXVLatentVideo",
	"EmptyWanLatentVideo", "EmptyCogVideoLatentVideo", "EmptyFluxLatentVideo",
	"ImageScale", "ImageResize", "LatentUpscale",
}

var imageLatentNodes = []string{"EmptyLatentImage", "EmptySD3LatentImage", "EmptyLatentImageSD3"}

var videoLatentNodes = []string{
	"EmptyMochiLatentVideo", "EmptyHunyuanLatentVideo", "EmptyLTXVLatentVideo",
	"EmptyWanLatentVideo", "EmptyCogVideoLatentVideo", "EmptyFluxLatentVideo",
}

// frameInputs are the names under which video latent nodes keep the frame count.
var frameInputs = []string{"num_frames", "video_length", "length"}

// Analysis is what a workflow produces and which of its nodes a user can pick
// (SPEC §3.2, §15.2 `POST /generators/analyze`).
type Analysis struct {
	OutputType          domain.OutputType
	Attributes          domain.OutputAttributes
	OutputNodeID        string
	OutputNodeClassType string
	OutputNodeTitle     string
	// PromptTitles are the titles of the text-encode nodes, the candidates for
	// the prompt-node fields.
	PromptTitles []string
	// LoadImageTitles and SaveImageTitles are the candidates for a fixer's
	// image-input and image-output nodes.
	LoadImageTitles []string
	SaveImageTitles []string
}

// attrOf reads a literal input as an attribute value; links and other values
// give false.
func attrOf(raw json.RawMessage) (domain.Attr, bool) {
	var a domain.Attr
	if err := a.UnmarshalJSON(raw); err != nil {
		return domain.Attr{}, false
	}
	return a, true
}

func num(a domain.Attr) float64 {
	if a.Kind == domain.AttrInt {
		return float64(a.Int)
	}
	return a.Float
}

func intAttr(v uint64) domain.Attr { return domain.Attr{Kind: domain.AttrInt, Int: v} }

// Analyze infers the output type, default attributes and output node of a
// workflow (SPEC §3.2). A workflow with no recognised output node fails with
// OutputNodeNotFound.
func Analyze(g Graph) (*Analysis, error) {
	nodes := g.Nodes()
	var found *outputNode
	var node Node
	for i := range outputNodes {
		for _, n := range nodes {
			if n.ClassType == outputNodes[i].class {
				found, node = &outputNodes[i], n
				break
			}
		}
		if found != nil {
			break
		}
	}
	if found == nil {
		return nil, errors.New("OutputNodeNotFound", "the workflow has no recognised output node", "recognised", outputNodeClasses())
	}

	a := &Analysis{
		OutputType: found.typ, OutputNodeID: node.ID, OutputNodeClassType: node.ClassType, OutputNodeTitle: node.Title,
		Attributes: domain.OutputAttributes{},
	}
	fileType := found.fileType
	if fileType == "" {
		for _, key := range []string{"extension", "format"} {
			if raw, ok := node.Inputs[key]; ok {
				var s string
				if json.Unmarshal(raw, &s) == nil && s != "" {
					fileType = strings.ToLower(s[strings.LastIndex(s, "/")+1:])
					break
				}
			}
		}
	}
	if fileType != "" {
		a.Attributes["file_type"] = domain.Attr{Kind: domain.AttrText, Text: fileType}
	}

	switch found.typ {
	case domain.OutputImage:
		analyzeDimensions(nodes, a.Attributes)
		analyzeImage(nodes, a.Attributes)
	case domain.OutputVideo:
		analyzeDimensions(nodes, a.Attributes)
		analyzeVideo(nodes, a.Attributes)
	}

	for _, n := range nodes {
		switch {
		case strings.Contains(n.ClassType, "TextEncode"):
			a.PromptTitles = append(a.PromptTitles, n.Title)
		case n.ClassType == "LoadImage":
			a.LoadImageTitles = append(a.LoadImageTitles, n.Title)
		case slices.ContainsFunc(outputNodes, func(o outputNode) bool { return o.class == n.ClassType && o.typ == domain.OutputImage }):
			a.SaveImageTitles = append(a.SaveImageTitles, n.Title)
		}
	}
	return a, nil
}

func outputNodeClasses() []string {
	out := make([]string, len(outputNodes))
	for i, o := range outputNodes {
		out[i] = o.class
	}
	return out
}

// analyzeDimensions takes width and height from the first dimension node, in
// priority order, that holds literal values for both.
func analyzeDimensions(nodes []Node, attrs domain.OutputAttributes) {
	best, bestRank := [2]uint64{}, math.MaxInt
	for _, n := range nodes {
		rank := slices.Index(dimensionNodes, n.ClassType)
		if rank < 0 || rank >= bestRank {
			continue
		}
		w, wok := attrOf(n.Inputs["width"])
		h, hok := attrOf(n.Inputs["height"])
		if wok && hok && w.Kind == domain.AttrInt && h.Kind == domain.AttrInt && w.Int > 0 && h.Int > 0 {
			best, bestRank = [2]uint64{w.Int, h.Int}, rank
		}
	}
	if bestRank != math.MaxInt {
		attrs["width"], attrs["height"] = intAttr(best[0]), intAttr(best[1])
	}
}

func analyzeImage(nodes []Node, attrs domain.OutputAttributes) {
	for _, n := range nodes {
		if slices.Contains(imageLatentNodes, n.ClassType) {
			if v, ok := attrOf(n.Inputs["batch_size"]); ok && v.Kind == domain.AttrInt && v.Int > 0 {
				attrs["batch_size"] = v
			}
		}
		if n.ClassType == classKSampler || n.ClassType == classKSamplerAdvanced {
			keys := []string{"steps", "cfg", "sampler_name", "scheduler", "denoise", "seed"}
			if n.ClassType == classKSamplerAdvanced {
				keys = []string{"steps", "cfg", "sampler_name", "scheduler"}
			}
			for _, key := range keys {
				if v, ok := attrOf(n.Inputs[key]); ok {
					attrs[key] = v
				}
			}
			if n.ClassType == classKSamplerAdvanced {
				if v, ok := attrOf(n.Inputs["noise_seed"]); ok {
					attrs["seed"] = v
				}
			}
		}
	}
}

func analyzeVideo(nodes []Node, attrs domain.OutputAttributes) {
	for _, n := range nodes {
		if n.ClassType == "VHS_VideoCombine" {
			if v, ok := attrOf(n.Inputs["frame_rate"]); ok && num(v) > 0 {
				attrs["fps"] = v
			}
		}
		if slices.Contains(videoLatentNodes, n.ClassType) {
			for _, key := range frameInputs {
				if v, ok := attrOf(n.Inputs[key]); ok && v.Kind == domain.AttrInt && v.Int > 0 {
					attrs["num_frames"] = v
					break
				}
			}
		}
	}
	fps, hasFPS := attrs["fps"]
	frames, hasFrames := attrs["num_frames"]
	if hasFPS && hasFrames {
		d := math.Round(float64(frames.Int)/num(fps)*100) / 100
		attrs["duration_seconds"] = domain.Attr{Kind: domain.AttrDecimal, Float: d}
	}
}
