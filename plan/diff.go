package plan

import (
	"fmt"
	"strings"
)

// unifiedDiff is `diff -u` for whole files: label headers, hunks with three lines of context.
func unifiedDiff(oldLabel, newLabel, oldBody, newBody string) string {
	a := splitLines(oldBody)
	b := splitLines(newBody)
	ops := editScript(a, b)
	var s strings.Builder
	s.WriteString("--- " + oldLabel + "\n")
	s.WriteString("+++ " + newLabel + "\n")
	const ctx = 3
	i := 0
	for i < len(ops) {
		if ops[i].kind == ' ' {
			i++
			continue
		}
		// hunk start: back off up to ctx equal lines
		start := i - ctx
		if start < 0 {
			start = 0
		}
		end := i
		for end < len(ops) {
			if ops[end].kind != ' ' {
				end++
				continue
			}
			// a run of equal lines: keep it if a change follows within 2*ctx
			j := end
			for j < len(ops) && ops[j].kind == ' ' {
				j++
			}
			if j < len(ops) && j-end <= ctx {
				end = j
				continue
			}
			break
		}
		if end < len(ops) {
			end += ctx
			if end > len(ops) {
				end = len(ops)
			}
		}
		s.WriteString(hunkHeader(ops, start, end))
		for _, op := range ops[start:end] {
			s.WriteString(string(op.kind) + op.text + "\n")
		}
		i = end
	}
	return s.String()
}

func splitLines(body string) []string {
	if body == "" {
		return nil
	}
	lines := strings.Split(body, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

type op struct {
	kind byte // ' ', '-', '+'
	text string
}

// editScript is the line diff by longest common subsequence.
func editScript(a, b []string) []op {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var ops []op
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{' ', a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, op{'-', a[i]})
			i++
		default:
			ops = append(ops, op{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, op{'+', b[j]})
	}
	return ops
}

// hunkHeader is the `@@ -a,b +c,d @@` line of a range.
func hunkHeader(ops []op, start, end int) string {
	oldStart, newStart := 1, 1
	for i := 0; i < start; i++ {
		switch ops[i].kind {
		case ' ':
			oldStart++
			newStart++
		case '-':
			oldStart++
		case '+':
			newStart++
		}
	}
	oldLen, newLen := 0, 0
	for _, op := range ops[start:end] {
		switch op.kind {
		case ' ':
			oldLen++
			newLen++
		case '-':
			oldLen++
		case '+':
			newLen++
		}
	}
	return fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", oldStart, oldLen, newStart, newLen)
}
