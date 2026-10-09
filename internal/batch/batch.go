// Package batch runs one prompt over every row of a CSV file concurrently.
package batch

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/aaronalphalabs-ship-it/prompt-factory/internal/check"
	"github.com/aaronalphalabs-ship-it/prompt-factory/internal/prompt"
	"github.com/aaronalphalabs-ship-it/prompt-factory/internal/provider"
)

type Options struct {
	In, Out     string
	Column      string // output column name
	Concurrency int
	System      string
	LimitKey    string // e.g. amazon.title; enforced if set
	AutoTrim    bool
	Limits      map[string]int
	Banned      []string
	Progress    func(done, total int)
}

type Stats struct {
	Rows, OK, Failed, Issues, Tokens int
}

func Run(ctx context.Context, p *prompt.Prompt, c *provider.Client, o Options) (*Stats, error) {
	f, err := os.Open(o.In)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	var rows [][]string
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		rows = append(rows, rec)
	}
	if o.Column == "" {
		o.Column = "output"
	}
	if o.Concurrency < 1 {
		o.Concurrency = 4
	}

	outputs := make([]string, len(rows))
	notes := make([]string, len(rows))
	st := &Stats{Rows: len(rows)}
	var mu sync.Mutex
	sem := make(chan struct{}, o.Concurrency)
	var wg sync.WaitGroup
	done := 0
	for i, rec := range rows {
		vars := map[string]string{}
		for j, h := range header {
			if j < len(rec) {
				vars[h] = rec[j]
			}
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, vars map[string]string) {
			defer wg.Done()
			defer func() { <-sem }()
			text, note, tokens := one(ctx, p, c, o, vars)
			mu.Lock()
			outputs[i], notes[i] = text, note
			st.Tokens += tokens
			if text == "" && note != "" {
				st.Failed++
			} else {
				st.OK++
				if note != "" {
					st.Issues++
				}
			}
			done++
			if o.Progress != nil {
				o.Progress(done, len(rows))
			}
			mu.Unlock()
		}(i, vars)
	}
	wg.Wait()

	out, err := os.Create(o.Out)
	if err != nil {
		return nil, err
	}
	defer out.Close()
	w := csv.NewWriter(out)
	_ = w.Write(append(append([]string{}, header...), o.Column, o.Column+"_notes"))
	for i, rec := range rows {
		row := append([]string{}, rec...)
		for len(row) < len(header) {
			row = append(row, "")
		}
		_ = w.Write(append(row, outputs[i], notes[i]))
	}
	w.Flush()
	return st, w.Error()
}

func one(ctx context.Context, p *prompt.Prompt, c *provider.Client, o Options, vars map[string]string) (string, string, int) {
	text, err := p.Render(vars, false)
	if err != nil {
		return "", "error: " + err.Error(), 0
	}
	res, err := c.Complete(ctx, o.System, text)
	if err != nil {
		return "", "error: " + err.Error(), 0
	}
	out := res.Text
	var notes []string
	if o.LimitKey != "" {
		if iss := check.Length(o.Limits, o.LimitKey, out); iss != nil {
			if o.AutoTrim {
				out = check.Truncate(out, o.Limits[o.LimitKey])
				notes = append(notes, "trimmed ("+iss.Detail+")")
			} else {
				notes = append(notes, iss.Detail)
			}
		}
	}
	for _, b := range check.Banned(o.Banned, o.Column, out) {
		notes = append(notes, b.Detail)
	}
	note := ""
	for i, n := range notes {
		if i > 0 {
			note += "; "
		}
		note += n
	}
	return out, note, res.Tokens
}
