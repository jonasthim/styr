// builtins.go classifies the CLI's own built-in slash commands for headless (-p
// stream-json) use. The init message's `slash_commands` array mixes custom commands, plugin
// skills and built-ins without saying which of them survive the pipe; these two lists carry
// the spike's findings (testdata/PROTOCOL.md, "Built-in slash commands in -p mode") so the
// sessions service and the UI can filter the menu without re-deriving them.
package claude

// headlessBuiltins are built-in commands verified to run over the pipe: the CLI answers them
// itself, as a synthetic assistant text block plus a `result` with num_turns 0 and cost 0, and
// the session keeps its id and its transcript. Verified against CLI 2.1.276.
var headlessBuiltins = []string{"compact", "cost"}

// hiddenBuiltins are built-in commands Styr keeps out of the slash-command menu:
//
//   - clear: the CLI resets the conversation and re-inits under a *new* session id, which
//     orphans the id Styr resumes with and silently desynchronises Styr's transcript
//     (verified; see PROTOCOL.md).
//   - doctor, color, reload-plugins: the CLI itself reports these as terminal-only in the
//     init message's `terminal_slash_commands` array.
//   - model, effort: Styr owns these through the session header's selects, which restart the
//     process with new flags and persist the choice; letting the CLI change them behind
//     Styr's back would leave the stored values wrong.
var hiddenBuiltins = []string{"clear", "doctor", "color", "reload-plugins", "model", "effort"}

// HeadlessBuiltins returns the built-in slash commands that work over the pipe in -p
// stream-json mode.
func HeadlessBuiltins() []string { return append([]string(nil), headlessBuiltins...) }

// HiddenBuiltins returns the built-in slash commands the UI must hide: they do not work over
// the pipe, are terminal-only, or are replaced by Styr's own controls.
func HiddenBuiltins() []string { return append([]string(nil), hiddenBuiltins...) }
