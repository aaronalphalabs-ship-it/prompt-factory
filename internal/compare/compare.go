// Package compare (Pro) runs the same prompt on several models, has a judge
// model score each answer against a rubric, and writes a Markdown report.
package compare

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/aaronalphalabs-ship-it/prompt-factory/internal/check"
	"github.com/aaronalphalabs-ship-it/prompt-factory/internal/provider"
)

type Entry struct {
	Provider string
	Model    string
	Output   string
	Judge    float64 // raw judge score
	Score    float64 // judge score minus 1.5 per rule violation
	Reason   string
	Issues   []string
	Tokens   int
	LatencyS float64
	Err      string
}

type Options struct {
	Rubric   string
	Voice    string
	Banned   []string
	Limits   map[string]int
	LimitKey string
}

const defaultRubric = "Accuracy to the product facts, persuasiveness for shoppers, clarity, and fit with the brand voice."

func Run(ctx context.Context, rendered string, contenders []*provider.Client, judge *provider.Client, o Options) []Entry {
	entries := make([]Entry, len(contenders))
	var wg sync.WaitGroup
	for i, c := range contenders {
		wg.Add(1)
		go func(i int, c *provider.Client) {
			defer wg.Done()
			e := Entry{Provider: c.P.Name, Model: c.P.Model}
			res, err := c.Complete(ctx, sys(o.Voice), rendered)
			if err != nil {
				e.Err = err.Error()
				entries[i] = e
				return
			}
			e.Output, e.Tokens, e.LatencyS = res.Text, res.Tokens, res.Latency.Seconds()
			if o.LimitKey != "" {
				if iss := check.Length(o.Limits, o.LimitKey, e.Output); iss != nil {
					e.Issues = append(e.Issues, o.LimitKey+": "+iss.Detail)
				}
			}
			for _, b := range check.Banned(o.Banned, "output", e.Output) {
				e.Issues = append(e.Issues, b.Detail)
			}
			e.Judge, e.Reason = score(ctx, judge, rendered, e.Output, o)
			e.Score = e.Judge - 1.5*float64(len(e.Issues))
			if e.Score < 0 {
				e.Score = 0
			}
			entries[i] = e
		}(i, c)
	}
	wg.Wait()
	sort.SliceStable(entries, func(a, b int) bool { return entries[a].Score > entries[b].Score })
	return entries
}

func sys(voice string) string {
	if voice == "" {
		return ""
	}
	return "Brand voice: " + voice
}

var numRe = regexp.MustCompile(`\d+(\.\d+)?`)

func score(ctx context.Context, judge *provider.Client, task, answer string, o Options) (float64, string) {
	rubric := o.Rubric
	if rubric == "" {
		rubric = defaultRubric
	}
	if o.Voice != "" {
		rubric += " Brand voice: " + o.Voice + "."
	}
	q := fmt.Sprintf(`You are a strict e-commerce copy reviewer. Penalize invented facts or brand names, placeholders like [Brand], and padding. Score the ANSWER to the TASK from 0 to 10 using this rubric: %s
Reply with JSON only: {"score": <number>, "reason": "<one sentence>"}

TASK:
%s

ANSWER:
%s`, rubric, task, answer)
	res, err := judge.Complete(ctx, "", q)
	if err != nil {
		return 0, "judge error: " + err.Error()
	}
	txt := res.Text
	if i := strings.Index(txt, "{"); i >= 0 {
		if j := strings.LastIndex(txt, "}"); j > i {
			var v struct {
				Score  float64 `json:"score"`
				Reason string  `json:"reason"`
			}
			if json.Unmarshal([]byte(txt[i:j+1]), &v) == nil {
				return v.Score, v.Reason
			}
		}
	}
	if m := numRe.FindString(txt); m != "" {
		var f float64
		fmt.Sscanf(m, "%g", &f)
		return f, strings.TrimSpace(txt)
	}
	return 0, "unparseable judge reply"
}

func Report(name, hash, judge string, entries []Entry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Model comparison — `%s` @ %s\n\nJudge: %s · Final = judge score − 1.5 per rule issue (length, banned words)\n\n", name, hash, judge)
	b.WriteString("| Rank | Provider / model | Final | Judge | Rule issues | Tokens | Latency |\n|---|---|---|---|---|---|---|\n")
	for i, e := range entries {
		iss := "—"
		if e.Err != "" {
			iss = "error"
		} else if len(e.Issues) > 0 {
			iss = strings.Join(e.Issues, "; ")
		}
		fmt.Fprintf(&b, "| %d | %s / %s | %.1f | %.1f | %s | %d | %.1fs |\n", i+1, e.Provider, e.Model, e.Score, e.Judge, iss, e.Tokens, e.LatencyS)
	}
	for i, e := range entries {
		fmt.Fprintf(&b, "\n## %d. %s / %s — %.1f\n\n", i+1, e.Provider, e.Model, e.Score)
		if e.Err != "" {
			fmt.Fprintf(&b, "Error: %s\n", e.Err)
			continue
		}
		fmt.Fprintf(&b, "> %s\n\n```\n%s\n```\n", e.Reason, e.Output)
	}
	return b.String()
}
