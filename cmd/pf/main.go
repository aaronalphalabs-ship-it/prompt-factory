// pf — Prompt Factory: version, test and batch-run prompts from your terminal.
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/aaronalphalabs-ship-it/prompt-factory/internal/batch"
	"github.com/aaronalphalabs-ship-it/prompt-factory/internal/check"
	"github.com/aaronalphalabs-ship-it/prompt-factory/internal/compare"
	"github.com/aaronalphalabs-ship-it/prompt-factory/internal/config"
	"github.com/aaronalphalabs-ship-it/prompt-factory/internal/license"
	"github.com/aaronalphalabs-ship-it/prompt-factory/internal/prompt"
	"github.com/aaronalphalabs-ship-it/prompt-factory/internal/provider"
)

var version = "0.1.0"

const usage = `pf — Prompt Factory %s
Version, test and batch-run prompts from your terminal.

Free:
  pf init                         create pf.json and an example prompt
  pf new <name>                   create prompts/<name>.md
  pf list                         list prompts
  pf run <name> [--var k=v]...    render + run one prompt (--provider, --dry)
  pf batch <name> --in a.csv      run a prompt for every CSV row (--out, --col, --limit amazon.title, --trim, -j 4)
  pf history <name>               versions of a prompt that have been run
  pf check --in out.csv --col title --limit amazon.title
                                  marketplace length + banned-word check
Pro (14-day free trial):
  pf compare <name> --providers a,b,c [--judge x] [--var k=v]...
                                  same prompt on several models, scored by a judge, Markdown report
  pf license activate <key>       activate a Prompt Factory Pro key
  pf license status

https://aaronalpha.top/pf
`

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	if len(os.Args) < 2 {
		fmt.Printf(usage, version)
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "init":
		err = cmdInit()
	case "new":
		err = cmdNew(args)
	case "list", "ls":
		err = cmdList()
	case "run":
		err = cmdRun(ctx, args)
	case "batch":
		err = cmdBatch(ctx, args)
	case "history":
		err = cmdHistory(args)
	case "check":
		err = cmdCheck(args)
	case "compare":
		err = cmdCompare(ctx, args)
	case "license":
		err = cmdLicense(ctx, args)
	case "version", "--version", "-v":
		fmt.Println("pf", version)
	case "help", "--help", "-h":
		fmt.Printf(usage, version)
	default:
		err = fmt.Errorf("unknown command %q (pf help)", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "pf:", err)
		os.Exit(1)
	}
}

// parse lets flags and the positional <name> appear in any order.
func parse(fs *flag.FlagSet, args []string) (string, error) {
	var pos []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return "", err
		}
		args = fs.Args()
		if len(args) > 0 {
			pos = append(pos, args[0])
			args = args[1:]
		}
	}
	if len(pos) > 0 {
		return pos[0], nil
	}
	return "", nil
}

const examplePrompt = `You are a senior Amazon copywriter.

Write ONE Amazon product title (max 200 characters) for:
Product: {{.product}}
Key features: {{.features}}
Target buyer: {{.audience | default "online shoppers"}}
Language: {{.lang | default "English"}}

Rules: brand first if one is given (never invent a brand), no ALL CAPS, no promotional claims like "best" or "#1".
Return only the title.
`

const exampleCSV = `product,features,audience,lang
Bamboo cutting board,"extra large, juice groove, knife-friendly",home cooks,English
Silicone baby bib,"waterproof, food catcher pocket, BPA free",new parents,English
LED desk lamp,"3 color modes, USB-C, touch dimmer",students,English
`

func cmdInit() error {
	if _, err := os.Stat(config.FileName); err == nil {
		return fmt.Errorf("%s already exists", config.FileName)
	}
	c := config.Default()
	c.Brand.BannedWords = []string{"best", "#1", "guaranteed", "100% cure"}
	if err := c.Save("."); err != nil {
		return err
	}
	_ = os.MkdirAll("prompts", 0o755)
	_ = os.WriteFile(filepath.Join("prompts", "amazon-title.md"), []byte(examplePrompt), 0o644)
	_ = os.WriteFile("products.csv", []byte(exampleCSV), 0o644)
	_ = os.WriteFile(".gitignore", []byte(".pf/runs.jsonl\n"), 0o644)
	fmt.Println(`Created pf.json, prompts/amazon-title.md and products.csv.

Next:
  export OPENAI_API_KEY=...        # or DEEPSEEK_API_KEY / XAI_API_KEY, then set default_provider
  pf run amazon-title --var product="Bamboo cutting board" --var features="juice groove"
  pf batch amazon-title --in products.csv --out titles.csv --col title --limit amazon.title --trim`)
	return nil
}

func cmdNew(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: pf new <name>")
	}
	c, err := config.Load()
	if err != nil {
		return err
	}
	_ = os.MkdirAll(c.PromptsPath(), 0o755)
	p := filepath.Join(c.PromptsPath(), strings.TrimSuffix(args[0], ".md")+".md")
	if _, err := os.Stat(p); err == nil {
		return fmt.Errorf("%s already exists", p)
	}
	if err := os.WriteFile(p, []byte("Write about {{.topic}}.\n"), 0o644); err != nil {
		return err
	}
	fmt.Println("created", p)
	return nil
}

func cmdList() error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	names, err := prompt.List(c.PromptsPath())
	if err != nil {
		return err
	}
	for _, n := range names {
		v, _ := prompt.History(c.StatePath(), n)
		fmt.Printf("%-28s %d version(s) run\n", n, len(v))
	}
	return nil
}

func load(name string) (*config.Config, *prompt.Prompt, error) {
	if name == "" {
		return nil, nil, fmt.Errorf("missing prompt name")
	}
	c, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	p, err := prompt.Load(c.PromptsPath(), name)
	return c, p, err
}

func client(c *config.Config, name string) (*provider.Client, error) {
	p, err := c.Provider(name)
	if err != nil {
		return nil, err
	}
	return provider.New(p, c.Temperature), nil
}

func logRun(c *config.Config, rec map[string]any) {
	_ = os.MkdirAll(c.StatePath(), 0o755)
	f, err := os.OpenFile(filepath.Join(c.StatePath(), "runs.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	rec["at"] = time.Now().UTC().Format(time.RFC3339)
	b, _ := json.Marshal(rec)
	f.Write(append(b, '\n'))
}

func cmdRun(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	var vars multi
	fs.Var(&vars, "var", "template variable key=value (repeatable)")
	prov := fs.String("provider", "", "provider name from pf.json")
	dry := fs.Bool("dry", false, "print the rendered prompt only")
	name, err := parse(fs, args)
	if err != nil {
		return err
	}
	c, p, err := load(name)
	if err != nil {
		return err
	}
	m, err := prompt.ParseVars(vars)
	if err != nil {
		return err
	}
	text, err := p.Render(m, true)
	if err != nil {
		return err
	}
	if *dry {
		fmt.Println(text)
		return nil
	}
	cl, err := client(c, *prov)
	if err != nil {
		return err
	}
	_ = p.Snapshot(c.StatePath())
	res, err := cl.Complete(ctx, voice(c), text)
	if err != nil {
		return err
	}
	fmt.Println(res.Text)
	fmt.Fprintf(os.Stderr, "\n— %s/%s · %s@%s · %d tokens · %.1fs\n", res.Provider, res.Model, p.Name, p.Hash, res.Tokens, res.Latency.Seconds())
	logRun(c, map[string]any{"cmd": "run", "prompt": p.Name, "version": p.Hash, "provider": res.Provider, "model": res.Model, "vars": m, "output": res.Text, "tokens": res.Tokens})
	return nil
}

func voice(c *config.Config) string {
	if c.Brand.Voice == "" {
		return ""
	}
	return "Brand voice: " + c.Brand.Voice
}

func cmdBatch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("batch", flag.ContinueOnError)
	in := fs.String("in", "", "input CSV (header row = template variables)")
	out := fs.String("out", "", "output CSV (default <in>.out.csv)")
	col := fs.String("col", "output", "name of the output column")
	limit := fs.String("limit", "", "length rule from pf.json limits, e.g. amazon.title")
	trim := fs.Bool("trim", false, "auto-trim outputs that exceed --limit")
	j := fs.Int("j", 4, "concurrent requests")
	prov := fs.String("provider", "", "provider name from pf.json")
	name, err := parse(fs, args)
	if err != nil {
		return err
	}
	if *in == "" {
		return fmt.Errorf("--in is required")
	}
	if *out == "" {
		*out = strings.TrimSuffix(*in, ".csv") + ".out.csv"
	}
	c, p, err := load(name)
	if err != nil {
		return err
	}
	cl, err := client(c, *prov)
	if err != nil {
		return err
	}
	_ = p.Snapshot(c.StatePath())
	start := time.Now()
	st, err := batch.Run(ctx, p, cl, batch.Options{
		In: *in, Out: *out, Column: *col, Concurrency: *j, System: voice(c),
		LimitKey: *limit, AutoTrim: *trim, Limits: c.Limits, Banned: c.Brand.BannedWords,
		Progress: func(d, t int) { fmt.Fprintf(os.Stderr, "\r%d/%d", d, t) },
	})
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return err
	}
	fmt.Printf("%d rows → %s · ok %d · failed %d · flagged %d · %d tokens · %s · %s@%s\n",
		st.Rows, *out, st.OK, st.Failed, st.Issues, st.Tokens, time.Since(start).Round(time.Second), p.Name, p.Hash)
	logRun(c, map[string]any{"cmd": "batch", "prompt": p.Name, "version": p.Hash, "provider": cl.P.Name, "model": cl.P.Model, "in": *in, "out": *out, "rows": st.Rows, "failed": st.Failed, "tokens": st.Tokens})
	return nil
}

func cmdHistory(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: pf history <name>")
	}
	c, err := config.Load()
	if err != nil {
		return err
	}
	vs, err := prompt.History(c.StatePath(), args[0])
	if err != nil {
		return err
	}
	if len(vs) == 0 {
		fmt.Println("no versions yet — versions are saved when a prompt is run")
		return nil
	}
	for _, v := range vs {
		fmt.Printf("%s  %s  %s\n", v.Hash, v.When, v.File)
	}
	return nil
}

func cmdCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	in := fs.String("in", "", "CSV to check")
	col := fs.String("col", "output", "column to check")
	limit := fs.String("limit", "", "length rule, e.g. amazon.title")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	if *in == "" {
		return fmt.Errorf("--in is required")
	}
	c, err := config.Load()
	if err != nil {
		return err
	}
	if *limit != "" {
		if _, ok := c.Limits[*limit]; !ok {
			return fmt.Errorf("no limit %q in pf.json", *limit)
		}
	}
	f, err := os.Open(*in)
	if err != nil {
		return err
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("empty CSV")
	}
	idx := -1
	for i, h := range rows[0] {
		if h == *col {
			idx = i
		}
	}
	if idx < 0 {
		return fmt.Errorf("column %q not found", *col)
	}
	bad := 0
	for r, row := range rows[1:] {
		if idx >= len(row) {
			continue
		}
		var issues []check.Issue
		if *limit != "" {
			if iss := check.Length(c.Limits, *limit, row[idx]); iss != nil {
				issues = append(issues, *iss)
			}
		}
		issues = append(issues, check.Banned(c.Brand.BannedWords, *col, row[idx])...)
		for _, iss := range issues {
			bad++
			fmt.Printf("row %d  %-12s %s\n", r+2, iss.Rule, iss.Detail)
		}
	}
	fmt.Printf("%d row(s) checked, %d issue(s)\n", len(rows)-1, bad)
	if bad > 0 {
		os.Exit(2)
	}
	return nil
}

func cmdCompare(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	var vars multi
	fs.Var(&vars, "var", "template variable key=value (repeatable)")
	provs := fs.String("providers", "", "comma-separated providers to compare")
	judgeName := fs.String("judge", "", "provider used as judge (default: default_provider)")
	rubric := fs.String("rubric", "", "scoring rubric (optional)")
	limit := fs.String("limit", "", "length rule to flag, e.g. amazon.title")
	out := fs.String("out", "", "write the Markdown report here (default: stdout)")
	name, err := parse(fs, args)
	if err != nil {
		return err
	}
	if err := license.Require("pf compare"); err != nil {
		return err
	}
	c, p, err := load(name)
	if err != nil {
		return err
	}
	if *provs == "" {
		return fmt.Errorf("--providers is required, e.g. --providers openai,deepseek,xai")
	}
	m, err := prompt.ParseVars(vars)
	if err != nil {
		return err
	}
	text, err := p.Render(m, true)
	if err != nil {
		return err
	}
	var cs []*provider.Client
	for _, n := range strings.Split(*provs, ",") {
		cl, err := client(c, strings.TrimSpace(n))
		if err != nil {
			return err
		}
		cs = append(cs, cl)
	}
	judge, err := client(c, *judgeName)
	if err != nil {
		return err
	}
	_ = p.Snapshot(c.StatePath())
	entries := compare.Run(ctx, text, cs, judge, compare.Options{
		Rubric: *rubric, Voice: c.Brand.Voice, Banned: c.Brand.BannedWords, Limits: c.Limits, LimitKey: *limit,
	})
	rep := compare.Report(p.Name, p.Hash, judge.P.Name+"/"+judge.P.Model, entries)
	if *out != "" {
		if err := os.WriteFile(*out, []byte(rep), 0o644); err != nil {
			return err
		}
		fmt.Println("report →", *out)
	} else {
		fmt.Println(rep)
	}
	logRun(c, map[string]any{"cmd": "compare", "prompt": p.Name, "version": p.Hash, "providers": *provs})
	return nil
}

func cmdLicense(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "status" {
		fmt.Println(license.Load().Status())
		return nil
	}
	if args[0] == "activate" && len(args) == 2 {
		s, err := license.Activate(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Println("✦ Activated.", s.Status())
		return nil
	}
	return fmt.Errorf("usage: pf license [status | activate <key>]")
}
