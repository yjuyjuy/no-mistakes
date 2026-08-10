# Size profile config templates

Global `config.yaml` templates for the small / medium / large per-ticket-size
validation profiles.

Materialize them into dedicated `NM_HOME` roots with:

```sh
scripts/nm-size-profiles.sh init all
scripts/nm-size-profiles.sh doctor all
```

The full model, rationale, and usage live in
`docs/src/content/docs/guides/size-profiles.md`. The model aliases in these
templates are examples; edit the `agent_args_override` block to match the agent
your machine runs (`no-mistakes doctor` shows it).
