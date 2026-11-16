package ringrun

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The cap on one printed segment: a longer one keeps its head and its tail, where the error usually is.
const (
	segmentMaxLines  = 400
	segmentHeadLines = 40
	segmentTailLines = 200
)

var (
	versionRe = regexp.MustCompile(`GNU Make (\d+)\.(\d+)`)
	// traceRe matches the line `make --trace` prints before a target's recipe: 4.3 and later write
	// "Makefile:40: update target 'x' due to: …", 4.0 to 4.2 also "Makefile:40: target 'x' does not exist".
	traceRe = regexp.MustCompile("^(?:\\S+:\\d+: |make(?:\\[\\d+\\])?: )(?:update )?target [`']([^']+)' (?:due to:|does not exist)")
	// errorRe matches make's line for a failed recipe: "make: *** [Makefile:40: x] Error 1" (4.x),
	// "make: *** [x] Error 1" (3.81) and the "make[1]: ***" forms of a sub-make.
	errorRe = regexp.MustCompile(`^make(?:\[\d+\])?: \*\*\* \[(?:[^\]:]+:\d+: )?([^\]]+)\] `)
	// notRemadeRe matches make's closing line for a target whose prerequisite failed; it belongs to no step.
	notRemadeRe = regexp.MustCompile(`^make(?:\[\d+\])?: Target .+ not remade because of errors\.$`)
)

// segment is the output of one target: the lines make printed after its trace line, up to the next one.
type segment struct {
	Target string // empty for the output before the first trace line
	Lines  []string
}

// hasTrace reports whether the `make --version` output names GNU make 4.0 or later, which knows --trace.
func hasTrace(versionOutput string) bool {
	m := versionRe.FindStringSubmatch(versionOutput)
	if m == nil {
		return false
	}
	major, err := strconv.Atoi(m[1])
	return err == nil && major >= 4
}

// splitLines splits captured output into lines without the trailing empty one.
func splitLines(out string) []string {
	out = strings.TrimRight(out, "\n")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// failedTarget returns the target an error line of make names; ok is false for any other line. An ignored error (a
// recipe line prefixed with -) has no "***" and does not match.
func failedTarget(line string) (string, bool) {
	m := errorRe.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// failedTargets returns the targets whose recipe failed, in the order make reported them, each once. A target that
// was not remade only because a prerequisite failed has no error line and is not listed.
func failedTargets(lines []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range lines {
		if t, ok := failedTarget(line); ok && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// splitSegments splits `make --trace` output into one segment per target, the trace lines and the "not remade"
// lines removed. An error line goes to the segment of the target it names, so the error of a sub-make lands with its
// own target.
func splitSegments(lines []string) []segment {
	segs := []segment{{}}
	index := map[string]int{}
	for _, line := range lines {
		if m := traceRe.FindStringSubmatch(line); m != nil {
			index[m[1]] = len(segs)
			segs = append(segs, segment{Target: m[1]})
			continue
		}
		if notRemadeRe.MatchString(line) {
			continue
		}
		at := len(segs) - 1
		if t, ok := failedTarget(line); ok {
			if i, known := index[t]; known {
				at = i
			}
		}
		segs[at].Lines = append(segs[at].Lines, line)
	}
	if len(segs[0].Lines) == 0 {
		return segs[1:]
	}
	return segs
}

// withoutTrace returns the lines minus make's trace lines.
func withoutTrace(lines []string) []string {
	var out []string
	for _, line := range lines {
		if !traceRe.MatchString(line) {
			out = append(out, line)
		}
	}
	return out
}

// truncate caps a segment: past segmentMaxLines lines it keeps the head and the tail with a line naming how many
// lines it left out and how to see them.
func truncate(lines []string, ring string) []string {
	if len(lines) <= segmentMaxLines {
		return lines
	}
	omitted := len(lines) - segmentHeadLines - segmentTailLines
	out := make([]string, 0, segmentHeadLines+1+segmentTailLines)
	out = append(out, lines[:segmentHeadLines]...)
	out = append(out, fmt.Sprintf("… %d lines omitted (pawl run %s --verbose shows everything) …", omitted, ring))
	return append(out, lines[len(lines)-segmentTailLines:]...)
}

// lastSegment returns the last segment of the target; a target updated twice (by two sub-makes) shows its last run.
func lastSegment(segs []segment, target string) (segment, bool) {
	for i := len(segs) - 1; i >= 0; i-- {
		if segs[i].Target == target {
			return segs[i], true
		}
	}
	return segment{}, false
}

// passLine is the summary of a ring that passed; steps below zero means unknown (make without --trace).
func passLine(ring string, d time.Duration, steps int) string {
	if steps < 0 {
		return fmt.Sprintf("pawl: %s passed in %s", ring, formatDuration(d))
	}
	return fmt.Sprintf("pawl: %s passed in %s (%d steps)", ring, formatDuration(d), steps)
}

// failLine is the summary of a ring that failed.
func failLine(ring string, d time.Duration, failed []string) string {
	if len(failed) == 0 {
		return fmt.Sprintf("pawl: %s failed in %s", ring, formatDuration(d))
	}
	return fmt.Sprintf("pawl: %s failed in %s: %s", ring, formatDuration(d), strings.Join(failed, ", "))
}

// failReport is what quiet mode prints for a failed ring: the summary, then each failed target's segment. Without
// segments for the failed targets (make without --trace, or a Makefile that did not parse) it prints the whole
// output.
func failReport(ring string, d time.Duration, lines []string, isTraced bool) []string {
	failed := failedTargets(lines)
	out := []string{failLine(ring, d, failed)}
	var segs []segment
	if isTraced {
		segs = splitSegments(lines)
	}
	var shown []string
	for _, t := range failed {
		if s, ok := lastSegment(segs, t); ok {
			shown = append(shown, "--- "+t)
			shown = append(shown, truncate(s.Lines, ring)...)
		}
	}
	if len(shown) == 0 {
		return append(out, truncate(withoutTrace(lines), ring)...)
	}
	return append(out, shown...)
}

// stepCount is the number of targets make updated, or -1 without --trace.
func stepCount(lines []string, isTraced bool) int {
	if !isTraced {
		return -1
	}
	n := 0
	for _, line := range lines {
		if traceRe.MatchString(line) {
			n++
		}
	}
	return n
}

// formatDuration rounds a run's wall time for the summary line.
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(100 * time.Millisecond).String()
}

// dockerArgs returns the arguments of `docker` that run make with makeArgs in the toolbox image on the project
// mounted at /work, as the calling user, with the PAWL_* variables that steer the rings passed through when set.
func dockerArgs(image, root string, uid, gid int, environ, makeArgs []string) []string {
	args := []string{"run", "--rm", "-u", fmt.Sprintf("%d:%d", uid, gid), "-v", root + ":/work", "-w", "/work"}
	for _, name := range passedVars() {
		if lookup(environ, name) != "" {
			args = append(args, "-e", name)
		}
	}
	args = append(args, image, "make")
	return append(args, makeArgs...)
}

// passedVars are the variables a toolbox run takes from the caller's environment.
func passedVars() []string { return []string{"PAWL_BASE", "PAWL_FULL", "PAWL_WORK_TYPE"} }

// lookup returns the value of name in an environment list; the last entry wins, as in the process environment.
func lookup(environ []string, name string) string {
	value := ""
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok && k == name {
			value = v
		}
	}
	return value
}
