# The catalogue

The catalogue is what commands are made of: servers to run on, generators and fixers (workflows), and groups of items. Set it up once; change it whenever you like. A job that already exists keeps the copy it made, so changing the catalogue never changes finished or waiting jobs (edit the job, or recreate the command, to pick up changes).

Every thing in the catalogue has a **code name**: letters, digits and `_`, at most 100 characters, and not one of the reserved words of the command language (`where repeat limit sample with spread on and or not in`). Code names are how commands refer to things, so they are checked when you create them.

## Servers

A server is one ComfyUI installation.

| Field | Meaning |
|---|---|
| Name | For people |
| Address | `http://host:port` of ComfyUI, for example `http://192.168.1.20:8188` |
| Code name | Used in commands: `gpu1 -$ …` |
| Jobs at once | How many jobs it runs at the same time (default 1). A card that runs one SDXL job at a time should stay at 1 |
| ComfyUI folder | Optional. Where ComfyUI is installed, as this program sees it (for example `/home/me/ComfyUI`, or the folder where its disk is mounted). When you [import LoRAs from Civitai](civitai.md) you pick a server with a folder from a list and the folder fills itself in. It is also how the program deletes a result from ComfyUI once it has its own copy (the setting `delete_comfy_outputs`, on by default, [Settings](configuration.md)): the file is removed from `output` below this folder. A server without a folder keeps its files |
| Same machine | Tick it when ComfyUI can read this program's files directly. (Input images are always uploaded to ComfyUI today, so the tick only records the fact.) |

The list shows whether each server is **online**. The program probes every server about once a second and tells you when one goes away or comes back (a message in the web page, an event for other clients). *Test* in the web page asks one server right now.

**Every server a command uses must be able to run the generator's workflow:** the checkpoints, LoRAs and custom nodes it names must be installed there. A preview warns about a LoRA that an online server does not have.

## Generators

A generator is a ComfyUI workflow that makes the main result.

**Getting the workflow.** In ComfyUI turn on *Dev mode options* in the settings, then use *Save (API Format)*. A workflow saved in the normal format does not work: it has no `class_type` for each node. The example workflows in `examples/workflows/creators/` are in the right format.

**Titles.** The program has to know which node takes the positive prompt and which the negative one. It finds nodes by the title shown on the node (`_meta.title`): rename the nodes in ComfyUI (double-click the title) to `Positive` and `Negative`, or give the titles you chose in the generator. Titles that are used must be **unique** in the workflow: two nodes titled `Positive` are refused.

**Text nodes that take both prompts.** Some text encoders, such as the Qwen Image 2.1 one (`TextEncodeQwenImage21`), have one node with a `prompt` input and a `negative_prompt` input. The program writes the positive prompt into `text` or, when the node has no `text`, into `prompt`, and the negative prompt into `negative_prompt` of the same node: give only the positive title (the dialog suggests it) and leave the negative one empty. A node with no `_meta.title` is found by its class name, which is then its title. A node that has none of these inputs makes the job fail with `InjectorFailed`, so a prompt never goes nowhere without a message. Regional prompts (`duo{red: …}`) still need an ordinary `CLIPTextEncode` with a `text` input.

**Analysis.** Paste or load the workflow and the dialog analyses it by itself, as soon as you leave the box, paste, or choose a file; there is nothing to press (`bosctl generator analyze` does the same from a terminal). The program finds what the workflow produces (image, video, 3D) and the node that saves it, reads the default width, height, steps and so on, and lists the node titles you can choose from. The fields it fills are only suggestions; you can change them. If it cannot find an output node, the workflow is refused.

| Field | Meaning |
|---|---|
| Positive and negative prompt node | Titles of the text-encode nodes that receive the prompts. **Required** for the positive one: with no node to put the prompt in, every job would run with the text already in the workflow file. If you leave it empty, the node whose title starts with `positive` (any case) is used, and a workflow with no such node is refused (`GeneratorTitleRequired`). The negative one is filled the same way from a title that starts with `negative`; leave it empty if the workflow has none, or if one node takes both prompts (see below) |
| Output type | `image`, `video`, `3d` or `unknown` |
| Output node | The node that produces the result (its title and class) |
| Output attributes | Defaults such as `width`, `height`, `steps`, `cfg`, `sampler_name`, `scheduler`, `denoise`, `seed` (image); `fps`, `num_frames` (video). Commands and items can override them |
| New random seed for every job | Each job gets a fresh random seed, recorded on the job. Needed if a command uses `repeat` without setting a seed |

What the program writes into your workflow for each job: the prompts, the seed and the attributes; LoRA loaders for the item's LoRAs; ControlNet, IP-Adapter and regional-prompt nodes for the items that carry them; and the input images (uploaded to the server first). ControlNet, IP-Adapter and region support need the matching ComfyUI custom nodes on the server (`IPAdapterAdvanced`, `ControlNetApplyAdvanced`, `ConditioningSetMask` and so on; the [specification](../SPEC.md) §12 lists them).

## Fixers

A fixer is a workflow that improves a picture that already exists: *simple upscale*, *tiled upscale*, *face repair (ADetailer-style)*, *background removal*, *img2img refinement*. Examples are in `examples/workflows/fixers/`.

A fixer needs the titles of its **image input** node (the program puts the picture there) and its **image output** node (the program takes the improved picture from there). It may also name prompt nodes and carry fixed text. *Analyze* offers the candidates.

In a command, `> upscale` runs the fixer after the generation, `> upscale on gpu2` runs it on another server, and several fixers run in the order written. The improved picture **replaces** the result.

## Groups

A group is a named list of items that play one role: `characters`, `poses`, `places`, `styles`. In a command you write the group's code name and get its items.

| Field | Meaning |
|---|---|
| Name, code name, description | |
| Tags | **Group tags** only organise the groups page (filter by a tag chip). Commands cannot see them |
| Features | What this group's items may carry: LoRAs, ControlNet, IP-Adapter, mask regions, box regions, output settings. Only the sections of the features you tick appear in the item editor |
| Thumbnail | PNG, JPEG or GIF, for the list |

Deleting a group deletes its items and their files.

You can also make a group from a text (one item per non-empty line; an item's code name and name are its line number counted from 0, its positive prompt is the line) and from Civitai ([importing LoRAs](civitai.md)).

## Items

An item is one choice in a group.

| Field | Meaning |
|---|---|
| Code name | Unique inside the group; used in `group(item)` |
| Name | For people |
| Positive and negative prompt | Text added to the job's prompts; may hold `<tmpl:name>` placeholders for [templates](commands.md#templates) |
| Item tags | Labels the language can select by: `#acrobatic`. Lower-cased when saved |
| LoRAs | `name`, strength for the model, strength for the text encoder; the name is the file as ComfyUI knows it (`styles/fox.safetensors`) |
| ControlNets | Type (`openpose`, `dwpose`, `tile`, `canny`, `midas`, `depth`, `lineart`, `softedge`, `scribble`, `normal`), an image, a strength, an optional part of the model's file name to choose it, and whether the image is already a ready-made map (a reference) |
| IP-Adapter | A reference image, weight and its model names |
| Mask regions | A PNG in flat colours on a white background; each colour becomes a named region (`red`, `blue`, …, plus `background`). The program makes the masks when you save the item and shows the names it found |
| Box regions | Rectangles `keyword width height x y` as another way to define regions |
| Output settings | An output type and attributes that apply to jobs using this item |
| Thumbnail | For the list |

When a command combines items, their prompts are joined (a single space between), LoRAs and the like are collected from all of them, and the job copies the result.

**Pictures you upload** are checked by what they contain, not by their name: thumbnails must be PNG, JPEG or GIF; reference images may also be WebP; the mask picture must be PNG, because a lossy format would add stray colours. Files bigger than `upload_limit_bytes` are refused.

## Tags

There are two separate sets of tags: **group tags** (for the groups page) and **item tags** (for commands). The tag list offers what is already in use so spellings do not drift. A tag is lower case letters, digits and `_`, at most 50 characters.

**Rename or merge**: renaming a tag changes it on every group (or item) that has it; renaming to a tag that exists merges the two. Renaming an *item* tag does not rewrite command texts. The answer lists the commands that mention the old tag, because they will fail validation the next time you edit or recreate them.
