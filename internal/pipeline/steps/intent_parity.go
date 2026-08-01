package steps

import (
	"context"
	"sort"
	"strings"

	"github.com/kunchenguid/no-mistakes/internal/git"
)

// intentDrop describes author-added content that a rebase-conflict resolution
// (or a review fix round) silently deleted: the change the branch INTENDED to
// introduce is no longer present in the resulting tree. The whole job of this
// tool is to not lose people's code, so a detected drop parks the run for a
// human rather than reporting green (see AGENTS.md "Rebase Base & Force-Push
// Safety" and "Post-Review Head Continuity").
type intentDrop struct {
	// commits are the pre-operation commit SHAs whose changes have no patch-id
	// equivalent afterward (Signal A). Populated only on the rebase path.
	commits []string
	// lines are trimmed, author-added source lines present before the operation
	// but absent from the resulting tree (Signal B). This is the confirmation
	// that a patch-id mismatch reflects a real content loss, not just a
	// legitimate context shift from a correct conflict resolution.
	lines []string
	// files are the distinct files that carried the dropped lines.
	files []string
}

// detectRebaseHunkDrop reports author-added content that a rebase silently
// dropped. It combines two deterministic (zero-LLM) signals so it fires on the
// incident but never on a legitimate resolution:
//
//   - Signal A (fast trigger): per-commit patch-id parity. A clean rebase
//     rewrites SHAs but preserves patch-ids, so when every pre-rebase commit has
//     a patch-id equivalent after the rebase, no hunk could have been dropped and
//     the check returns nil immediately. This keeps the common no-conflict rebase
//     free of any content comparison.
//   - Signal B (confirmation): author-added line survival. Any conflict
//     resolution changes context lines, so Signal A alone would false-positive on
//     a correct resolution. Signal B compares the significant author-added lines
//     the branch introduced (oldBase..preHead) against the lines that survive
//     (oldBase..postHead) and reports only those that vanished. A resolution that
//     keeps both sides preserves every author line and returns nothing.
//
// A drop is declared only when BOTH signals agree, which is exactly the incident
// shape: a resolution that kept the upstream side and deleted the feature's own
// lines while unconflicted files (a module, a guard test) stayed intact so the
// branch still diffs non-empty and the run would otherwise proceed green.
//
// It is best-effort and fails OPEN (returns nil, nil) on any git error or when
// the required commits are missing: this guard adds safety and must never itself
// break an otherwise-valid rebase. Callers that detect a drop must park for a
// human (NeedsApproval, non-AutoFixable) rather than proceed.
func detectRebaseHunkDrop(ctx context.Context, workDir, oldBase, preHead, postHead string) *intentDrop {
	oldBase = strings.TrimSpace(oldBase)
	preHead = strings.TrimSpace(preHead)
	postHead = strings.TrimSpace(postHead)
	if preHead == "" || postHead == "" || preHead == postHead {
		return nil
	}
	for _, rev := range []string{oldBase, preHead, postHead} {
		if rev == "" || git.IsZeroSHA(rev) {
			return nil
		}
		if _, err := git.Run(ctx, workDir, "rev-parse", "--verify", "--quiet", rev+"^{commit}"); err != nil {
			return nil
		}
	}

	// Signal A: if every pre-rebase commit replayed by content, stop.
	missing, err := commitsWithoutPatchIDEquivalent(ctx, workDir, oldBase, preHead, postHead)
	if err != nil {
		return nil
	}
	if len(missing) == 0 {
		return nil
	}

	// Signal B: confirm that real author-added lines vanished from the tree.
	lines, files, err := detectDroppedAuthorLines(ctx, workDir, oldBase, preHead, postHead)
	if err != nil || len(lines) == 0 {
		return nil
	}
	// A rebase that legitimately MODIFIES an author line in place (e.g. resolving
	// timeout=60 vs timeout=45 to timeout=90) rewrites the line rather than
	// dropping the contribution. Keep only lines the rebase did not reintroduce
	// by identifier among its own additions, so an in-place resolution is not
	// mistaken for a revert. The incident's dropped call site has no such
	// reintroduction (the resolution added only the upstream statement), so it
	// stays flagged.
	reverted := filterRewrittenInPlace(ctx, workDir, preHead, postHead, lines)
	if len(reverted) == 0 {
		return nil
	}
	return &intentDrop{commits: missing, lines: reverted, files: files}
}

// detectDroppedAuthorLines compares the author-added lines the branch
// introduced between oldBase and preHead against the lines that survive between
// oldBase and postHead, and returns the significant author-added lines that
// vanished plus the files that carried them.
//
// It considers only files the author NET-ADDED to (more significant added lines
// than deleted lines in oldBase..preHead). This is the "net-deleted-author-
// lines" framing: a pure/net insertion whose lines then vanish is an
// unambiguous drop, whereas a modify/modify change (equal adds and deletes, e.g.
// a single line rewritten) is inherently a replacement that a conflict
// resolution legitimately merges, so it is out of scope for this hard gate and
// left to the LLM review. Membership is by trimmed line text, so a legitimate
// re-indentation during a conflict resolution does not read as a drop. Only
// "significant" lines count: blank lines, pure bracket/paren/keyword
// scaffolding, and lines without a real identifier are ignored so trivial
// structural churn never trips the check.
func detectDroppedAuthorLines(ctx context.Context, workDir, oldBase, preHead, postHead string) ([]string, []string, error) {
	added, err := addedLinesByFile(ctx, workDir, oldBase, preHead)
	if err != nil {
		return nil, nil, err
	}
	if len(added) == 0 {
		return nil, nil, nil
	}
	deletedCount, err := deletedSignificantCountByFile(ctx, workDir, oldBase, preHead)
	if err != nil {
		return nil, nil, err
	}
	survivingLines, err := addedLineSet(ctx, workDir, oldBase, postHead)
	if err != nil {
		return nil, nil, err
	}

	var droppedLines []string
	fileSet := map[string]struct{}{}
	seenLine := map[string]struct{}{}
	for _, file := range sortedKeys(added) {
		// Count the file's significant author-added lines; skip files that are
		// not net-additive (modify/modify, not an insertion).
		significantAdded := 0
		for _, line := range added[file] {
			if isSignificantSourceLine(strings.TrimSpace(line)) {
				significantAdded++
			}
		}
		if significantAdded <= deletedCount[file] {
			continue
		}
		for _, line := range added[file] {
			trimmed := strings.TrimSpace(line)
			if !isSignificantSourceLine(trimmed) {
				continue
			}
			if _, ok := survivingLines[trimmed]; ok {
				continue
			}
			if _, ok := seenLine[trimmed]; !ok {
				seenLine[trimmed] = struct{}{}
				droppedLines = append(droppedLines, trimmed)
			}
			fileSet[file] = struct{}{}
		}
	}
	if len(droppedLines) == 0 {
		return nil, nil, nil
	}
	files := make([]string, 0, len(fileSet))
	for file := range fileSet {
		files = append(files, file)
	}
	sort.Strings(files)
	return droppedLines, files, nil
}

// deletedSignificantCountByFile returns, per file, the count of significant
// removed (-) content lines in the diff base..head.
func deletedSignificantCountByFile(ctx context.Context, workDir, base, head string) (map[string]int, error) {
	out, err := git.Run(ctx, workDir, "diff", "--no-color", "--no-ext-diff", base, head)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	currentFile := ""
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "--- a/"):
			currentFile = strings.TrimPrefix(line, "--- a/")
		case strings.HasPrefix(line, "+++ b/"):
			currentFile = strings.TrimPrefix(line, "+++ b/")
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			if currentFile == "" || currentFile == "/dev/null" {
				continue
			}
			if isSignificantSourceLine(strings.TrimSpace(line[1:])) {
				counts[currentFile]++
			}
		}
	}
	return counts, nil
}

// addedLinesByFile returns, per file, the added (+) content lines in the diff
// base..head, preserving order and excluding the +++ header lines.
func addedLinesByFile(ctx context.Context, workDir, base, head string) (map[string][]string, error) {
	out, err := git.Run(ctx, workDir, "diff", "--no-color", "--no-ext-diff", base, head)
	if err != nil {
		return nil, err
	}
	result := map[string][]string{}
	currentFile := ""
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ b/"):
			currentFile = strings.TrimPrefix(line, "+++ b/")
		case strings.HasPrefix(line, "+++ "):
			currentFile = strings.TrimPrefix(line, "+++ ")
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			if currentFile == "" || currentFile == "/dev/null" {
				continue
			}
			result[currentFile] = append(result[currentFile], line[1:])
		}
	}
	return result, nil
}

// addedLineSet returns the set of trimmed added content lines in base..head.
func addedLineSet(ctx context.Context, workDir, base, head string) (map[string]struct{}, error) {
	byFile, err := addedLinesByFile(ctx, workDir, base, head)
	if err != nil {
		return nil, err
	}
	set := map[string]struct{}{}
	for _, lines := range byFile {
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			set[trimmed] = struct{}{}
		}
	}
	return set, nil
}

// isSignificantSourceLine reports whether a trimmed line carries enough real
// content that its disappearance is meaningful. It filters blank lines, pure
// structural scaffolding (braces, parens, lone keywords), and lines with no
// identifier of at least three characters so that trivial churn never reads as
// a dropped hunk.
func isSignificantSourceLine(trimmed string) bool {
	if len(trimmed) < 3 {
		return false
	}
	switch trimmed {
	case "{", "}", "};", "})", "});", ")", "),", "(", "[", "]", "],", "else", "else {", "return", "return;", "break", "break;", "continue", "continue;", "*/", "/*":
		return false
	}
	return hasIdentifierRun(trimmed, 3)
}

// hasIdentifierRun reports whether s contains a run of at least n consecutive
// identifier characters (letters, digits, or underscore).
func hasIdentifierRun(s string, n int) bool {
	run := 0
	for _, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			run++
			if run >= n {
				return true
			}
			continue
		}
		run = 0
	}
	return false
}

// commitsWithoutPatchIDEquivalent returns the commits reachable from preHead
// (excluding oldBase's history) whose changes have no patch-id equivalent among
// the commits reachable from postHead. It is the "Signal A" fast trigger: a
// clean rebase rewrites SHAs but preserves patch-ids, so an empty result means
// every hunk the branch proposed replayed by content and no drop is possible.
//
// A non-empty result only means a commit did not replay identically (which any
// conflict resolution produces, since resolving changes context lines); it is
// NOT sufficient on its own to declare a drop. detectDroppedAuthorLines is the
// confirming Signal B. Using git's own --cherry-pick patch-id machinery keeps a
// single owner for "are these changes still present by content" (see
// forcepush.go remoteCommitsNotIncorporated).
func commitsWithoutPatchIDEquivalent(ctx context.Context, workDir, oldBase, preHead, postHead string) ([]string, error) {
	args := []string{"rev-list", "--cherry-pick", "--left-only", preHead + "..." + postHead}
	if oldBase != "" && !git.IsZeroSHA(oldBase) {
		if _, err := git.Run(ctx, workDir, "rev-parse", "--verify", "--quiet", oldBase+"^{commit}"); err == nil {
			args = append(args, "^"+oldBase)
		}
	}
	out, err := git.Run(ctx, workDir, args...)
	if err != nil {
		return nil, err
	}
	var commits []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			commits = append(commits, line)
		}
	}
	return commits, nil
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// detectFixRoundRevert reports author-added lines that a review fix round
// silently reverted: content the branch introduced before the fix round
// (base..preFixHead) that is gone from the tree after it (base..postFixHead),
// and that the fix round did NOT merely rewrite in place.
//
// This is the held net-deleted-author-lines backstop (review.go's former HELD
// TODO): it catches the same "silently reverted contributed work" class the
// rebase parity check catches, but arriving through a review fixer round rather
// than a rebase, and it parks regardless of intent source.
//
// Distinguishing a REVERT from a legitimate MODIFICATION is the whole design
// problem - a fixer is allowed to rewrite an author line to fix a bug in it. The
// discriminator is identifier reappearance: when a dropped author line's
// identifiers all reappear among the lines the fix round newly ADDED
// (preFixHead..postFixHead), the fixer rewrote the line in place (e.g.
// validate(x) -> validate(x, opts)) and it is NOT reported. Only a line whose
// identifiers do not reappear in the round's additions is a genuine removal of
// contributed work, which parks for a human. It fails open on any git error.
func detectFixRoundRevert(ctx context.Context, workDir, base, preFixHead, postFixHead string) ([]string, []string) {
	base = strings.TrimSpace(base)
	preFixHead = strings.TrimSpace(preFixHead)
	postFixHead = strings.TrimSpace(postFixHead)
	if base == "" || preFixHead == "" || postFixHead == "" || preFixHead == postFixHead {
		return nil, nil
	}
	for _, rev := range []string{base, preFixHead, postFixHead} {
		if git.IsZeroSHA(rev) {
			return nil, nil
		}
		if _, err := git.Run(ctx, workDir, "rev-parse", "--verify", "--quiet", rev+"^{commit}"); err != nil {
			return nil, nil
		}
	}

	droppedLines, droppedFiles, err := detectDroppedAuthorLines(ctx, workDir, base, preFixHead, postFixHead)
	if err != nil || len(droppedLines) == 0 {
		return nil, nil
	}

	// A dropped line whose identifiers all reappear among the lines the round
	// newly added was rewritten in place (a legitimate forward fix), not
	// reverted. Keep only genuine removals.
	reverted := filterRewrittenInPlace(ctx, workDir, preFixHead, postFixHead, droppedLines)
	if len(reverted) == 0 {
		return nil, nil
	}
	return reverted, droppedFiles
}

// filterRewrittenInPlace keeps only the dropped lines that were genuinely
// removed, discarding lines whose identifiers all reappear among the additions
// between preHead and postHead. Reappearance means the operation rewrote the
// line in place (an in-place conflict resolution or a forward fix that changed
// arguments) rather than reverting the contributed behavior. A line with no
// usable identifier is dropped from consideration (it cannot be attributed).
// Fails open: on any git error it returns the input unchanged so a real drop is
// never hidden by an inability to read the round's additions.
func filterRewrittenInPlace(ctx context.Context, workDir, preHead, postHead string, dropped []string) []string {
	if len(dropped) == 0 {
		return nil
	}
	roundAdded, err := addedLineSet(ctx, workDir, preHead, postHead)
	if err != nil {
		return dropped
	}
	addedIdentifiers := map[string]struct{}{}
	for line := range roundAdded {
		for _, id := range identifiers(line) {
			addedIdentifiers[id] = struct{}{}
		}
	}
	var reverted []string
	for _, line := range dropped {
		ids := identifiers(line)
		if len(ids) == 0 {
			continue
		}
		rewritten := true
		for _, id := range ids {
			if _, ok := addedIdentifiers[id]; !ok {
				rewritten = false
				break
			}
		}
		if !rewritten {
			reverted = append(reverted, line)
		}
	}
	return reverted
}

// identifiers extracts identifier tokens (runs of >=3 letter/digit/underscore
// characters) from a line, lowercased for stable comparison.
func identifiers(line string) []string {
	var ids []string
	var b strings.Builder
	flush := func() {
		if b.Len() >= 3 {
			ids = append(ids, strings.ToLower(b.String()))
		}
		b.Reset()
	}
	for _, r := range line {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return ids
}
