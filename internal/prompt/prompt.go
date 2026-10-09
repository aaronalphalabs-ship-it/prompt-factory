// Package prompt loads, renders and versions prompt files.
//
// A prompt is a Markdown file in prompts/ using Go template syntax:
//
//	Write an Amazon title for {{.product}} in {{.lang | default "English"}}.
//
// Every time a prompt is run, its exact text is snapshotted by content hash
// under .pf/versions/<name>/, so results can always be traced to the prompt
// version that produced them.
package prompt

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"
)

type Prompt struct {
	Name string
	Text string
	Hash string // first 8 hex chars of sha256(Text)
}

func Load(dir, name string) (*Prompt, error) {
	name = strings.TrimSuffix(name, ".md")
	b, err := os.ReadFile(filepath.Join(dir, name+".md"))
	if err != nil {
		return nil, fmt.Errorf("prompt %q: %w", name, err)
	}
	return &Prompt{Name: name, Text: string(b), Hash: hash(b)}, nil
}

func hash(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])[:8]
}

var funcs = template.FuncMap{
	"default": func(def string, v any) string {
		if v == nil {
			return def
		}
		if s, ok := v.(string); ok && s == "" {
			return def
		}
		return fmt.Sprint(v)
	},
	"upper": strings.ToUpper,
	"lower": strings.ToLower,
}

// Render fills the template. Missing variables render as empty strings
// unless strict is set, in which case they are an error.
func (p *Prompt) Render(vars map[string]string, strict bool) (string, error) {
	t, err := template.New(p.Name).Funcs(funcs).Parse(p.Text)
	if err != nil {
		return "", fmt.Errorf("template %s: %w", p.Name, err)
	}
	m := map[string]any{}
	for k, v := range vars {
		m[k] = v
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, m); err != nil {
		return "", fmt.Errorf("render %s: %w", p.Name, err)
	}
	out := buf.String()
	if strings.Contains(out, "<no value>") {
		if strict {
			return "", fmt.Errorf("render %s: a variable is missing — pass it with --var key=value (or use {{.key | default \"x\"}})", p.Name)
		}
		out = strings.ReplaceAll(out, "<no value>", "")
	}
	return strings.TrimSpace(out), nil
}

// Snapshot stores this exact version once.
func (p *Prompt) Snapshot(stateDir string) error {
	dir := filepath.Join(stateDir, "versions", p.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*-"+p.Hash+".md"))
	if len(matches) > 0 {
		return nil
	}
	f := filepath.Join(dir, time.Now().UTC().Format("20060102T150405Z")+"-"+p.Hash+".md")
	return os.WriteFile(f, []byte(p.Text), 0o644)
}

type Version struct {
	File string
	When string
	Hash string
}

func History(stateDir, name string) ([]Version, error) {
	files, err := filepath.Glob(filepath.Join(stateDir, "versions", name, "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var out []Version
	for _, f := range files {
		base := strings.TrimSuffix(filepath.Base(f), ".md")
		parts := strings.SplitN(base, "-", 2)
		if len(parts) != 2 {
			continue
		}
		out = append(out, Version{File: f, When: parts[0], Hash: parts[1]})
	}
	return out, nil
}

func List(dir string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range files {
		out = append(out, strings.TrimSuffix(filepath.Base(f), ".md"))
	}
	sort.Strings(out)
	return out, nil
}

// ParseVars turns ["k=v", ...] into a map.
func ParseVars(kv []string) (map[string]string, error) {
	m := map[string]string{}
	for _, s := range kv {
		k, v, ok := strings.Cut(s, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("bad --var %q, want key=value", s)
		}
		m[k] = v
	}
	return m, nil
}
