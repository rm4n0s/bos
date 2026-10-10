# The command language

A command is one line of text. It says **where** to run, **what** to combine, how to **shape** the list, which **settings** to change, how to **split** the work, and which **fixers** to run afterwards.

```
gpu1, gpu2 -$ anime: characters * poses(~jumping) * emotions(cry) * "masterpiece"
    where not #acrobatic
    sample 20 seed 7  repeat 3
    with steps=30, cfg=6.5
    spread blocks
    > upscale on gpu2
```

Line breaks and extra spaces do not matter (`a*b`, `a * b` and `a   *   b` are the same), except inside quotes. The clauses must come in this order: `where`, then `limit` or `sample`, then `repeat`, then `with`, then `spread`, then the fixers.

**Names complete as you type.** In the web page, the box under the command offers the code names that fit where the cursor is: servers at the start, the generator after `-$`, groups after the colon and after `*`, the items of a group inside its parentheses, tags after `#`, and fixers after `>`. Press `Tab` to take the first one, `↑` `↓` to choose and `Tab` or `Enter` to take it, `Esc` to close the list, or click a name. Nothing is offered inside quotes or where you make up a name yourself (the tag of a template, the keyword of a region).

**Always preview first.** In the web page the preview updates as you type and *Save* stays disabled until the command is valid; in a terminal use `bosctl command preview "…"`. It tells you how many jobs the command makes and what is wrong, with the offending part marked, and changes nothing.

## The shortest command

```
gpu1 -$ anime: characters
```

`gpu1` is a server's code name, `-$` separates where from what, `anime` is a generator, and after the colon come the **selections**: here every item of the group `characters`. One job for each item.

## Choosing items

| You write | You get |
|---|---|
| `characters` | every item of the group, in the order they were created |
| `characters(alice, bob)` | those items |
| `characters(~alice)` | every item **except** alice (`~` before every excluded name: `characters(~alice, ~bob)`) |
| `poses(run%)` | every item whose code name starts with `run` (`%` is any run of characters, also none) |
| `poses(pose_?)` | `pose_` plus exactly one character |
| `poses(#acrobatic)` | every item tagged `acrobatic` |
| `"masterpiece, best quality"` | one anonymous item that only adds positive text |
| `!"blurry, lowres"` | one anonymous item that only adds negative text |

A filter is all-include or all-exclude, never mixed (`poses(standing, ~jumping)` is an error). **Every name or pattern in a filter must match something**, so a typo is caught instead of silently making fewer jobs.

## Combining

**`*` multiplies.** `characters * poses` makes one job for every character with every pose. The **last term changes fastest**: with 2 characters and 3 poses the order is `a1b1, a1b2, a1b3, a2b1, a2b2, a2b3`, always the same for the same data.

```
gpu1 -$ anime: characters * poses(~jumping) * emotions(cry)      3 × 3 × 1 = 9 jobs
```

**`&&` merges** the items of several groups into one list (duplicates kept): `characters && anime` is the characters followed by the anime characters, as one term. **`||` pairs**: the first of one with the first of the other, the second with the second, up to the shorter list; each pair becomes one item that carries both prompts and both sets of LoRAs. You cannot mix `&&` and `||` in one term.

```
gpu1 -$ anime: "red hair" && "blue hair" * characters           2 × 3 = 6 jobs
gpu1 -$ anime: characters || emotions                            3 jobs (alice+happy, bob+sad, carol+cry)
```

## Templates

An item's prompt can have a placeholder `<tmpl:subject>`. A template term fills it from other groups:

```
gpu1 -$ anime: scene[subject: characters(alice, bob)]
```

For each item of `scene`, for each choice in the body, the placeholder is replaced with that choice's text. Identical results are removed. Several tags multiply: `scene[subject: characters, mood: emotions]`.

## Regions

A group whose items define regions (masks or boxes) can be prompted region by region:

```
gpu1 -$ anime: duo{red: characters(alice), blue: characters(bob, carol)} * style
```

The names in braces must be **exactly** the regions the group's items define. Each region gets its own prompt, and the choices multiply. At most one region term per command, never inside a template or a merge, and a region group takes no filter.

## Shaping the list

After the product is made it passes through these steps, in this order.

**`where`** keeps only combinations that satisfy a condition:

| You write | Keeps combinations that… |
|---|---|
| `where #acrobatic` | contain an item tagged `acrobatic` |
| `where characters = alice` | contain alice |
| `where poses in (standing, sitting)` | contain one of those |
| `where not #acrobatic` | …do not |
| `where characters = alice and poses = running` | both (also `or`, and parentheses) |

Every group named in a `where` must be part of the command. If nothing is left, the command is invalid.

**`limit 20`** keeps the first 20 combinations. **`sample 10 seed 7`** keeps 10 chosen at random, in the same order as before; the same seed gives the same choice. If you leave the seed out, the program picks one and stores it, so *recreating* the jobs gives the same ones. The number counts **combinations**.

**`repeat 4`** makes 4 jobs of each remaining combination. `limit 20 repeat 4` is 80 jobs. A repeat needs the seed to vary: either set a seed (`with seed=42`, which gives 42, 43, 44, … to the repeats) or use a generator that has a new random seed for every job. Otherwise you would make the same picture four times, and the command is refused.

## Settings

`with` changes generation settings for every job of the command: `with steps=30, cfg=6.5, sampler_name=euler`.

| For | Keys |
|---|---|
| image | `width height steps batch_size seed` (whole numbers), `cfg denoise` (numbers), `sampler_name scheduler file_type` (words) |
| video | `width height num_frames`, `fps duration_seconds`, `file_type` |
| 3D | `file_type` |

A key the generator's output type does not have is an error. A command's `with` beats an item's settings, which beat the generator's. The seed each job used is recorded on the job.

## Several servers

List more than one target: `gpu1, gpu2 -$ anime: …`. Without anything else the servers **share the work as they become free** (`dynamic`): each takes the next waiting job, so a faster card does more. You can also decide in advance:

| `spread …` | Result |
|---|---|
| `even` | Jobs go round the servers in turn: `gpu1, gpu2, gpu1, gpu2, …` |
| `blocks` | The first half to `gpu1`, the rest to `gpu2`: consecutive jobs share earlier terms, so a server reuses the LoRAs it has loaded |
| `by characters` | Every job with the same character goes to the same server |
| `… pinned` | The plan is binding: a job waits for its server even if another is free |

Weights: `gpu1=3, gpu2=1 -$ … spread blocks` gives `gpu1` three quarters (12 jobs: 1–9 on `gpu1`, 10–12 on `gpu2`). Weights need `even`, `blocks` or `by`. `by g` needs exactly one plain group term `g`. Static plans are decided when the command is **added**, so you can see them before running.

Every server must have what the generator needs (models, LoRAs, custom nodes).

## Fixers

Everything after `>` is a fixer step, in order:

```
gpu1 -$ anime: characters > upscale > face
gpu1 -$ anime: characters > upscale on gpu2 > face
```

A step runs on the server that made the picture, or on the server named after `on`. Each step replaces the result with the improved picture.

## Reserved words and names

`where repeat limit sample with spread on and or not in` cannot be used as code names. `seed dynamic even blocks by pinned` only have meaning where the grammar expects them. Names are letters, digits and `_`.

## Common mistakes

| You wrote | Problem |
|---|---|
| `gpu1 - anime: characters` | `-$` is the separator |
| `characters && poses \|\| emotions` | `&&` and `\|\|` cannot be mixed |
| `poses(standing, ~jumping)` | include and exclude cannot be mixed in one filter |
| `characters(nobody)` | no such item |
| `poses(#nosuchtag)` | the tag matches nothing |
| `characters where emotions = sad` | `emotions` is not part of the command |
| `characters repeat 3` | needs a seed or a generator with a random seed |
| `gpu1, gpu1 -$ …` | the same server twice |
| `gpu1=2, gpu2 -$ …` | weights need `spread even`, `blocks` or `by` |
| `characters limit 5 sample 3` | one of `limit` and `sample`, not both |
| `characters repeat 3 where …` | wrong order of clauses |

The preview shows each problem with a rule name (`S1`, `T2`, `R1`, …), the position in the text and a sentence. The [specification](../SPEC.md) §6 has the exact grammar and every rule.
