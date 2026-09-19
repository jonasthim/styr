# Pipelines

v0.5 chains agents: a **pipeline** is a small YAML DAG of steps, each one a template run whose
structured report feeds the steps after it. A list a step's report carries fans out into N
parallel steps, a failed step retries within its own budget, and the run page shows the whole
graph live with per-step reports and a link to each step's own session transcript. This doc
covers the YAML format, validation, execution semantics and how to start a pipeline;
`internal/pipelines` (`def.go` the parser/validator, `executor.go`/`advance.go` the executor,
`view.go` the run-page read model) is the source of truth if anything here and the code
disagree.

## Concepts

- **Pipeline.** A name, a workspace and a YAML definition (`pipelines` table). Every step's
  template must belong to the pipeline's own workspace. Pipelines can be personal or shared
  (owner `null`, admin-managed), the same visibility rule templates, triggers and schedules use.
  Manage them under **Pipelines** (`/pipelines`, rail item).
- **Pipeline run.** One execution of a pipeline (`pipeline_runs` table): its own input, state
  (`running`/`success`/`failed`/`cancelled`/`timeout`), start/finish time and summed cost. A
  pipeline run is not a `runs` row — it owns several of them, one per step attempt.
- **Step run.** One node's execution within a pipeline run (`step_runs` table): which step id,
  which attempt, which fan-out index and item (for a `foreach` node), the `runs` row it started
  (once started), its state (`pending`/`running`/`success`/`failed`/`skipped`/`cancelled`), its
  report and the worktree its session landed in. A retried attempt of the same node is a new
  `step_runs` row (same `step_id`/`index_in_fanout`, `attempt` incremented) rather than mutating
  the failed one, so every attempt's report stays on its own row.

## YAML format

```yaml
name: fix-ci
workspace: styr            # workspace name; every step's template must use this workspace
timeout: 2h
steps:
  - id: triage
    template: "CI failure triage"          # template name (or template_id)
    with: { alert: "{{ .payload.title }}" } # extra vars rendered from the pipeline input
  - id: fix
    needs: [triage]
    template: "Apply fix"
    with: { plan: "{{ .steps.triage.report.proposed_action }}" }
    worktree: own
    retries: 1
  - id: verify
    needs: [fix]
    template: "Run tests and report"
    worktree: shared                        # continue in fix's worktree
  - id: review-each
    needs: [triage]
    foreach: "{{ .steps.triage.report.files }}"   # list → N parallel step-runs, item as .item
    template: "Review file"
```

### Top-level fields

| Field | Required | Notes |
|---|---|---|
| `name` | yes | The pipeline's display name (also settable independently via `POST`/`PATCH /pipelines`'s own `name` field — the yaml's `name` is not currently cross-checked against it). |
| `workspace` | no | The workspace name every step's template must belong to. If set, it must match the pipeline's actual `workspace_id` (by name) or validation fails; if omitted, no cross-check is done. |
| `timeout` | no | A Go duration (`2h`, `90m`). Defaults to **2 hours** (`pipelines.DefaultTimeout`) when omitted; an unparsable value falls back to the default and is reported as a validation problem. Capped at **24 hours** (`pipelines.MaxTimeout`) — a bigger value fails validation. |
| `steps` | yes | The DAG's nodes, defined below. At most **20 steps** (`pipelines.MaxSteps`). |

### Step fields

| Field | Required | Notes |
|---|---|---|
| `id` | yes | Unique within the pipeline. |
| `template` | yes | A template name or a template id, resolved against the pipeline's workspace (matched by name first, then id). |
| `needs` | no | A list of earlier step ids this step depends on. Every id must be defined **before** this step in the yaml (forward references are rejected) and the graph must be acyclic. |
| `with` | no | A map of extra template variables, each value a Go `text/template` string rendered against the step's vars (see "Template variables" below) before the step's template renders. |
| `worktree` | no | `""`/`own` (default, a fresh worktree from the workspace's base branch) or `shared` (reuse a dependency's worktree — see "Worktree modes" below). |
| `retries` | no | How many extra attempts a failed step gets, `0`–`3` (`pipelines.MaxRetries`). Default `0` — a failed step with no retry budget ends the node in that one attempt. |
| `foreach` | no | A Go template rendered to a list (see "Fan-out" below); when set, this step becomes a fan-out node instead of a single step. |

### Validation rules

`Definition.Validate` (`internal/pipelines/def.go`) runs the full rule set, returning every
problem with the yaml line number where one could be determined:

- unique step ids; a duplicate id is rejected
- `needs` refers only to ids defined earlier in the file — a forward reference or an unknown id
  fails
- the `needs` graph must be acyclic (including a step that needs itself)
- every step's `template` must resolve against the pipeline's workspace (skipped when the
  workspace itself can't be read, e.g. during structural-only validation)
- `worktree` must be `""`, `own` or `shared`; `shared` requires at least one `needs` entry (there
  is nothing to share a worktree with otherwise)
- `retries` must be `0`–`3`
- every `with` value and a `foreach` expression must parse as a valid Go template (they are only
  **parsed**, not executed, at validation time — a value like `.steps.triage.report.files`
  legitimately references data that doesn't exist yet)
- at most 20 steps total
- `timeout` (if set) must not exceed 24 hours

`POST /pipelines/validate {yaml, workspace_id}` runs this same check without creating anything,
returning `{ok, errors: [{line, message}], graph: {nodes, edges}}` — always `200`, even when the
definition is invalid, so the editor's live preview never has to special-case a non-2xx response.
`POST`/`PATCH /pipelines` run it too and reject an invalid definition with `422
invalid_pipeline` and the full problem list.

## Template variables

Every step renders `with` (and a `foreach` node's `with`) against:

- the pipeline run's **input** — whatever JSON object started the run (an operator's typed input,
  or a trigger/schedule's rendered vars) — merged directly as top-level keys. A trigger-started
  pipeline's input is exactly the trigger's normalised vars, so a generic/GitHub trigger's
  `.payload` and a Grafana trigger's `.status`/`.alerts`/etc are available the same way they are
  to an ordinary template (`docs/TRIGGERS.md`).
- `steps.<id>.report` — the decoded JSON report of a finished (successful) non-fan-out step.
- `steps.<id>.reports` — for a `foreach` node, a list of every successful item's decoded report,
  in fan-out order. (A fan-out node has no singular `.report`.)
- `.item` — for a step inside a fan-out (i.e. a step whose own `foreach` produced it), the
  decoded value of this particular item.

A step referencing `steps.<id>.report` before `<id>` has finished renders against whatever the
`steps` map currently holds for that key — nothing, in practice, since a step only starts once
every step it `needs` has succeeded (see "Execution semantics" below), so by the time a
dependent step renders, its dependency's report is already present.

## Execution semantics

### Levels and starting a step

`Executor.Start` creates a pipeline run row and one pending `step_runs` row per non-fan-out node
(a `foreach` node's step runs are created once its dependencies succeed and the list is known),
then calls `advance`. `advance` walks every step in yaml order on each pass: a step whose `needs`
have **all** succeeded (for a fan-out dependency, every one of its items) is eligible; an eligible
fan-out node is expanded into its item step-runs first, then every pending step-run whose node is
eligible is started — rendered `with`, template resolved, and handed to `runs.Engine.Start` in a
session on the pipeline's workspace. Steps in the same dependency "level" (no needs between them)
start in the same `advance` pass and so run concurrently; nothing in the executor limits how many
step sessions run at once beyond the workspace's own open-session behaviour.

### Fan-out

A `foreach` step's expression is rendered against the same vars as `with` and must decode either
as a JSON array or Go's own `[a b c]` slice rendering (what a bare `{{ .steps.x.report.files }}`
prints without `json`). Each item becomes its own step-run (`index_in_fanout`, `item`); the node
is capped at **25 items** (`pipelines.MaxFanout`) — more than that fails the node outright rather
than starting an unbounded number of sessions. An empty list still gets one terminal step-run
(recorded `success` with an empty report and no `runs` row) so the node's dependents can become
eligible — a `foreach` over nothing is not an error, it's a no-op branch. A `foreach` that doesn't
render to a list, or that can't be rendered at all, fails the node with the reason recorded on its
(single) step-run's report.

### Retries

When a step's run finishes with a failure, `completeStep` marks that attempt `failed` and — if
the attempt number is still within the step's `retries` budget (`attempt <= retries`) — creates a
new step-run for the same node (`step_id`/`index_in_fanout` unchanged, `attempt` incremented) and
advances again, which starts it. Once the budget is exhausted the node is left `failed` and
nothing more is created for it.

### Downstream skip and settling

Once nothing is left pending or running, `settle` closes the pipeline run out: if any node ended
`failed`/`skipped`/`cancelled`, every still-pending step-run is marked `skipped` and any still
`running` one is stopped (interrupted and closed) before the run is recorded `failed`; a run with
no such node succeeds. In other words, a failed node (after exhausting its retries) fails the
whole run and skips every step that was still waiting behind it, but does not touch nodes that
had already started or already finished independently of it.

### Cancel

`POST /pipeline-runs/{id}/cancel` interrupts and closes every running step's session, marks every
pending step-run `cancelled`, and records the pipeline run `cancelled`. Cancelling a run that
isn't `running` is a `409`.

### Timeout

The executor sweeps every 30 seconds (`pipelines.sweepInterval`) for pipeline runs older than
their definition's `timeout` (or the 2h default if the definition can no longer be parsed) and
closes them out the same way `Cancel` does, but recorded as `timeout` — running step sessions
interrupted and closed, pending steps cancelled. The same sweep also **reconciles** step-runs
whose finishing `run.finished`/`run.failed` bus message this process never saw (dropped, or the
server restarted mid-run): it re-reads each `running` step-run's underlying `runs` row directly
and, if it has already finished, applies that outcome — so a pipeline never gets stuck `running`
just because one bus message was lost.

### Retry-failed

`POST /pipeline-runs/{id}/retry-failed` (only on a finished/terminal run) gives every `failed`
step-run a fresh attempt and puts every `skipped`/`cancelled` step-run back to `pending` (these
never actually ran, so re-running them costs no attempt), reopens the pipeline run to `running`,
and advances — steps that already succeeded are left untouched, so their reports still feed the
retry. A run with nothing failed to retry is a `409`.

### Cost

A pipeline run's cost is the sum of every `runs` row its step-runs started, retries included — a
retried attempt really did cost twice. The cost dashboard's origin breakdown (`docs/
SCHEDULES.md`) includes `pipeline` alongside `ui`/`webhook`/`schedule`/`loop`.

## Worktree modes

- **`own`** (default, or an empty `worktree` field). The step's session creates its own worktree
  from the workspace's base branch, exactly like an ordinary session on a worktree-enabled
  workspace (`docs/REVIEW.md`) — unrelated to any other step's edits.
- **`shared`**. The step's session starts **inside** the worktree of one of its dependencies (the
  first `needs` entry that has a recorded worktree) instead of creating a fresh one
  (`sessions.CreateInput.WorktreePath`; `sessions.Service.attachWorktree`), so it continues
  editing the same branch and files the dependency left off in — this is what lets a
  `triage → fix → verify` chain apply a fix and then run tests against exactly that fix in one
  continuous checkout. Requires the workspace to have worktrees enabled and requires at least one
  `needs` entry (enforced at validation time). A step's own worktree path (own or shared) is
  carried forward on its `step_runs.worktree` column so a later `shared` step can find it.
- Nothing in the executor removes a step's worktree when the pipeline finishes — a worktree
  behaves exactly like any other session's: it survives for review (diff, checkpoint, commit,
  open PR, or discard) via that step's own session page, the same Review flow any worktree
  session gets (`docs/REVIEW.md`). This is deliberate: the whole point of `worktree: shared` and
  the final step's own worktree is to leave something to review, not to clean up after itself.

## Starting a pipeline

- **By hand.** The Pipelines page (`/pipelines`) lists every visible pipeline with its step
  count and its last run's state, and a Start action; the editor page (`/pipelines/$id`) has a
  Start dialog with a JSON input editor. Both call `POST /pipelines/{id}/start {input?}` →
  `202 {pipeline_run_id}`, origin `ui`.
- **From a trigger.** A trigger names either a template **or** a pipeline (`template_id`/
  `pipeline_id`, exactly one — `POST`/`PATCH /triggers`'s `exactlyOneOfTemplateOrPipeline` check,
  `422` otherwise). When a delivery is accepted, `triggers.Service.startPipeline` calls
  `pipelines.Executor.Start` with the delivery's normalised vars as the pipeline's input, origin
  `webhook`, `origin_ref` the trigger id — dedupe, cooldown and the storm cap all apply exactly as
  they do for a template trigger (`docs/TRIGGERS.md`).
- **From a schedule.** Likewise, a schedule names either `template_id` or `pipeline_id`. A
  schedule firing that targets a pipeline calls `pipelines.Executor.Start` with the schedule's
  merged vars (including the injected `schedule` key, `docs/SCHEDULES.md`) as input, origin
  `schedule`, `origin_ref` the schedule id.
- **The `pr:<id>` reference.** A pipeline run is not a `runs` row, so a delivery's or a firing's
  `run_id` column — which the schema shares with an ordinary template run's real run id — records
  a pipeline run as the string `pr:<pipeline_run_id>` (`triggers.PipelineRunRefPrefix` /
  `schedules.PipelineRunRefPrefix`, both `"pr:"`) instead. The Triggers and Schedules pages
  recognise that prefix and link to `/pipeline-runs/<id>` rather than `/runs/<id>`.

## The run page and visibility

`GET /pipeline-runs/{id}` (the `/pipeline-runs/$id` page) returns the run, its pipeline, every
step-run attempt (each with a summary of the `runs` row it started, once it has one) and the
pipeline's graph, all in one call. The page shows:

- the graph live (`PipelineGraph`, node state glyphs for pending/running/success/failed/skipped/
  cancelled, a retry badge with the attempt number, a fan-out node showing n/N done)
- clicking a node opens a side panel with that attempt's report and a link to its session's own
  transcript
- a header with state, elapsed time, cost, **Cancel** (while running) and **Retry failed** (once
  terminal with a failed step)
- a flat step log below (step, attempt, started, duration, outcome)

Pipeline runs also appear as their own tab on the Runs page (`/runs/pipelines`, `PipelineRuns.tsx`)
— a third tab alongside ordinary runs and loops — and a step's own run detail page shows "Step of
pipeline `<name>`". Live updates arrive over the same `GET /api/v1/events` SSE stream every other
resource uses: the executor publishes a generic bus message of kind `pipeline.state`
(`{pipeline_run_id, step_run_id?, state}`) on every pipeline-run and step-run transition, which
the SSE handler forwards as `event: pipeline.state` to any visible subscriber. Like every
unattended run, a pipeline run's events carry no owner, so they're visible to every signed-in
member, not just admins or the operator who started it — shared infrastructure, same as triggers'
runs (`docs/ARCHITECTURE.md`'s visibility rules).

## Known gaps

- **A pipeline schedule never skips on overlap.** A template schedule's tick checks whether the
  run it last started is still `running` and skips the tick rather than starting a second
  concurrent instance (`docs/SCHEDULES.md`). `internal/schedules/tick.go`'s `overlapping` does
  not do this for a pipeline: since a pipeline run is not a `runs` row, this service has no reader
  for one, so a pipeline schedule's tick always starts a new pipeline run regardless of whether
  the previous one is still going. The code comment records the accepted assumption: a pipeline
  schedule's cadence is expected to be longer than its pipeline's own timeout, so overlap in
  practice shouldn't come up — but nothing enforces that, so a pipeline schedule set to fire more
  often than its pipeline can finish will pile up concurrent pipeline runs rather than skipping
  ticks the way a template schedule does.
- **A pipeline's `workspace` yaml field isn't required to match, when unset.** If the yaml omits
  `workspace:`, validation does not cross-check it against the pipeline's actual `workspace_id`
  at all — only an explicit mismatch is caught.
- **No per-step timeout.** Only the whole pipeline has a timeout; a single slow step can consume
  the entire budget before the sweep notices anything is wrong (the same shape as v0.2's runs,
  which likewise have one whole-run timeout and no fixed per-turn one).
