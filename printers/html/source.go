package html

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"sort"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"golang.org/x/tools/cover"

	"github.com/erioch/covlens"
)

const contextLines = 3

// RenderSource syntax-highlights a Go source file and overlays coverage
// information from the given profile blocks.
//
// When hunks are provided only the changed line ranges (± contextLines) are
// rendered, separated by ··· dividers — like a GitHub PR diff view.
// When hunks is nil the full file is shown.
func RenderSource(filePath string, blocks []cover.ProfileBlock, hunks []covlens.Hunk) (template.HTML, error) {
	src, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("reading source %s: %w", filePath, err)
	}

	lexer := lexers.Get("go")
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	style := styles.Get("monokai")
	if style == nil {
		style = styles.Fallback
	}

	formatter := chromahtml.New(
		chromahtml.WithLineNumbers(true),
		chromahtml.WithClasses(true),
	)

	iterator, err := lexer.Tokenise(nil, string(src))
	if err != nil {
		return "", fmt.Errorf("tokenising %s: %w", filePath, err)
	}

	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, iterator); err != nil {
		return "", fmt.Errorf("formatting %s: %w", filePath, err)
	}

	// Build per-line coverage map: 0=not instrumented, 1=uncovered, 2=covered.
	lineCount := strings.Count(string(src), "\n") + 1
	lineCov := make([]int, lineCount+1)
	for _, b := range blocks {
		for line := b.StartLine; line <= b.EndLine && line <= lineCount; line++ {
			if b.Count > 0 {
				lineCov[line] = 2
			} else if lineCov[line] == 0 {
				lineCov[line] = 1
			}
		}
	}

	header, lines, footer := splitAndAnnotate(buf.String(), lineCov)

	var sb strings.Builder
	sb.WriteString(header)

	if len(hunks) == 0 {
		for _, l := range lines {
			sb.WriteString(l)
		}
	} else {
		const sep = `<span class="line diff-sep"><span class="ln">···</span><span class="cl"></span></span>`
		visible := buildVisibleLines(hunks, len(lines))
		prevShown := 0
		for _, lineNum := range visible {
			if lineNum < 1 || lineNum > len(lines) {
				continue
			}
			if lineNum > prevShown+1 && (prevShown > 0 || lineNum > 1) {
				sb.WriteString(sep)
			}
			sb.WriteString(lines[lineNum-1])
			prevShown = lineNum
		}
		if prevShown > 0 && prevShown < len(lines) {
			sb.WriteString(sep)
		}
	}

	sb.WriteString(footer)
	return template.HTML(sb.String()), nil
}

// splitAndAnnotate splits chroma HTML output into (header, per-line spans, footer)
// and injects covered/uncovered CSS classes into each line span.
//
// Chroma emits each source line as <span class="line">...</span>. Splitting on
// the prefix <span class="line (without the closing ") lets us prepend coverage
// class suffixes before the closing " and reconstruct valid HTML.
func splitAndAnnotate(html string, lineCov []int) (header string, lines []string, footer string) {
	const marker = `<span class="line`
	sections := strings.Split(html, marker)
	if len(sections) <= 1 {
		return html, nil, ""
	}

	header = sections[0]

	for i, sec := range sections[1:] {
		lineNum := i + 1
		var covSuffix string
		if lineNum < len(lineCov) {
			switch lineCov[lineNum] {
			case 2:
				covSuffix = " covered"
			case 1:
				covSuffix = " uncovered"
			}
		}
		// sec starts with `"` (the closing quote of class="line..."), so
		// marker + covSuffix + sec → <span class="line covered">...
		lines = append(lines, marker+covSuffix+sec)
	}

	// The last section ends with </span></code></pre>; separate the footer.
	if len(lines) > 0 {
		last := lines[len(lines)-1]
		if closeIdx := strings.LastIndex(last, "</span>"); closeIdx >= 0 {
			footer = last[closeIdx+len("</span>"):]
			lines[len(lines)-1] = last[:closeIdx+len("</span>")]
		}
	}

	return header, lines, footer
}

// buildVisibleLines returns a sorted list of 1-based line numbers that fall
// within any hunk expanded by contextLines on both sides.
func buildVisibleLines(hunks []covlens.Hunk, totalLines int) []int {
	visible := make(map[int]struct{})
	for _, h := range hunks {
		start := h.Start - contextLines
		if start < 1 {
			start = 1
		}
		end := h.End + contextLines
		if end > totalLines {
			end = totalLines
		}
		for l := start; l <= end; l++ {
			visible[l] = struct{}{}
		}
	}
	sorted := make([]int, 0, len(visible))
	for l := range visible {
		sorted = append(sorted, l)
	}
	sort.Ints(sorted)
	return sorted
}
