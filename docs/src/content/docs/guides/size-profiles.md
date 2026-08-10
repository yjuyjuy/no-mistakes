---
title: Per-Ticket-Size Validation Profiles
description: Validate small, medium, and large changes with rigor proportional to their size using separate NM_HOME roots.
---

Not every change deserves the same validation cost.
A one-line copy fix does not need the deepest model reviewing it for an hour, and a sweeping refactor should not be validated as cheaply as a typo.

no-mistakes ships three ready-made **size profiles** - `small`, `medium`, and `large` - so a dispatcher can pick the rigor that matches a ticket's size.
Each profile is a separately initialized [`NM_HOME`](/no-mistakes/reference/environment/#nm_home) root with its own `config.yaml`.

## Why separate `NM_HOME` roots

`NM_HOME` moves **all** no-mistakes state, not just config: the gate repos, worktrees, database, socket, and daemon all live under it.
Giving each size its own root is the supported way to run genuinely separate profiles side by side, and it is the lever the profiles use.

Within one root, each profile's `config.yaml` tunes rigor two ways:

- a default model in [`agent_args_override`](/no-mistakes/reference/global-config/#agent_args_override) that every pipeline step inherits, and
- per-step promotions in [`agent_args_override_per_step`](/no-mistakes/reference/global-config/#agent_args_override_per_step) that pay for a stronger model or deeper reasoning on the steps where a mistake is most costly (chiefly the adversarial review).

This is deliberately config only.
The profiles do **not** try to fold multiple pipeline steps into one agent session; step isolation is a core pipeline invariant, not a knob to tune.

## What each profile changes

| Knob | `small` | `medium` | `large` |
| ---- | ------- | -------- | ------- |
| Default model (claude alias) | `haiku` | `sonnet` | `opus` |
| Review step model | default | promoted (`opus`) | promoted (`opus`, high effort) |
| Other promoted steps | none | none | `test`, `rebase` |
| `auto_fix` depth | shallow (1) | standard (3) | deep (5) |
| `ci_timeout` | `24h` | `168h` | `336h` |

The exact flags live in the template files under `contrib/size-profiles/`.
The Claude model aliases there are examples; edit the `agent_args_override` block to match the agent your machine actually runs (`no-mistakes doctor` shows it), and the `codex` blocks show the equivalent for a Codex agent.

## Materializing the profiles

Use the helper script from the repo root:

```sh
# Create all three profile roots and drop each config.yaml in place.
scripts/nm-size-profiles.sh init all

# Confirm each root's config loads.
scripts/nm-size-profiles.sh doctor all
```

The roots live under `${NM_PROFILES_ROOT:-~/.no-mistakes-profiles}/<size>`.
Set `NM_PROFILES_ROOT` to relocate them.

## Using a profile

Export the matching `NM_HOME`, then drive no-mistakes exactly as usual.
Each repo you validate is initialized once per profile root:

```sh
export NM_HOME="$(scripts/nm-size-profiles.sh home medium)"

# One-time per repo, per profile:
no-mistakes init

# Then validate, sized to the ticket:
no-mistakes axi run --intent "add retry to the upload client"
```

A dispatcher picks the size by choosing which `NM_HOME` to export before the run.
Because each profile is a full independent root with its own daemon and database, runs under different profiles do not share state.

## Extending or retuning

The templates are ordinary global config files, so anything in the
[Global Config Reference](/no-mistakes/reference/global-config/) is fair game.
Common edits:

- swap the model aliases for your agent, or point them at a different provider,
- add more per-step promotions (for example promote `document` for a docs-heavy change), or
- add a fourth size by dropping a new `contrib/size-profiles/<name>.config.yaml` and extending the `SIZES` list in `scripts/nm-size-profiles.sh`.
