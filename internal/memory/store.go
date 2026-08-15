package memory

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// IndexFile is the per-directory index. It has no frontmatter — it is a
	// bare markdown list (§I.3) — and is excluded from listings but still
	// readable.
	IndexFile = "MEMORY.md"

	// IndexName is the index as a completion and match candidate (§II.10).
	IndexName = "MEMORY"
)

// Memory is one memory file. Every memory has two names (§I.5): the filename
// is the identifier used for matching and completion, the frontmatter name is
// the human title.
type Memory struct {
	File        string // "project_roadmap.md"
	Name        string // "project_roadmap" — completion + match key
	Title       string // frontmatter name → index title → Name
	Description string // frontmatter description → index hook → ""
	Type        string // "project"; "" if absent
	Path        string // absolute
	Body        string // file content with frontmatter stripped
	Raw         []byte // the file verbatim, for byte-identical passthrough
}

// Store is one memory directory and the memories in it.
type Store struct {
	Dir      string // absolute memory directory
	Project  string // human name: trailing slug segment, e.g. "courier"
	Memories []Memory
	HasIndex bool
	Index    *Memory // MEMORY.md, excluded from Memories but still readable
}

type indexEntry struct {
	title string
	hook  string
}

// Readable is the pool `read` and completion match against: the memories plus
// the index, which is hidden from `list` but must still be readable (§II.5).
func (s Store) Readable() []Memory {
	if s.Index == nil {
		return s.Memories
	}
	out := make([]Memory, 0, len(s.Memories)+1)
	out = append(out, s.Memories...)
	return append(out, *s.Index)
}

// Load reads a memory directory. Never fails on bad data: an unreadable file
// is skipped, a missing index is simply absent (§II.2). Project is left empty:
// naming a project means interpreting its slug, which belongs to the resolve
// package, so callers fill it in.
func Load(dir string) (Store, error) {
	s := Store{Dir: dir}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return s, err
	}
	var idx map[string]indexEntry
	if data, err := os.ReadFile(filepath.Join(dir, IndexFile)); err == nil {
		idx = parseIndex(data)
		s.HasIndex = true
		im := parseMemory(IndexFile, data)
		im.Path = filepath.Join(dir, IndexFile)
		s.Index = &im
	}
	var names []string
	for _, e := range entries {
		// A flat directory of .md files, no subdirectories (§I.2); courier also
		// holds a .consolidate-lock, so filter rather than assume.
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || e.Name() == IndexFile {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		m := parseMemory(name, data)
		m.Path = path
		m.applyIndex(idx)
		s.Memories = append(s.Memories, m)
	}
	return s, nil
}

// applyIndex fills in whatever the frontmatter did not supply, per the
// precedence of §II.4. An index entry pointing at a file that is not here is
// simply never consulted, so index drift costs nothing.
func (m *Memory) applyIndex(idx map[string]indexEntry) {
	e, ok := idx[m.File]
	if !ok {
		return
	}
	if m.Title == m.Name && e.title != "" {
		m.Title = e.title
	}
	if m.Description == "" {
		m.Description = e.hook
	}
}

// parseMemory parses one memory file. The frontmatter subset here is small
// enough to hand-parse (§II.11): a --- fence, `key: value` lines, and one
// level of 2-space nesting under `metadata:`.
func parseMemory(file string, data []byte) Memory {
	m := Memory{
		File: file,
		Name: strings.TrimSuffix(file, ".md"),
		Raw:  data,
	}
	top, nested, body := splitFrontmatter(string(data))
	m.Body = body
	m.Title = top["name"]
	if m.Title == "" {
		m.Title = m.Name
	}
	m.Description = top["description"]
	// type lives at the top level or under metadata (§I.4); both shapes occur
	// inside the same directory, so try both.
	m.Type = top["type"]
	if m.Type == "" {
		m.Type = nested["type"]
	}
	return m
}

// splitFrontmatter returns the top-level keys, the keys nested one level under
// a parent (metadata), and the body. An absent or unterminated fence yields no
// body loss — the file is returned as the body.
func splitFrontmatter(s string) (top, nested map[string]string, body string) {
	top, nested = map[string]string{}, map[string]string{}
	lines := strings.Split(s, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r \t") != "---" {
		return top, nested, s
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r \t") == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		// Unterminated fence: harvest what keys we can, keep the whole file as
		// the body so nothing is silently swallowed (§II.2).
		parseFrontmatterLines(lines[1:], top, nested)
		return top, nested, s
	}
	parseFrontmatterLines(lines[1:end], top, nested)
	rest := lines[end+1:]
	// Drop the single blank line that conventionally follows the fence.
	for len(rest) > 0 && strings.TrimSpace(rest[0]) == "" {
		rest = rest[1:]
	}
	return top, nested, strings.Join(rest, "\n")
}

// parseFrontmatterLines fills top and nested from the lines between the fences.
// Nesting is exactly 2 spaces (§I.4) but any indent is accepted.
func parseFrontmatterLines(lines []string, top, nested map[string]string) {
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		indented := line != strings.TrimLeft(line, " \t")
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		// The value may contain ':' — Cut already took only the first one.
		value = unquote(strings.TrimSpace(value))
		if indented {
			nested[key] = value
		} else {
			top[key] = value
		}
	}
}

// unquote strips YAML quoting and its escapes. Real descriptions are
// double-quoted and contain \" (§I.4).
func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		inner := s[1 : len(s)-1]
		var b strings.Builder
		b.Grow(len(inner))
		for i := 0; i < len(inner); i++ {
			if inner[i] == '\\' && i+1 < len(inner) {
				switch inner[i+1] {
				case '"', '\\', '/':
					b.WriteByte(inner[i+1])
					i++
					continue
				case 'n':
					b.WriteByte('\n')
					i++
					continue
				case 't':
					b.WriteByte('\t')
					i++
					continue
				}
			}
			b.WriteByte(inner[i])
		}
		return b.String()
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
	}
	return s
}

// indexSeparators are tried longest-first so "--" is not mistaken for "-".
// Every observed line uses the em-dash (§I.3); the rest are tolerated.
var indexSeparators = []string{" — ", " – ", " -- ", " - "}

// parseIndex reads MEMORY.md into file → {title, hook}. Shape is
// `- [<Title>](<file.md>) — <hook>`; a missing hook is fine, and anything that
// is not a list line is ignored.
func parseIndex(data []byte) map[string]indexEntry {
	out := map[string]indexEntry{}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		line = strings.TrimPrefix(line, "-")
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[") {
			continue
		}
		close := strings.Index(line, "](")
		if close < 0 {
			continue
		}
		title := line[1:close]
		rest := line[close+2:]
		end := strings.Index(rest, ")")
		if end < 0 {
			continue
		}
		file := strings.TrimSpace(rest[:end])
		hook := strings.TrimSpace(rest[end+1:])
		for _, sep := range indexSeparators {
			if strings.HasPrefix(hook, strings.TrimLeft(sep, " ")) {
				hook = strings.TrimSpace(strings.TrimPrefix(hook, strings.TrimLeft(sep, " ")))
				break
			}
		}
		if file != "" {
			out[file] = indexEntry{title: title, hook: hook}
		}
	}
	return out
}
