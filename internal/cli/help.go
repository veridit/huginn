package cli

import "strings"

// Command help is self-contained so agents never need to hand-format Markdown.
func commandHelp(command string) string {
	usage := map[string]string{
		"init": "init", "new": `new "title" --goal "outcome" --accept "observable check" [flags]`,
		"ls":   "ls [--status todo|doing|done|dropped] [--type TYPE] [--priority LABEL] [--label LABEL] [--group FOLDER] [--mine]",
		"show": "show ID", "edit": "edit ID", "move": "move ID (--before ID | --after ID | --top)", "mv": "mv ID GROUP",
		"take": "take ID", "done": "done ID", "check": "check ID N", "note": `note ID "text"`,
		"set":  `set ID [--title "title"] [--type TYPE] [--priority LABEL] [--label LABEL] [--status STATUS] [--assignee NAME]`,
		"drop": "drop ID", "next": "next [--type TYPE] [--priority LABEL] [--label LABEL] [--group FOLDER] [--mine]",
		"rebalance": "rebalance [ID ...]", "types": "types", "lint": "lint", "sync": "sync", "import": "import backlog DIRECTORY", "tui": "tui", "mcp": "mcp", "help": "help [COMMAND]",
	}
	details := map[string]string{
		"new":       "Required: --goal TEXT and at least one --accept TEXT. Repeat --accept for each check.\nOptional: --type feature|bug|chore|spike|docs (default feature), --priority LABEL.\nRepeat --dep REF for dependencies and --label LABEL for labels.\nUse --group FOLDER, --top, or --after REF to choose placement.\nExample: huginn new \"Form\" --goal \"Submit works\" --accept \"Saved\" --dep k7m2qx --group auth\n",
		"ls":        "All statuses are included unless filtered. Filters combine.\n--mine uses HUGINN_USER, otherwise git config user.name.\n",
		"show":      "Shows goal, notes, numbered acceptance and dependency ids with current titles/statuses/paths.\n",
		"edit":      "Runs $EDITOR, commits the edited task and reports lint findings.\nPrefer check, note and set for non-interactive changes.\n",
		"move":      "Changes filename rank only, repairing neighbouring ranks automatically if needed.\nUse mv to change folder.\n",
		"mv":        "Moves the task to a folder without changing its id, rank or contents.\nExample: huginn mv k7m2qx auth\n",
		"take":      "Pulls, checks dependencies and ownership, sets doing, commits and pushes as a lock.\nIdentity: HUGINN_USER, otherwise git config user.name.\nRepeating take on your own doing task succeeds without changing it.\n",
		"done":      "Blocks on open dependencies and unchecked acceptance (exit 3 with exact fixes).\nCheck every acceptance item first. There is no --force. Repeating done succeeds.\n",
		"check":     "Ticks acceptance item N, numbered from 1 in show output. Repeating it succeeds.\nExample: huginn check k7m2qx 1\n",
		"note":      "Appends text to Notes, preserving existing content.\nExample: huginn note k7m2qx \"Blocked on p3rt9a; waiting for API\"\n",
		"set":       "Types: feature|bug|chore|spike|docs. Statuses: todo|doing|done|dropped.\nPriority is a free label. --label and --assignee replace the field with one value.\nStatus done still enforces dependencies and acceptance. Use take to claim work.\n",
		"drop":      "Marks a task dropped even when blocked. Does not delete the file. Repeating it succeeds.\n",
		"next":      "Plain next: highest-ranked unblocked, unassigned todo. Exit 4 when none match.\n--mine: my unblocked doing first, then todo, in rank order. Filters combine.\nTypes: feature|bug|chore|spike|docs. Priority is a free label.\n",
		"rebalance": "Rekeys ranked filenames without changing task contents. Usually unnecessary.\n",
		"lint":      "Reports file:line: problem for malformed tasks, cycles and dangling dependencies.\nJSON uses findings, an array (empty when clean).\n",
		"sync":      "Commits local task edits, pulls with rebase and pushes only the huginn branch.\n",
		"import":    "Imports backlog-md tasks idempotently, preserving old ids as aliases.\n",
		"tui":       "Interactive ranked list and board. For non-interactive output use ls --json.\n",
		"mcp":       "Runs the standard-I/O MCP server with ten flat tools and next-step hints.\n",
	}
	return "huginn " + usage[command] + "\n" + details[command] + "Refs accept ids, unique prefixes, slugs and aliases case-insensitively.\nAdd --json for structured output.\nnext: huginn next\n"
}
func isJSON(args []string) bool {
	for _, a := range args {
		if a == "--json" || strings.HasPrefix(a, "--json=") {
			return true
		}
	}
	return false
}
