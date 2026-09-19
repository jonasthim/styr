package domain

import "time"

// WorkspaceSource is how a workspace's directory came to exist.
type WorkspaceSource string

const (
	WorkspaceSourceGit   WorkspaceSource = "git"
	WorkspaceSourcePath  WorkspaceSource = "path"
	WorkspaceSourceEmpty WorkspaceSource = "empty"
)

// WorkspaceState is where a workspace is in its (possibly asynchronous)
// setup lifecycle.
type WorkspaceState string

const (
	WorkspaceCloning WorkspaceState = "cloning"
	WorkspaceReady   WorkspaceState = "ready"
	WorkspaceFailed  WorkspaceState = "failed"
)

// WorkspaceAccessMode controls which signed-in users may see and use a
// shared (OwnerID nil) workspace. It has no effect on an owned workspace,
// which is always visible only to its owner and to admins regardless of
// this value.
type WorkspaceAccessMode string

const (
	// WorkspaceAccessEveryone (the default) is the pre-T64 behaviour: every
	// signed-in user can see and use the workspace.
	WorkspaceAccessEveryone WorkspaceAccessMode = "everyone"
	// WorkspaceAccessListed restricts the workspace to the users named in
	// its workspace_access rows, plus admins.
	WorkspaceAccessListed WorkspaceAccessMode = "listed"
)

// Workspace is a working directory (optionally worktree-enabled) that
// sessions run in. A workspace is either owned by a member (OwnerID set,
// Styr manages its directory under UsersDir) or shared and admin-registered
// (OwnerID nil, an existing server path Styr does not manage).
type Workspace struct {
	ID      string
	OwnerID *string // nil = shared, admin-registered
	Name    string
	Path    string

	Source  WorkspaceSource
	RepoURL string
	Branch  string
	// Managed reports whether Styr created Path and may delete it; false
	// for admin-registered shared path workspaces.
	Managed bool
	State   WorkspaceState
	Error   string

	DefaultProfileID string
	Worktrees        bool
	// BaseBranch is the branch new session worktrees are created from; ""
	// means "whatever the checkout's current branch is at that moment".
	BaseBranch string
	// AutoCheckpoint makes Styr commit the worktree after every turn, so
	// the session can be rewound turn by turn.
	AutoCheckpoint bool
	// Access controls, for a shared (OwnerID nil) workspace, which
	// non-admin users may see and use it: "everyone" (the default) or
	// "listed" (only the users in workspace_access). Meaningless for an
	// owned workspace.
	Access    WorkspaceAccessMode
	CreatedAt time.Time
	UpdatedAt time.Time
}
