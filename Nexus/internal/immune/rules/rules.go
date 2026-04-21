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

// Package rules contains the compiled Tier-0 immune scanner heuristics.
// Rules are Go code (not interpreted config) for sub-millisecond evaluation.
package rules

import (
	"bytes"
	"encoding/base64"
	"regexp"
	"strings"
	"unicode"
)

// Result is the evaluation outcome returned by ScanContent.
type Result struct {
	Action            string // "accept", "quarantine", "reject", "flag"
	Rule              string // rule ID that triggered; empty when Action is "accept"
	Details           string // human-readable description
	NormalizedContent string // homoglyph-normalized content; non-empty only for T0-006
}

var (
	reT0001       = regexp.MustCompile(`(?i)(ignore|disregard|forget|bypass|override)\s+(\w+\s+){0,2}(previous|prior|all|any|every|safety|system)\s+(\w+\s+){0,2}(instructions|prompts|rules|guidelines|restrictions|constraints|directives?)`)
	reT0002       = regexp.MustCompile(`(?i)(system|admin|root)\s*:\s*(you are|act as|pretend|roleplay)`)
	reT0003       = regexp.MustCompile(`(?i)(ADMIN_OVERRIDE|SUDO_MODE|DEBUG_MODE|JAILBREAK)`)
	reT0007       = regexp.MustCompile(`(?i)(DROP\s+TABLE|UNION\s+SELECT|;\s*DELETE|INSERT\s+INTO)`)
	reT0013       = regexp.MustCompile(`(?i)(you are now|act as|pretend to be|roleplay as|behave as|become)\s+(DAN|STAN|DUDE|AIM|KEVIN|BISH|MONGO|an?\s+(unrestricted|uncensored|unfiltered|unlimited|evil|unaligned)\s+\w+)`)
	reT0014a      = regexp.MustCompile(`(?i)(output|reveal|show|print|display|leak|exfiltrate)\s+(\w+\s+)?(your|the|my)?\s*(system prompt|instructions|initial prompt|hidden prompt|original prompt)`)
	reT0014b      = regexp.MustCompile(`(?i)(execute|eval|run|exec)\s*[:(]\s*(rm\s|curl\s|wget\s|nc\s|bash\s|sh\s|python|import\s+os)`)
	reBase64Block = regexp.MustCompile(`[A-Za-z0-9+/]{500,}={0,2}`)
)

const maxContentBytes = 100 * 1024 // T0-012: 100 KB hard limit

// execMagic is the set of known executable file magic byte sequences.
var execMagic = [][]byte{
	{0x4D, 0x5A},             // PE/MZ (Windows)
	{0x7F, 0x45, 0x4C, 0x46}, // ELF (Linux)
	{0xCA, 0xFE, 0xBA, 0xBE}, // Mach-O fat binary
	{0xFE, 0xED, 0xFA, 0xCE}, // Mach-O 32-bit
	{0xFE, 0xED, 0xFA, 0xCF}, // Mach-O 64-bit
	{0x23, 0x21},             // shebang (#!)
}

// homoglyphs maps visually confusable Cyrillic code points to their Latin equivalents.
var homoglyphs = map[rune]rune{
	// lowercase
	'а': 'a', 'е': 'e', 'о': 'o', 'р': 'p', 'с': 'c',
	'у': 'y', 'х': 'x', 'і': 'i', 'ј': 'j',
	// uppercase
	'А': 'A', 'В': 'B', 'Е': 'E', 'К': 'K', 'М': 'M',
	'Н': 'H', 'О': 'O', 'Р': 'P', 'С': 'C', 'Т': 'T',
	'У': 'Y', 'Х': 'X',
}

// latinScript is the set of ISO-639-1 codes that use the Latin alphabet.
var latinScript = map[string]bool{
	"en": true, "fr": true, "de": true, "es": true, "it": true,
	"pt": true, "nl": true, "sv": true, "da": true, "no": true,
	"nb": true, "nn": true, "fi": true, "pl": true, "cs": true,
	"ro": true, "hu": true, "tr": true, "id": true, "ms": true,
	"cy": true, "ga": true, "af": true, "sq": true, "eu": true,
}

// ScanContent runs all 12 Tier-0 rules in order and returns the first
// non-accept result, or accept if all rules pass.
// embDim=0 disables the T0-008 embedding-dimension check.
func ScanContent(content string, metadata map[string]any, embedding []float32, embDim int) Result {
	// T0-012: content size cap
	if len(content) > maxContentBytes {
		return Result{Action: "reject", Rule: "T0-012", Details: "content exceeds 100KB limit"}
	}

	// T0-011: null bytes
	if strings.ContainsRune(content, '\x00') {
		return Result{Action: "reject", Rule: "T0-011", Details: "null byte in content"}
	}

	// T0-001: prompt injection
	if reT0001.MatchString(content) {
		return Result{Action: "quarantine", Rule: "T0-001", Details: "prompt injection pattern detected"}
	}

	// T0-002: role hijacking
	if reT0002.MatchString(content) {
		return Result{Action: "quarantine", Rule: "T0-002", Details: "role hijacking pattern detected"}
	}

	// T0-003: admin override keywords
	if reT0003.MatchString(content) {
		return Result{Action: "quarantine", Rule: "T0-003", Details: "admin override keyword detected"}
	}

	// T0-013: jailbreak persona invocation
	if reT0013.MatchString(content) {
		return Result{Action: "quarantine", Rule: "T0-013", Details: "jailbreak persona invocation detected"}
	}

	// T0-014: system prompt exfiltration / command execution
	if reT0014a.MatchString(content) {
		return Result{Action: "quarantine", Rule: "T0-014", Details: "system prompt exfiltration pattern detected"}
	}
	if reT0014b.MatchString(content) {
		return Result{Action: "quarantine", Rule: "T0-014", Details: "command execution pattern detected"}
	}

	// T0-004: base64-encoded executable payload
	for _, seg := range reBase64Block.FindAllString(content, -1) {
		if segHasExecMagic(seg) {
			return Result{Action: "quarantine", Rule: "T0-004", Details: "base64-encoded executable payload detected"}
		}
	}

	// T0-005: token flooding (same word > 50 times)
	if words := strings.Fields(content); len(words) > 0 {
		counts := make(map[string]int, len(words))
		for _, w := range words {
			counts[strings.ToLower(w)]++
		}
		for _, c := range counts {
			if c > 50 {
				return Result{Action: "reject", Rule: "T0-005", Details: "token flooding detected"}
			}
		}
	}

	// T0-006: homoglyph substitution — normalize and flag
	if normalized, found := detectHomoglyphs(content); found {
		return Result{
			Action:            "flag",
			Rule:              "T0-006",
			Details:           "unicode homoglyph substitution detected",
			NormalizedContent: normalized,
		}
	}

	// T0-007: SQL injection patterns
	if reT0007.MatchString(content) {
		return Result{Action: "quarantine", Rule: "T0-007", Details: "SQL injection pattern detected"}
	}

	// T0-008: embedding dimension mismatch
	if embDim > 0 && len(embedding) > 0 && len(embedding) != embDim {
		return Result{Action: "reject", Rule: "T0-008", Details: "embedding dimension mismatch"}
	}

	// T0-009: short content with embedding (embedding injection)
	if len(embedding) > 0 && len(content) < 5 {
		return Result{Action: "flag", Rule: "T0-009", Details: "short content with embedding (possible injection)"}
	}

	// T0-010: language metadata mismatch
	if r := detectLangMismatch(content, metadata); r != nil {
		return *r
	}

	return Result{Action: "accept"}
}

// segHasExecMagic decodes a base64 segment and checks for executable magic bytes.
func segHasExecMagic(seg string) bool {
	var decoded []byte
	var err error
	decoded, err = base64.StdEncoding.DecodeString(seg)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(seg)
		if err != nil {
			return false
		}
	}
	for _, magic := range execMagic {
		if len(decoded) >= len(magic) && bytes.Equal(decoded[:len(magic)], magic) {
			return true
		}
	}
	return false
}

// detectHomoglyphs scans s for Cyrillic lookalike characters.
// Returns the normalized string and true if any were found.
func detectHomoglyphs(s string) (string, bool) {
	found := false
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		if latin, ok := homoglyphs[r]; ok {
			sb.WriteRune(latin)
			found = true
		} else {
			sb.WriteRune(r)
		}
	}
	if !found {
		return "", false
	}
	return sb.String(), true
}

// detectLangMismatch returns a flag result when metadata claims a Latin-script
// language but the content has a high ratio of non-ASCII characters.
func detectLangMismatch(content string, metadata map[string]any) *Result {
	var lang string
	for _, key := range []string{"lang", "language"} {
		if v, ok := metadata[key]; ok {
			if s, ok := v.(string); ok {
				lang = strings.ToLower(strings.SplitN(s, "-", 2)[0])
				break
			}
		}
	}
	if lang == "" || !latinScript[lang] {
		return nil
	}
	total, nonASCII := 0, 0
	for _, r := range content {
		total++
		if r > unicode.MaxASCII {
			nonASCII++
		}
	}
	if total > 10 && nonASCII*100/total > 30 {
		return &Result{
			Action:  "flag",
			Rule:    "T0-010",
			Details: "language metadata mismatch: non-ASCII ratio too high for claimed Latin-script language",
		}
	}
	return nil
}
