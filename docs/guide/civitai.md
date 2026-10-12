# Importing LoRAs from Civitai

The program can download LoRAs from [Civitai](https://civitai.com) and turn them into a group, ready to use in commands.

## Set it up

You need a Civitai **API key** (in your Civitai account settings). Open **Settings** in the web page, paste it into *Civitai → API key*, press *Save* and then *Restart now*. That is all: the folder where downloads are kept is already set (`civitai` next to the database). Without the key, an import is refused with `CivitaiNotConfigured` and nothing is attempted. (If your Civitai account lives on another domain, set *Domain* there too.)

The program **uploads** each LoRA to the ComfyUI server you choose, into its `loras` folder, through the [comfyui-model-crud](catalogue.md#servers) extension that every server runs. The server must be online, and the program does not need to see its files: ComfyUI can be on another machine.

## Import a group

*Groups → Import from Civitai* (or `bosctl group import-civitai`, or `POST /api/v1/groups/import-civitai`). You give:

- a name and a code name for the new group;
- the Civitai model ids, one per line, each with optional strengths for the model and the text encoder (default 1). Without more, the newest version of the model is taken; to take another, write `/` and the version id after the model id (`123456/789012`, in the API `version_id`);
- the **server** to upload the LoRAs to (in the API and `bosctl`, its code name: `server`, `--upload-to`), and a **sub-folder** of its `loras` folder to keep these LoRAs in (for example `civitai`). A server that is offline or unknown is refused before anything is downloaded.

The group is created at once, empty. (Imported LoRAs show up on the [LoRAs page](catalogue.md#loras), where you can also delete one from all servers.) The downloads then go on **in the background**, one model after another with a pause between downloads (15 seconds by default, as Civitai asks for politeness; none after a model that was already downloaded). For each model you get a message, and an **item** appears in the group:

- code name: the model id (`123456`, so you write `loras(123456)`);
- name: the model's name on Civitai;
- positive prompt: the trigger words, separated by blank lines;
- LoRA: `<sub-folder>/<id>.safetensors` with the strengths you gave.

A version you chose is a file and an item of its own, so it can sit next to the newest version of the same model: the code name is `<id>_<version>` (`loras(123456_789012)`) and the file `<id>-<version>.safetensors`.

If one model fails (not found, rate limited, a bad file), you get a message for it and the next model goes on. Only one import runs at a time; a second waits.

A model that was downloaded before is taken from the cache (`data/civitai`) and not downloaded again.

## Add one model to an existing group

*Items → Add from Civitai* (or `bosctl item add-civitai GROUP --model ID [--version VERSION] …`) adds one model as an item of a group you already have. If the model is already an item of that group (same code name) you are told.

## Good to know

- The cache holds `<id>.safetensors` and `<id>.json` (a version you chose is kept in `versions/<version>` below it) (the model's name, trigger words, tags and description).
- The download uses the newest version of the model and its main `.safetensors` file; the file's checksum is verified.
- A LoRA that is already on the server with the same size is not sent again; one with another size is replaced.
- ComfyUI may need to reload its model list (refresh its page) before it offers a new LoRA. When you preview a command, the program asks each server for its LoRAs, so a LoRA a server lacks is flagged.
- Nothing is sent to Civitai but your requests for the models you ask for.
