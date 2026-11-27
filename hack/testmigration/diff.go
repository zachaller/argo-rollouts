package main

import (
	"fmt"
	"strings"
)

// lineDiff returns a unified-style diff of two texts with the given number of
// context lines, or "" when they are equal. It is a plain longest-common-
// subsequence diff, which is enough for function bodies.
func lineDiff(a, b string, context int) string {
	if a == b {
		return ""
	}
	al := strings.Split(a, "\n")
	bl := strings.Split(b, "\n")

	// lcs[i][j] is the length of the LCS of al[i:] and bl[j:].
	lcs := make([][]int, len(al)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(bl)+1)
	}
	for i := len(al) - 1; i >= 0; i-- {
		for j := len(bl) - 1; j >= 0; j-- {
			if al[i] == bl[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	type op struct {
		kind byte // ' ', '-', '+'
		text string
	}
	var ops []op
	i, j := 0, 0
	for i < len(al) && j < len(bl) {
		switch {
		case al[i] == bl[j]:
			ops = append(ops, op{' ', al[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, op{'-', al[i]})
			i++
		default:
			ops = append(ops, op{'+', bl[j]})
			j++
		}
	}
	for ; i < len(al); i++ {
		ops = append(ops, op{'-', al[i]})
	}
	for ; j < len(bl); j++ {
		ops = append(ops, op{'+', bl[j]})
	}

	// Keep only changed lines and their context.
	keep := make([]bool, len(ops))
	for idx, o := range ops {
		if o.kind == ' ' {
			continue
		}
		lo, hi := idx-context, idx+context
		if lo < 0 {
			lo = 0
		}
		if hi >= len(ops) {
			hi = len(ops) - 1
		}
		for k := lo; k <= hi; k++ {
			keep[k] = true
		}
	}

	var sb strings.Builder
	skipped := false
	for idx, o := range ops {
		if !keep[idx] {
			skipped = true
			continue
		}
		if skipped && sb.Len() > 0 {
			sb.WriteString("  ...\n")
		}
		skipped = false
		fmt.Fprintf(&sb, "%c %s\n", o.kind, o.text)
	}
	return sb.String()
}
