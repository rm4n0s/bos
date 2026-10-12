// The LoRA injector adds LoRA nodes to a ComfyUI workflow in API format. A
// checkpoint workflow gets a LoraLoader per LoRA, patching MODEL and CLIP; a
// split workflow (a UNETLoader and a separate CLIPLoader, as Z-Image and Flux
// use) gets a LoraLoaderModelOnly, patching MODEL only. The LoRAs are chained,
// each one reading the output of the one before.
//
// Every failure is an InjectorFailed error (SPEC §17.2).

package workflow

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// failed is an InjectorFailed error with the reason in its metadata.
func failed(reason string, kv ...any) *errors.Error {
	return errors.New("InjectorFailed", "cannot inject LoRA: "+reason, append([]any{"injector", "LoRA", "reason", reason}, kv...)...)
}

// failedBy is failed for an error of the standard library.
func failedBy(err error, reason string, kv ...any) *errors.Error {
	return errors.NewErr("InjectorFailed", err).SetMetadata(append([]any{"injector", "LoRA", "reason", reason}, kv...)...)
}

// nodeData is a helper for reading fields out of a raw workflow node.
type nodeData struct {
	ClassType string                     `json:"class_type"`
	Inputs    map[string]json.RawMessage `json:"inputs"`
	Meta      map[string]interface{}     `json:"_meta,omitempty"`
}

// WorkflowType describes the kind of loader found in a ComfyUI workflow.
type WorkflowType string

// The kinds of workflow.
const (
	WorkflowCheckpoint WorkflowType = "checkpoint"
	WorkflowSplit      WorkflowType = "split"
	WorkflowUnknown    WorkflowType = "unknown"
)

// LoRAInjector injects LoRA nodes into a ComfyUI workflow.
type LoRAInjector struct {
	workflow   map[string]json.RawMessage
	nextNodeID int
}

// NewLoRAInjector creates a new injector from a workflow map. The map is
// edited in place.
func NewLoRAInjector(workflow map[string]json.RawMessage) *LoRAInjector {
	inj := &LoRAInjector{workflow: workflow}
	inj.nextNodeID = inj.getNextNodeID()
	return inj
}

// getNextNodeID returns the next available numeric node ID.
// If no numeric IDs exist, returns 1.
func (inj *LoRAInjector) getNextNodeID() int {
	max := 0
	for id := range inj.workflow {
		n, err := strconv.Atoi(id)
		if err != nil {
			continue // skip non-numeric IDs like "4a"
		}
		if n > max {
			max = n
		}
	}
	return max + 1
}

// allocNodeID returns a new unique node ID string and advances the counter.
// It checks the workflow map to guarantee no collision even if non-numeric
// IDs happen to equal the candidate string.
func (inj *LoRAInjector) allocNodeID() string {
	for {
		candidate := strconv.Itoa(inj.nextNodeID)
		inj.nextNodeID++
		if _, taken := inj.workflow[candidate]; !taken {
			return candidate
		}
	}
}

// parseNode decodes a raw JSON node into a nodeData struct.
func parseNode(raw json.RawMessage) (nodeData, error) {
	var nd nodeData
	if err := json.Unmarshal(raw, &nd); err != nil {
		return nd, err
	}
	return nd, nil
}

// DetectWorkflowType inspects the workflow and returns its type.
func (inj *LoRAInjector) DetectWorkflowType() WorkflowType {
	hasCheckpoint := false
	hasUNET := false

	for _, raw := range inj.workflow {
		nd, err := parseNode(raw)
		if err != nil {
			continue
		}
		if strings.Contains(nd.ClassType, "CheckpointLoader") {
			hasCheckpoint = true
		}
		if nd.ClassType == "UNETLoader" {
			hasUNET = true
		}
	}

	if hasCheckpoint {
		return WorkflowCheckpoint
	}
	if hasUNET {
		return WorkflowSplit
	}
	return WorkflowUnknown
}

// findModelNodes returns node IDs of all CheckpointLoader nodes.
func (inj *LoRAInjector) findModelNodes() []string {
	var ids []string
	for id, raw := range inj.workflow {
		nd, err := parseNode(raw)
		if err != nil {
			continue
		}
		if strings.Contains(nd.ClassType, "CheckpointLoader") {
			ids = append(ids, id)
		}
	}
	return ids
}

// findUNETLoader returns the node ID of the UNETLoader, or "" if not found.
func (inj *LoRAInjector) findUNETLoader() string {
	for id, raw := range inj.workflow {
		nd, err := parseNode(raw)
		if err != nil {
			continue
		}
		if nd.ClassType == "UNETLoader" {
			return id
		}
	}
	return ""
}

// findCLIPLoader returns the node ID of the CLIPLoader, or "" if not found.
func (inj *LoRAInjector) findCLIPLoader() string {
	for id, raw := range inj.workflow {
		nd, err := parseNode(raw)
		if err != nil {
			continue
		}
		if nd.ClassType == "CLIPLoader" {
			return id
		}
	}
	return ""
}

// loraRef represents a ComfyUI node reference: [sourceID, outputIndex].
type loraRef struct {
	SourceID    string
	OutputIndex int
}

// parseInputRef tries to decode a raw input value as [nodeID, outputIndex].
// ComfyUI node IDs may be encoded as either a JSON string ("4") or a JSON
// number (4), so we try both. Returns (ref, true) on success.
func parseInputRef(raw json.RawMessage) (loraRef, bool) {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil || len(arr) < 2 {
		return loraRef{}, false
	}

	// Try the node ID as a string first, then fall back to number.
	var srcID string
	if err := json.Unmarshal(arr[0], &srcID); err != nil {
		// Maybe it's a plain JSON number — convert to string.
		var n json.Number
		if err2 := json.Unmarshal(arr[0], &n); err2 != nil {
			return loraRef{}, false
		}
		srcID = n.String()
	}

	var outIdx int
	if err := json.Unmarshal(arr[1], &outIdx); err != nil {
		return loraRef{}, false
	}
	return loraRef{SourceID: srcID, OutputIndex: outIdx}, true
}

// encodeInputRef marshals an loraRef back to [sourceID, outputIndex].
func encodeInputRef(ref loraRef) (json.RawMessage, error) {
	b, err := json.Marshal([]interface{}{ref.SourceID, ref.OutputIndex})
	if err != nil {
		return nil, failedBy(err, "cannot encode a link", "node", ref.SourceID)
	}
	return b, nil
}

// nodeInputRef groups a node ID, its input key, and the parsed ref.
type nodeInputRef struct {
	NodeID   string
	InputKey string
	Ref      loraRef
}

// findNodesUsingModel returns all (nodeID, inputKey) pairs whose input
// references sourceID (any output index).
func (inj *LoRAInjector) findNodesUsingModel(sourceID string) []nodeInputRef {
	var results []nodeInputRef
	for id, raw := range inj.workflow {
		nd, err := parseNode(raw)
		if err != nil {
			continue
		}
		for key, val := range nd.Inputs {
			ref, ok := parseInputRef(val)
			if ok && ref.SourceID == sourceID {
				results = append(results, nodeInputRef{NodeID: id, InputKey: key, Ref: ref})
			}
		}
	}
	return results
}

// findNodesUsingOutput returns all (nodeID, inputKey) pairs whose input
// references sourceID at the given outputIndex.
func (inj *LoRAInjector) findNodesUsingOutput(sourceID string, outputIndex int) []nodeInputRef {
	var results []nodeInputRef
	for id, raw := range inj.workflow {
		nd, err := parseNode(raw)
		if err != nil {
			continue
		}
		for key, val := range nd.Inputs {
			ref, ok := parseInputRef(val)
			if ok && ref.SourceID == sourceID && ref.OutputIndex == outputIndex {
				results = append(results, nodeInputRef{NodeID: id, InputKey: key, Ref: ref})
			}
		}
	}
	return results
}

// updateNodeInput sets a single input key on an already-stored workflow node.
func (inj *LoRAInjector) updateNodeInput(nodeID, inputKey string, value json.RawMessage) error {
	raw, ok := inj.workflow[nodeID]
	if !ok {
		return failed("a node to edit is missing", "node", nodeID)
	}

	// Unmarshal into a generic map to preserve all fields.
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &generic); err != nil {
		return failedBy(err, "a node cannot be decoded", "node", nodeID)
	}

	// Unmarshal inputs sub-map.
	var inputs map[string]json.RawMessage
	if rawInputs, ok := generic["inputs"]; ok {
		if err := json.Unmarshal(rawInputs, &inputs); err != nil {
			return failedBy(err, "the inputs of a node cannot be decoded", "node", nodeID)
		}
	} else {
		inputs = make(map[string]json.RawMessage)
	}

	inputs[inputKey] = value

	newInputs, err := json.Marshal(inputs)
	if err != nil {
		return failedBy(err, "the inputs of a node cannot be encoded", "node", nodeID)
	}
	generic["inputs"] = newInputs

	newRaw, err := json.Marshal(generic)
	if err != nil {
		return failedBy(err, "a node cannot be encoded", "node", nodeID)
	}
	inj.workflow[nodeID] = newRaw
	return nil
}

// LoRAConfig describes a single LoRA to inject.
type LoRAConfig struct {
	Name          string
	StrengthModel float64
	StrengthCLIP  float64
}

// AddLoRA adds a single LoRA node, auto-detecting the workflow type.
// insertAfterNode may be "" to auto-detect.
// Returns the new node's ID.
func (inj *LoRAInjector) AddLoRA(cfg LoRAConfig, insertAfterNode string) (string, error) {
	switch inj.DetectWorkflowType() {
	case WorkflowSplit:
		return inj.addLoRASplitWorkflow(cfg, insertAfterNode)
	case WorkflowCheckpoint:
		return inj.addLoRACheckpointWorkflow(cfg, insertAfterNode)
	default:
		return "", failed("could not detect the workflow type: expected a CheckpointLoader or a UNETLoader node")
	}
}

// addLoRASplitWorkflow handles Z-Image / Flux workflows.
func (inj *LoRAInjector) addLoRASplitWorkflow(cfg LoRAConfig, insertAfterNode string) (string, error) {
	nodeID := inj.allocNodeID()

	if insertAfterNode == "" {
		insertAfterNode = inj.findUNETLoader()
		if insertAfterNode == "" {
			return "", failed("no UNETLoader found in the workflow")
		}
	}

	if _, ok := inj.workflow[insertAfterNode]; !ok {
		return "", failed("the node to insert after is not in the workflow", "node", insertAfterNode)
	}

	// Collect downstream connections BEFORE inserting the new node.
	downstream := inj.findNodesUsingOutput(insertAfterNode, 0)

	// Build the LoRA node.
	loraNode := map[string]interface{}{
		"class_type": "LoraLoaderModelOnly",
		"_meta":      map[string]string{"title": "Load LoRA - " + cfg.Name},
		"inputs": map[string]interface{}{
			"lora_name":      cfg.Name,
			"strength_model": cfg.StrengthModel,
			"model":          []interface{}{insertAfterNode, 0},
		},
	}

	raw, err := json.Marshal(loraNode)
	if err != nil {
		return "", failedBy(err, "the LoRA node cannot be encoded", "lora", cfg.Name)
	}
	inj.workflow[nodeID] = raw

	// Redirect downstream model connections to the new LoRA output.
	for _, d := range downstream {
		if d.NodeID == nodeID {
			continue
		}
		ref, err := encodeInputRef(loraRef{SourceID: nodeID, OutputIndex: 0})
		if err != nil {
			return "", err
		}
		if err := inj.updateNodeInput(d.NodeID, d.InputKey, ref); err != nil {
			return "", err
		}
	}

	return nodeID, nil
}

// addLoRACheckpointWorkflow handles standard SD / SDXL workflows.
func (inj *LoRAInjector) addLoRACheckpointWorkflow(cfg LoRAConfig, insertAfterNode string) (string, error) {
	nodeID := inj.allocNodeID()

	if insertAfterNode == "" {
		modelNodes := inj.findModelNodes()
		if len(modelNodes) == 0 {
			return "", failed("no checkpoint loader found in the workflow")
		}
		insertAfterNode = modelNodes[0]
	}

	if _, ok := inj.workflow[insertAfterNode]; !ok {
		return "", failed("the node to insert after is not in the workflow", "node", insertAfterNode)
	}

	// Collect downstream connections BEFORE inserting the new node.
	downstream := inj.findNodesUsingModel(insertAfterNode)

	// Build the LoRA node.
	loraNode := map[string]interface{}{
		"class_type": "LoraLoader",
		"_meta":      map[string]string{"title": "Load LoRA - " + cfg.Name},
		"inputs": map[string]interface{}{
			"lora_name":      cfg.Name,
			"strength_model": cfg.StrengthModel,
			"strength_clip":  cfg.StrengthCLIP,
			"model":          []interface{}{insertAfterNode, 0},
			"clip":           []interface{}{insertAfterNode, 1},
		},
	}

	raw, err := json.Marshal(loraNode)
	if err != nil {
		return "", failedBy(err, "the LoRA node cannot be encoded", "lora", cfg.Name)
	}
	inj.workflow[nodeID] = raw

	// Redirect model (0) and CLIP (1) connections; leave VAE (2) alone.
	for _, d := range downstream {
		if d.NodeID == nodeID {
			continue
		}
		if d.Ref.OutputIndex == 0 || d.Ref.OutputIndex == 1 {
			ref, err := encodeInputRef(loraRef{SourceID: nodeID, OutputIndex: d.Ref.OutputIndex})
			if err != nil {
				return "", err
			}
			if err := inj.updateNodeInput(d.NodeID, d.InputKey, ref); err != nil {
				return "", err
			}
		}
	}

	return nodeID, nil
}

// AddMultipleLoRAs injects a sequence of LoRA nodes, chaining them together.
// insertAfterNode may be "" to auto-detect. A name without ".safetensors" gets
// it. On failure it returns the ids created so far with the error.
func (inj *LoRAInjector) AddMultipleLoRAs(loras []LoRAConfig, insertAfterNode string) ([]string, error) {
	var createdIDs []string
	current := insertAfterNode

	for _, cfg := range loras {
		name := cfg.Name
		if !strings.Contains(name, ".safetensors") {
			name += ".safetensors"
		}
		cfg.Name = name

		id, err := inj.AddLoRA(cfg, current)
		if err != nil {
			return createdIDs, err
		}
		createdIDs = append(createdIDs, id)
		current = id // chain: next LoRA attaches to this one's output
	}

	return createdIDs, nil
}

// WorkflowInfo summarises the current state of the workflow.
type WorkflowInfo struct {
	WorkflowType      WorkflowType `json:"workflow_type"`
	NodeCount         int          `json:"node_count"`
	NextNodeID        int          `json:"next_node_id"`
	LoRAType          string       `json:"lora_type,omitempty"`
	UNETLoader        string       `json:"unet_loader,omitempty"`
	CLIPLoader        string       `json:"clip_loader,omitempty"`
	CheckpointLoaders []string     `json:"checkpoint_loaders,omitempty"`
}

// GetWorkflowInfo returns a summary of the current workflow state.
func (inj *LoRAInjector) GetWorkflowInfo() WorkflowInfo {
	wt := inj.DetectWorkflowType()
	info := WorkflowInfo{
		WorkflowType: wt,
		NodeCount:    len(inj.workflow),
		NextNodeID:   inj.nextNodeID,
	}
	switch wt {
	case WorkflowSplit:
		info.UNETLoader = inj.findUNETLoader()
		info.CLIPLoader = inj.findCLIPLoader()
		info.LoRAType = "LoraLoaderModelOnly"
	case WorkflowCheckpoint:
		info.CheckpointLoaders = inj.findModelNodes()
		info.LoRAType = "LoraLoader"
	case WorkflowUnknown:
	}
	return info
}

// Save writes the workflow to a JSON file at outputPath.
func (inj *LoRAInjector) Save(outputPath string) error {
	b, err := json.MarshalIndent(inj.workflow, "", "  ")
	if err != nil {
		return failedBy(err, "the workflow cannot be encoded")
	}
	if err := os.WriteFile(outputPath, b, 0o600); err != nil {
		return errors.NewErr("FileStoreFailure", err).SetMetadata("path", outputPath, "operation", "write")
	}
	return nil
}

// GetWorkflow returns a shallow copy of the workflow map.
// Callers cannot accidentally mutate the injector's internal state by
// writing new keys to the returned map.
func (inj *LoRAInjector) GetWorkflow() map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(inj.workflow))
	for k, v := range inj.workflow {
		out[k] = v
	}
	return out
}
