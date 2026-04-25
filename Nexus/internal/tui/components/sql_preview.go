// Copyright © 2026 BubbleFish Technologies, Inc.
//
// This file is part of BubbleFish Nexus.
//
// BubbleFish Nexus is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// BubbleFish Nexus is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with BubbleFish Nexus. If not, see <https://www.gnu.org/licenses/>.

package components

import (
	"fmt"
	"strings"

	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/lipgloss"
)

var sqlKeywords = map[string]bool{
	"SELECT": true, "FROM": true, "WHERE": true, "AND": true,
	"OR": true, "ORDER": true, "BY": true, "LIMIT": true,
	"BETWEEN": true, "INSERT": true, "INTO": true, "VALUES": true,
	"UPDATE": true, "SET": true, "DELETE": true, "JOIN": true,
	"LEFT": true, "RIGHT": true, "INNER": true, "ON": true,
	"GROUP": true, "HAVING": true, "DESC": true, "ASC": true,
	"AS": true, "IN": true, "NOT": true, "NULL": true, "LIKE": true,
}

// SQLPreviewProps configures the SQL preview renderer.
type SQLPreviewProps struct {
	SQL    string
	Params []string
	Width  int
}

// RenderSQLPreview renders SQL with keyword highlighting and parameter list.
func RenderSQLPreview(p SQLPreviewProps) string {
	header := lipgloss.NewStyle().Foreground(styles.TextMuted).Bold(true).
		Render("◈ SQL PREVIEW (Stage 3 — Structured)")

	if p.SQL == "" {
		body := lipgloss.NewStyle().Foreground(styles.TextMuted).
			Render("  — no structured query yet —")
		return lipgloss.JoinVertical(lipgloss.Left, header, body)
	}

	highlighted := highlightSQL(p.SQL, p.Width-2)

	var paramsBlock string
	if len(p.Params) > 0 {
		pairs := make([]string, len(p.Params))
		for i, v := range p.Params {
			pairs[i] = fmt.Sprintf("$%d=%s", i+1, v)
		}
		paramsBlock = lipgloss.NewStyle().Foreground(styles.ColorGreen).
			Render("  " + strings.Join(pairs, "  "))
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, "", highlighted, "", paramsBlock)
}

func highlightSQL(sql string, maxWidth int) string {
	kwStyle := lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true)
	strStyle := lipgloss.NewStyle().Foreground(styles.ColorGreen)
	numStyle := lipgloss.NewStyle().Foreground(styles.ColorPurple)
	idStyle := lipgloss.NewStyle().Foreground(styles.TextWhiteDim)
	paramStyle := lipgloss.NewStyle().Foreground(styles.ColorAmber)

	var result strings.Builder
	tokens := strings.Fields(sql)
	col := 2
	result.WriteString("  ")

	for i, tok := range tokens {
		if i > 0 {
			result.WriteString(" ")
			col++
		}

		if col+len(tok) > maxWidth && maxWidth > 20 {
			result.WriteString("\n  ")
			col = 2
		}

		upper := strings.ToUpper(tok)
		switch {
		case sqlKeywords[upper]:
			result.WriteString(kwStyle.Render(upper))
		case strings.HasPrefix(tok, "'") || strings.HasPrefix(tok, "\""):
			result.WriteString(strStyle.Render(tok))
		case strings.HasPrefix(tok, "$"):
			result.WriteString(paramStyle.Render(tok))
		case len(tok) > 0 && tok[0] >= '0' && tok[0] <= '9':
			result.WriteString(numStyle.Render(tok))
		default:
			result.WriteString(idStyle.Render(tok))
		}
		col += len(tok)
	}

	return result.String()
}
