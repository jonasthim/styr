# Review

v0.3 gives every session on a worktree-enabled workspace its own git worktree: the session's
edits land on a disposable branch instead of the workspace's own checkout, so the Review tab can
show a real diff, comments can be sent back to the CLI as a prompt, and the result can be
committed or turned into a pull request from the UI without ever touching the checkout a human
might also be working in. `internal/gitops` is the only package that shells out to `git`/`gh`;
`internal/sessions/worktree.go`, `review.go` and `review_actions.go` are the source of truth if
anything here and the code disagree.

## Worktree per session

A workspace opts in with its `worktrees` flag (`PATCH /workspaces/{id}`, or at creation). When
it is on, creating a session adds a worktree *before* the CLI process starts, so the process's
`cwd` is the worktree from its very first turn (`internal/sessions/service.go`'s `sessionCwd`
falls back to the workspace's own checkout for a session with no worktree).

- **Where they live.** `<workspace path>/.styr/worktrees/<session id>`
  (`internal/sessions/worktree.go`'s `worktreeDir`). `gitops.Repo.AddWorktree` appends
  `.styr/` to the checkout's `.git/info/exclude` the first time it runs, so `.styr/` never shows
  up as untracked in the workspace's own `git status` — it is excluded per-checkout, not via a
  tracked `.gitignore` entry, so it never lands in a commit either.
- **Branch naming.** `styr/<first 8 chars of the session id>-<slugified title>`, e.g.
  `styr/3f2a9c11-fix-the-flaky-test`. The slug is lowercased, non-alphanumeric runs collapse to
  a single dash, and it is cut to 40 characters (`branchName`/`slugify` in `worktree.go`); a
  session with no title-derived slug just gets `styr/<short id>`. `internal/gitops` itself
  refuses any other shape (`^styr/[a-z0-9-]{1,60}$`), so nothing outside this naming scheme can
  be created through it.
- **Base branch detection.** The workspace's `base_branch` field names the branch new worktrees
  are created from; left empty (the default), Styr resolves the checkout's *current* branch at
  the moment the worktree is created (`git rev-parse --abbrev-ref HEAD`) and uses that instead.
  Either way, the worktree is created with `git worktree add -b <branch> <dir> <base>`, and the
  commit that resolves to is recorded as the session's `base_ref` — every later diff, checkpoint
  and commit works against that one fixed commit, not a moving branch tip.
- **`auto_checkpoint`.** The workspace's other worktree setting: whether Styr commits the
  worktree after every turn (on by default). See Checkpoints below. Both `base_branch` and
  `auto_checkpoint` are plain workspace columns, set at creation or via `PATCH
  /workspaces/{id}` — there is no per-session override.

Nothing in this path ever writes to the workspace's main checkout beyond `git worktree`
commands and reading refs (`ws.Path` itself is only ever the base for `AddWorktree`/
`RemoveWorktree` and `CurrentBranch`); every diff, checkpoint, commit, push and PR operates on
the session's own worktree directory.

## The Review tab

The session view's side panel gained a **Review** tab (next to Activity and Info) alongside the
existing per-tool-call Activity timeline; it only has anything to show for a session that has a
worktree — `GET /sessions/{id}/diff` on a worktree-less session answers 422
(`sessions.ErrNoWorktree`).

- **The diff.** `GET /sessions/{id}/diff` returns every file changed between the session's
  `base_ref` and its worktree's current state — committed turns *and* whatever is still
  uncommitted on disk, folded into one view (`gitops.Worktree.DiffSummary`: tracked changes from
  `git diff --numstat -M <base_ref>`, untracked files counted straight off disk). Each file
  carries its status (`A`/`M`/`D`/`R`), added/removed line counts, and whether git considers it
  binary; the response's own `dirty` flag says whether the worktree has any uncommitted changes
  right now. Picking a file calls `GET /sessions/{id}/diff/file?path=` for its unified diff
  (`git diff -M -U3 <base_ref> -- <path>`, or an all-additions synthetic diff for an untracked
  file), rendered side-by-side above 1100px and unified below it.
- **Inline comments.** Clicking a line number opens a comment box against that file, line and
  side (`old`/`new`); `POST /sessions/{id}/comments` queues it (`state: unsent`) without sending
  anything to the CLI yet, so a review can be built up over several files before it goes out.
  They list in the Review rail with an unsent-count badge on the tab itself.
- **Send review.** `POST /sessions/{id}/review` composes every unsent comment into a single user
  message, sends it to the session exactly like an ordinary prompt (starting or resuming the
  process as needed), and marks every comment it sent. With nothing unsent it answers 422. The
  message the CLI actually receives (`internal/sessions/review.go`'s `formatReview`):

  ```
  Review comments on your changes (address each, then summarise what you changed):
  - src/auth.go:42 (new): please handle the error
  - README.md:10 (old): this line is now stale
  ```

  One line per comment, `<path>:<line> (<side>): <body>`, in the order the comments were made;
  there is no per-comment reply — the whole batch is one turn, and the CLI's response (and
  whatever it edits) shows up as that turn's transcript and diff like any other.

## Checkpoints

When `auto_checkpoint` is on, every finished turn on a worktree session tries a checkpoint: `git
add -A` plus a commit under a fixed `Styr <styr@local>` identity (`--no-verify`, so no local
hook can block it). If the turn left nothing to commit, nothing happens — no empty checkpoint
commit, no `checkpoints` row. If it did, the commit's sha and turn number are recorded
(`GET /sessions/{id}/checkpoints`, newest first).

- **One per turn when dirty.** This is a straight "did anything change" check
  (`git diff --cached --quiet` after staging), not a fixed cadence — a turn that only reads
  files produces no checkpoint. The very first worktree also gets a "checkpoint" call right
  after `git worktree add`, but a freshly created worktree is always clean, so that call commits
  nothing either — it exists purely to resolve the worktree's starting commit sha for
  `base_ref`, not to leave a real checkpoint on the list.
- **Rewind semantics: files only, chat kept.** `POST /sessions/{id}/rewind {checkpoint_id}`
  resets the worktree's files to that checkpoint's commit (`git reset --hard` plus `git clean
  -fd`, refusing with 409 unless the checkpoint's sha is still an ancestor of the branch's
  current HEAD) and refreshes the session's diff stats. Nothing about the session's own
  transcript, event log or turn count is touched — the chat stays exactly as it was, so rewinding
  is "put the files back to how they looked after turn N" while the conversation, and the
  context the CLI has of it, carries on unchanged. The next prompt can then ask for a different
  approach without losing that context.
- **Refused while running.** Both rewind and discard (below) refuse with 409 while the session
  is `running` or `waiting` on an approval (`internal/sessions/review_actions.go`'s `busy`) —
  the live CLI process is holding those files open for its current tool call, and rewriting them
  underneath it would corrupt the run. A recorded spike confirms the reverse is safe, though: a
  checkpoint commit made *between* two turns of the same live session does not disturb the next
  turn, because the CLI only ever reads files off disk per tool call, never git history or the
  index (`internal/harness/claude/testdata/PROTOCOL.md`, "Checkpoint safety between turns").

## Commit

`POST /sessions/{id}/commit {message}` folds the session's work into one commit on top of
`base_ref`: any checkpoint commits already made since then are squashed away first
(`git reset --soft base_ref`, which unstages nothing lost — every changed file is still on disk,
just no longer split across several "styr: checkpoint after turn N" commits), then everything is
staged and committed as a single commit with `message`. The author is resolved from the calling
user's own profile — display name and email, falling back to `Styr <styr@local>` when the actor
has no user record or no email set (`internal/sessions/review_actions.go`'s `authorFor`) — never
the fixed `Styr <styr@local>` identity checkpoints themselves use. A message is required (422
without one); 409 (`sessions.ErrNothingToCommit`-derived) when there is nothing to commit, or
while the session is still running.

## Open PR

`POST /sessions/{id}/pr {title, body, base?}` publishes the branch: `git push -u origin
<branch>` (never forced), then `gh pr create --head <branch> --base <base> --title --body`.
`base` defaults to the workspace's `base_branch`, and failing that the checkout's current branch
— the same resolution `AddWorktree` used to create the worktree in the first place. Both steps
can fail for reasons only an operator, not Styr, can fix:

- **No `origin` remote.** `Push` checks `git remote` before pushing; with none configured it
  fails immediately rather than attempting the push. Answered as **409, error code
  `no_remote`**.
- **`gh` unavailable.** `CreatePR` requires `gh` on `PATH` and `gh auth status` to succeed
  *in the environment `styr serve` itself runs under* — not any of Styr's own per-user Claude
  `HOME` directories under `data_dir/users/<id>`, since `internal/gitops` runs `git`/`gh` with
  the server process's own environment (every `GIT_`-prefixed variable stripped, otherwise
  untouched). On the install-script/systemd deployment that's the `styr` system user
  (`deploy/styr.service`'s `User=styr`), so making PRs work means either running
  `sudo -u styr gh auth login` once on the box, or dropping a `gh` auth token into that user's
  own `HOME` (`gh auth login --with-token`, or a `GH_TOKEN`/`GITHUB_TOKEN` environment variable
  in `styr`'s own environment file). Answered as **409, error code `gh_unavailable`**.

On success the response is the PR's URL. A title is required (422 without one).

## Discard

`POST /sessions/{id}/discard` throws the session's work away entirely: closes any live process,
removes the worktree directory and its branch (`git worktree remove --force` plus
`git branch -D`, then `git worktree prune`), clears the session's worktree/branch/base fields
and diff stats, and closes the session. Like rewind, it refuses with 409 while the session is
still running or waiting on an approval.

## Patch download

`GET /sessions/{id}/patch` returns the whole worktree diff against `base_ref` (`git diff
base_ref`, committed and uncommitted changes together) as `text/x-diff`, for anyone who would
rather review it in their own editor or apply it elsewhere than on the branch itself.

## Plan approval

A session started under a profile in **plan** mode runs the CLI with `--permission-mode plan`.
When the model finishes planning, it calls `ExitPlanMode` like any other tool — there is no
distinct "plan ready" message — so it surfaces as an ordinary permission request
(`can_use_tool`/`ExitPlanMode`) with the plan's markdown at `request.input.plan`
(`harness.PermissionRequest.Plan`, decoded by `internal/harness/claude/plan.go`'s
`planFromInput`; `claude.IsPlanExit` is exactly `req.ToolName == "ExitPlanMode"`). Styr renders
that request as a Plan card instead of the ordinary tool-approval UI, and both responses go
through the same `POST /approvals/{id}` decision endpoint every other permission prompt uses:

- **Approve plan** is an ordinary `allow` decision on that request — nothing more. The CLI
  switches its own `permissionMode` from `plan` to `default` **in-process**, observed as a
  `system`/`status` line immediately after the response, and the model continues acting on the
  plan under the same session id and the same running process, asking permission for each
  subsequent tool call exactly as before. Styr never resumes or restarts the process for this —
  a spike confirmed the CLI needs no `--resume`/`--permission-mode default` restart at all
  (`internal/harness/claude/testdata/PROTOCOL.md`, "Plan mode in `-p`"; ADR-012).
- **Request changes** is a `deny` decision carrying the operator's typed comment as the
  decision's message. The CLI stays in plan mode and the model receives that comment to revise
  the plan, the same way any other tool denial's message reaches it.

Plan approval needs no worktree-specific handling beyond the ordinary checkpoint/diff bookkeeping
above: checkpointing between any two turns of a live session, plan-mode ones included, is safe
regardless of what happened in between (same spike).
