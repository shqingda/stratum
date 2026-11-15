package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/locales"
)

type diffLine struct {
	text   string
	sign   string
	number int
}

type cardDiff struct {
	oldText, newText string
	lines            []diffLine
	replaced         bool
	lineEndings      bool
	added, removed   int
}

// diffLines keeps line endings in the comparison, including a missing final newline.
func diffLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.SplitAfter(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func lineDiff(oldText, newText string) (lines []diffLine, replaced bool) {
	old, next := diffLines(oldText), diffLines(newText)
	add := func(text, sign string, number int) {
		lines = append(lines, diffLine{text, sign, number})
	}
	// Bound the LCS table; large edits still show all removed and added lines.
	if len(old)+len(next) > 2000 {
		for i, text := range old {
			add(text, "−", i+1)
		}
		for i, text := range next {
			add(text, "+", i+1)
		}
		return lines, true
	}
	width := len(next) + 1
	lcs := make([]uint16, (len(old)+1)*width)
	for i := len(old) - 1; i >= 0; i-- {
		for j := len(next) - 1; j >= 0; j-- {
			if old[i] == next[j] {
				lcs[i*width+j] = 1 + lcs[(i+1)*width+j+1]
			} else {
				lcs[i*width+j] = max(lcs[(i+1)*width+j], lcs[i*width+j+1])
			}
		}
	}
	for i, j := 0, 0; i < len(old) || j < len(next); {
		switch {
		case i < len(old) && j < len(next) && old[i] == next[j]:
			add(next[j], "", j+1)
			i++
			j++
		case i < len(old) && (j == len(next) || lcs[(i+1)*width+j] >= lcs[i*width+j+1]):
			add(old[i], "−", i+1)
			i++
		default:
			add(next[j], "+", j+1)
			j++
		}
	}
	return lines, false
}

func (a *App) card(c *ui.Context, e *client.Entry) {
	if !e.Done || e.Skipped || e.Output == "" || !strings.HasPrefix(strings.TrimSpace(string(e.Args)), "{") {
		a.step(c, e)
		return
	}
	switch e.Text {
	case "edit_file", "write_file":
		if e.Failed {
			break
		}
		var args struct {
			Path       string
			OldText    *string `json:"old_text"`
			NewText    *string `json:"new_text"`
			Content    *string
			ReplaceAll bool `json:"replace_all"`
		}
		if json.Unmarshal(e.Args, &args) == nil && args.Path != "" {
			if e.Text == "write_file" && args.Content != nil {
				suffix := fmt.Sprintf(" %s (%d lines)", args.Path, strings.Count(*args.Content, "\n")+1)
				if e.Output == "Created"+suffix || e.Output == "Wrote"+suffix {
					a.diffCard(c, e, args.Path, "", *args.Content, locales.NewFile())
					return
				}
			}
			if e.Text == "edit_file" && args.OldText != nil && *args.OldText != "" && args.NewText != nil {
				if n := editReplacements(e.Output, args.Path); n == 1 || (n > 1 && args.ReplaceAll) {
					note := ""
					if n > 1 {
						note = locales.ReplacedTimes(n)
					}
					a.diffCard(c, e, args.Path, *args.OldText, *args.NewText, note)
					return
				}
			}
		}
	case "shell":
		var args struct{ Command string }
		if json.Unmarshal(e.Args, &args) == nil && strings.TrimSpace(args.Command) != "" {
			a.shellCard(c, e)
			return
		}
	case "search", "list_dir":
		if e.Failed {
			break
		}
		if rows, count, ok := resultRows(e); ok {
			if a.cardStep(c, e, locales.Results(count), false) {
				a.cardOutput(c, func() {
					for _, row := range rows {
						ui.Text(c, row).Font("monospace").FontSize(13).TextColor(a.p.Fg2).Selectable().PreserveSpaces().SingleLine().Ellipsis("…").MinWidth(0).Tooltip(row)
					}
				})
			}
			return
		}
	}
	a.step(c, e)
}

func editReplacements(output, path string) int {
	rest, ok := strings.CutPrefix(output, "Edited "+path+" (")
	if !ok {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(rest, "%d replacement", &n); err != nil || n < 1 {
		return 0
	}
	suffix := "replacement)"
	if n != 1 {
		suffix = "replacements)"
	}
	if rest != fmt.Sprintf("%d %s", n, suffix) {
		return 0
	}
	return n
}

func (a *App) diffCard(c *ui.Context, e *client.Entry, path, oldText, newText, note string) {
	p := a.p
	// In a finished turn's work an edit is one line that opens to its card.
	if a.inWork && !a.editLine(c, e, path, oldText, newText) {
		return
	}
	box := ui.Column(c.Key("card:"+e.CallID)).Margin(8, 0, 14, 0).Radius(12).Background(p.Card).Clip()
	raise(box, p)
	diff := ui.Local(box, "diff", func() cardDiff { return cardDiff{} })
	if diff.lines == nil || diff.oldText != oldText || diff.newText != newText {
		*diff = cardDiff{oldText: oldText, newText: newText}
		diff.lines, diff.replaced = lineDiff(oldText, newText)
		diff.lineEndings = oldText != newText && strings.ReplaceAll(oldText, "\r\n", "\n") == strings.ReplaceAll(newText, "\r\n", "\n")
		for _, line := range diff.lines {
			if line.sign == "+" {
				diff.added++
			} else if line.sign == "−" {
				diff.removed++
			}
		}
	}
	lines := diff.lines
	if diff.replaced {
		note = strings.TrimPrefix(note+" · "+locales.ReplacedWhole(), " · ")
		note = strings.TrimPrefix(note, "· ")
	}
	if diff.lineEndings {
		if note != "" {
			note += " · "
		}
		note += locales.LineEndingsChanged()
	}
	box.Children(func() {
		ui.Row(c).Height(36).Padding(0, 8, 0, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			sym(c, "pencil", 15, p.Fg2)
			ui.Text(c, path).Font("monospace").FontSize(13.5).TextColor(p.Fg).SingleLine().Ellipsis("…").MinWidth(0).Tooltip(path)
			ui.Text(c, fmt.Sprintf("+%d", diff.added)).Font("monospace").FontSize(13).TextColor(p.Plus).Shrink(0)
			ui.Text(c, fmt.Sprintf("−%d", diff.removed)).Font("monospace").FontSize(13).TextColor(p.Minus).Shrink(0)
			ui.Spacer(c)
			a.fileUndo(c, e, path)
		})
		if note != "" {
			ui.Text(c, note).FontSize(12.5).TextColor(p.Mute).Padding(0, 12, 6, 12)
		}
		limit := min(12, len(lines))
		if a.opened[e.CallID] {
			limit = len(lines)
		}
		ui.Column(c).BorderWidth(.5, 0, 0, 0).BorderColor(p.Edge).Children(func() {
			for _, line := range lines[:limit] {
				bg, signColor := ui.Transparent, p.Mute
				if line.sign == "+" {
					bg, signColor = p.Plus.Alpha(.10), p.Plus
				} else if line.sign == "−" {
					bg, signColor = p.Minus.Alpha(.10), p.Minus
				}
				ui.Row(c).Height(20).Children(func() {
					ui.Text(c, strconv.Itoa(line.number)).Width(34).Shrink(0).Padding(0, 4, 0, 6).Font("monospace").FontSize(13).FixedLineHeight(20).TextColor(p.Faint).TextAlign(ui.End).Tooltip(locales.LineNumberInArgs())
					ui.Text(c, line.sign).Width(14).Height(20).Shrink(0).Font("monospace").FontSize(13).FixedLineHeight(20).TextColor(signColor).Background(bg)
					text := ui.Text(c, strings.TrimSuffix(line.text, "\n")).Grow(1).MinWidth(0).Height(20).Padding(0, 6).Font("monospace").FontSize(13).FixedLineHeight(20).TextColor(p.Fg).Background(bg).PreserveSpaces().SingleLine().Selectable()
					if !strings.HasSuffix(line.text, "\n") {
						text.Tooltip(locales.NoNewlineAtEnd())
					}
				})
			}
		})
		if len(lines) > 12 {
			label := locales.ShowMoreLines(len(lines) - 12)
			if a.opened[e.CallID] {
				label = locales.ShowLess()
			}
			if ui.ButtonBase(c.Key("expand")).Label(label).Height(28).Children(func() {
				ui.Text(c, label).FontSize(12.5).TextColor(p.Mute).Padding(6, 12)
			}).Clicked() {
				a.opened[e.CallID] = !a.opened[e.CallID]
			}
		}
	})
}

// cardStep keeps the existing step's dimensions, with a trailing result summary.
func (a *App) cardStep(c *ui.Context, e *client.Entry, summary string, failed bool) bool {
	p := a.p
	tool, _ := toolFor(e.Text)
	open := a.opened[e.CallID]
	row := ui.ButtonBase(c.Key("step:" + e.CallID)).Row().Height(25).Gap(8).AlignItems(ui.Center).Label(tool.verb + " " + e.Detail).Expanded(open)
	if row.Clicked() {
		open = !open
		a.opened[e.CallID] = open
	}
	row.Children(func() {
		ui.Box(c).Width(16).Center().Children(func() { sym(c, tool.symbol, 15, p.Mute) })
		ui.Text(c, tool.verb).FontSize(13.5).TextColor(p.Fg2).SingleLine().Shrink(0)
		ui.Text(c, e.Detail).Font("monospace").FontSize(13).TextColor(p.Fg).SingleLine().Ellipsis("…").MinWidth(0)
		ui.Spacer(c)
		color := p.Mute
		if failed {
			color = p.Minus
		}
		ui.Text(c, summary).FontSize(12.5).TextColor(color).SingleLine().Shrink(0)
		if row.Hovered() {
			sym(c, map[bool]string{true: "chevron-down", false: "chevron-right"}[open], 10, p.Mute)
		}
	})
	return open
}

func (a *App) cardOutput(c *ui.Context, content func()) {
	ui.Column(c).Padding(9, 11).Radius(8).Background(a.p.Chip).Margin(2, 0, 8, 24).Children(content)
}

func shellTail(output string) (lines []string, omitted int, exit string) {
	lines = strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	last := lines[len(lines)-1]
	if strings.HasPrefix(last, "[exit status ") && strings.HasSuffix(last, "]") {
		code := strings.TrimSuffix(strings.TrimPrefix(last, "[exit status "), "]")
		if n, err := strconv.Atoi(code); err == nil && n != 0 {
			exit = code
		}
	}
	if len(lines) > 20 {
		omitted = len(lines) - 20
		lines = lines[omitted:]
	}
	return lines, omitted, exit
}

func (a *App) shellCard(c *ui.Context, e *client.Entry) {
	lines, omitted, exit := shellTail(e.Output)
	summary := locales.Passed()
	if exit != "" {
		summary = locales.ExitCode(exit)
	}
	if e.Failed && summary == "" {
		summary = locales.Failed()
	}
	if a.cardStep(c, e, summary, exit != "" || e.Failed) {
		a.cardOutput(c, func() {
			if omitted > 0 {
				ui.Text(c, locales.OmittedLines(omitted)).FontSize(12.5).TextColor(a.p.Mute)
			}
			for i, line := range lines {
				color := a.p.Fg2
				if exit != "" && i == len(lines)-1 {
					color = a.p.Minus
				}
				ui.Text(c, line).Font("monospace").FontSize(13).TextColor(color).Selectable().PreserveSpaces()
			}
		})
	}
}

var searchLine = regexp.MustCompile(`^.+:[1-9][0-9]*:.*$`)
var searchFileLine = regexp.MustCompile(`^[1-9][0-9]*:.*$`)

// resultRows follows tools/search.go and files.go, falling back on clipped or malformed output.
func resultRows(e *client.Entry) (rows []string, count int, ok bool) {
	var args struct {
		Path, Pattern string
		Files         bool
	}
	if json.Unmarshal(e.Args, &args) != nil || strings.Contains(e.Output, " bytes cut …]") {
		return nil, 0, false
	}
	if e.Text == "search" && args.Pattern == "" {
		return nil, 0, false
	}
	if e.Text == "search" && e.Output == "No matches." {
		return []string{locales.NoMatches()}, 0, true
	}
	if e.Text == "list_dir" && e.Output == "(empty directory)" {
		return []string{locales.EmptyDir()}, 0, true
	}
	if e.Output == "" {
		return nil, 0, false
	}
	rows = strings.Split(strings.TrimSuffix(e.Output, "\n"), "\n")
	count = len(rows)
	for i, row := range rows {
		// ripgrep omits the filename when path names a single file.
		if row == "" || (e.Text == "search" && !args.Files && !searchLine.MatchString(row) && (args.Path == "" || !searchFileLine.MatchString(row))) {
			return nil, 0, false
		}
		if e.Text == "list_dir" && i == 1000 && i == len(rows)-1 && strings.HasPrefix(row, "[") && strings.HasSuffix(row, " more]") {
			n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(row, "["), " more]"))
			if err != nil || n < 1 {
				return nil, 0, false
			}
			count += n - 1
			rows[i] = locales.MoreItems(n)
		}
	}
	return rows, count, true
}

// fileUndo takes back, or redoes, what the card's turn did to its file.
func (a *App) fileUndo(c *ui.Context, e *client.Entry, path string) {
	p := a.p
	turn, abs := a.changedFile(e, path)
	reverted := turn != 0 && a.C.Changes.FileReverted(turn, abs)
	label, icon := locales.Undo(), "arrow-back-up"
	if reverted {
		label, icon = locales.Redo(), "arrow-forward-up"
	}
	b := ui.ButtonBase(c).Row().Height(28).Padding(0, 10).Gap(6).Radius(7).AlignItems(ui.Center).Label(label + " " + path)
	switch {
	case turn == 0:
		b.Disabled(true).Tooltip(locales.TurnChangesLoading())
	case a.C.Busy:
		b.Disabled(true).Tooltip(locales.CantRestoreWhileRunning())
	default:
		tip := locales.UndoFileTip(path)
		if reverted {
			tip = locales.RedoFileTip(path)
		}
		b.Tooltip(tip)
		if b.Hovered() {
			b.Background(p.Hover)
		}
	}
	b.Children(func() {
		sym(c, icon, 15, p.Fg2)
		ui.Text(c, label).FontSize(13.5).FontWeight(500).TextColor(p.Fg2).SingleLine()
	})
	if b.Clicked() {
		a.do(a.C.Revert(turn, abs, reverted))
	}
}

// changedFile finds the entry's file among its turn's recorded changes,
// whose paths are absolute.
func (a *App) changedFile(e *client.Entry, path string) (int64, string) {
	if e.Turn < 0 || e.Turn >= len(a.C.Turns) || a.C.Current == nil {
		return 0, ""
	}
	seq := a.C.Turns[e.Turn].Seq
	want := path
	if !filepath.IsAbs(want) {
		want = filepath.Join(a.C.Current.Cwd, want)
	}
	for _, t := range a.C.Changes.Turns {
		if t.TurnSeq != seq {
			continue
		}
		for _, f := range t.Files {
			if f.Path == want {
				return seq, f.Path
			}
		}
	}
	return 0, ""
}
