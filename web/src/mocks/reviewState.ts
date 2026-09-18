// Seed data and mutable state for the v0.3 review endpoints, kept out of
// handlers.ts (which owns the routes themselves) so the fixture stays legible
// and so nothing here imports the handler module back.
//
// The diff is a realistic one for the fixture session: a Go file changed in
// two places, a document added, and a file deleted — the three shapes the
// renderer has to get right, with a rename left to the real backend.
import type { Checkpoint, DiffHunk, DiffLine, DiffLineType, DiffSummary, FileDiff, ReviewComment } from '../api/types'
import { TOOL_FIXTURE_SESSION_ID } from './sessionsState'

export { TOOL_FIXTURE_SESSION_ID }

function iso(minutesAgo: number): string {
  return new Date(Date.now() - minutesAgo * 60_000).toISOString()
}

type LineSpec = [DiffLineType, string]

/** Builds a hunk from the compact [type, text] form, numbering both sides and
 * counting the header the way git does. */
function hunk(oldStart: number, newStart: number, specs: LineSpec[]): DiffHunk {
  let oldNo = oldStart
  let newNo = newStart
  const lines: DiffLine[] = specs.map(([type, text]) => ({
    type,
    // 0, not null, on the side a line does not exist on - the handler serves
    // plain ints (internal/api/review_handlers.go's diffLineDTO).
    old_no: type === 'add' ? 0 : oldNo++,
    new_no: type === 'del' ? 0 : newNo++,
    text,
  }))
  return {
    old_start: oldStart,
    old_lines: lines.filter((l) => l.type !== 'add').length,
    new_start: newStart,
    new_lines: lines.filter((l) => l.type !== 'del').length,
    lines,
  }
}

const SERVICE_GO: FileDiff = {
  path: 'internal/sessions/service.go',
  old_path: '',
  status: 'M',
  binary: false,
  truncated: false,
  hunks: [
    hunk(41, 41, [
      ['ctx', '// Service owns every running session and the worktree it works in.'],
      ['ctx', 'type Service struct {'],
      ['ctx', '\tstore    *db.Store'],
      ['del', '\tharness  harness.Factory'],
      ['add', '\tharness  harness.Factory'],
      ['add', '\tgit      *gitops.Repo'],
      ['add', '\tworktree string'],
      ['ctx', '\tslots    *slots'],
      ['ctx', '}'],
      ['ctx', ''],
    ]),
    hunk(118, 120, [
      ['ctx', 'func (s *Service) Create(ctx context.Context, in CreateInput) (*Session, error) {'],
      ['ctx', '\tws, err := s.store.Workspace(ctx, in.WorkspaceID)'],
      ['ctx', '\tif err != nil {'],
      ['del', '\t\treturn nil, err'],
      ['add', '\t\treturn nil, fmt.Errorf("workspace %q: %w", in.WorkspaceID, err)'],
      ['add', '\t}'],
      ['add', ''],
      ['add', '\tif ws.Worktrees {'],
      ['add', '\t\tif err := s.git.AddWorktree(ctx, dir, branch, base); err != nil {'],
      ['ctx', '\t}'],
      ['ctx', ''],
    ]),
  ],
}

const REVIEW_MD_LINES = [
  '# Reviewing a session',
  '',
  'Every session on a worktree-enabled workspace runs on its own branch, so the work is visible as a diff long before it reaches the main checkout.',
  '',
  '## Reading the diff',
  '',
  'Open a session and switch the side panel to **Review**. The file list shows',
  'each changed file with its status and line counts; picking one replaces the',
  'transcript with the diff.',
  '',
  '- [ ] Side by side from 1100px, unified below it',
  '- [ ] Click a line number to comment on that line',
  '- [ ] Comments stay unsent until you send the review',
  '',
  '## Sending a review',
  '',
  'Unsent comments collect in the Review rail. **Send review** turns them into',
  'the session\'s next prompt:',
  '',
  '```',
  'Review comments on your changes (address each, then summarise what you changed):',
  '- internal/sessions/service.go:46 (new): Guard this against an empty base ref.',
  '```',
  '',
  '## Committing and opening a pull request',
  '',
  'Commit writes the worktree to the branch. Open PR needs the GitHub CLI: if',
  '`gh` is missing or unauthenticated the dialog says so. Install it and run',
  '`gh auth login`.',
  '',
  '## Checkpoints',
  '',
  'Every turn leaves a checkpoint commit. Rewinding resets the worktree to one of',
  'them, throwing away everything after it.',
]

const REVIEW_MD: FileDiff = {
  path: 'docs/REVIEW.md',
  old_path: '',
  status: 'A',
  binary: false,
  truncated: false,
  hunks: [hunk(0, 1, REVIEW_MD_LINES.map((text): LineSpec => ['add', text]))],
}

const LEGACY_GO_LINES = [
  'package api',
  '',
  'import "net/http"',
  '',
  '// handleLegacyDiff served the pre-worktree diff, which read the workspace',
  '// checkout directly. Replaced by the session diff endpoints.',
  'func (s *Server) handleLegacyDiff(w http.ResponseWriter, r *http.Request) {',
  '\tsession, ok := s.session(r)',
  '\tif !ok {',
  '\t\twriteError(w, http.StatusNotFound, "not_found", "session not found")',
  '\t\treturn',
  '\t}',
  '',
  '\twriteJSON(w, http.StatusOK, legacyDiff{Path: session.Workspace})',
  '}',
  '',
]

const LEGACY_GO: FileDiff = {
  path: 'internal/api/legacy_diff.go',
  old_path: '',
  status: 'D',
  binary: false,
  truncated: false,
  hunks: [hunk(1, 0, LEGACY_GO_LINES.map((text): LineSpec => ['del', text]))],
}

export const fileDiffs: FileDiff[] = [SERVICE_GO, REVIEW_MD, LEGACY_GO]

function countOf(file: FileDiff, type: DiffLineType): number {
  return file.hunks.reduce((sum, h) => sum + h.lines.filter((l) => l.type === type).length, 0)
}

export const diffSummary: DiffSummary = {
  base_ref: 'main',
  branch: 'styr/0000005-read-note-txt',
  dirty: true,
  files: fileDiffs.map((file) => ({
    path: file.path,
    old_path: file.old_path,
    status: file.status,
    add: countOf(file, 'add'),
    del: countOf(file, 'del'),
    binary: file.binary,
  })),
  total_add: fileDiffs.reduce((sum, f) => sum + countOf(f, 'add'), 0),
  total_del: fileDiffs.reduce((sum, f) => sum + countOf(f, 'del'), 0),
}

export const checkpoints: Checkpoint[] = [
  { id: 'cp3', session_id: TOOL_FIXTURE_SESSION_ID, commit_sha: 'c3f10adf2b91', turn: 3, summary: 'Delete the legacy diff handler', created_at: iso(11) },
  { id: 'cp2', session_id: TOOL_FIXTURE_SESSION_ID, commit_sha: 'b71e94c0aa32', turn: 2, summary: 'Document the review flow', created_at: iso(18) },
  { id: 'cp1', session_id: TOOL_FIXTURE_SESSION_ID, commit_sha: 'a09d4471ee10', turn: 1, summary: 'Add the worktree fields', created_at: iso(26) },
]

/** Comments start empty: a review is something the operator writes, and every
 * spec that needs one writes its own. */
export const comments: ReviewComment[] = []

let nextCommentSeq = 1

export function newComment(input: {
  sessionId: string
  path: string
  line: number
  side: 'old' | 'new'
  body: string
  authorId: string
}): ReviewComment {
  return {
    id: `rc-${nextCommentSeq++}`,
    session_id: input.sessionId,
    path: input.path,
    line: input.line,
    side: input.side,
    body: input.body,
    author_id: input.authorId,
    author_name: 'Dev Admin',
    created_at: iso(0),
    sent_at: null,
  }
}

/** The message POST /review sends the CLI, straight from the plan's
 * "Review message format sent to the CLI". */
export function reviewMessage(unsent: ReviewComment[]): string {
  const lines = unsent.map((c) => `- ${c.path}:${c.line} (${c.side}): ${c.body}`)
  return ['Review comments on your changes (address each, then summarise what you changed):', ...lines].join('\n')
}

/** Flipped by POST /__mock/gh-unavailable so a spec can see the 409 path. */
export const ghState = { unavailable: false }

export const PLAN_MARKDOWN = `## Add the worktree diff endpoints

I read \`internal/sessions/service.go\` and \`internal/api/routes.go\`. Here is what I plan to do:

- [ ] Add \`gitops.Worktree.FileDiff\` and cover it with a temp-repo test
- [ ] Serve \`GET /sessions/{id}/diff\` and \`GET /sessions/{id}/diff/file\`
- [ ] Store review comments and send them as one user message
- [ ] Checkpoint after every result so a turn can be rewound

The endpoints stay owner-or-admin, and nothing touches the workspace checkout.`

export const PLAN_APPROVAL_ID = 'plan-1'

export function planApproval(sessionId: string, sessionTitle: string) {
  return {
    id: PLAN_APPROVAL_ID,
    session_id: sessionId,
    request_id: 'req-plan-1',
    tool: 'ExitPlanMode',
    input: { plan: PLAN_MARKDOWN },
    // The handler serves the plan on its own field too
    // (internal/api/approvals_handlers.go's approvalDTO.Plan); the UI reads
    // that first and only falls back to the raw tool input.
    plan: PLAN_MARKDOWN,
    risk: 'read' as const,
    state: 'pending' as const,
    created_at: iso(0),
    snoozed_until: null,
    updated_input: null,
    message: '',
    session_title: sessionTitle,
    now_line: 'Waiting for you to approve the plan',
  }
}
