# Prompt Factory (`pf`)

**Version, test and batch-run prompts from your terminal.** Built for e-commerce teams who write listings at scale.

```bash
pf init
pf batch amazon-title --in products.csv --out titles.csv --col title --limit amazon.title --trim
# 3 rows → titles.csv · ok 3 · failed 0 · flagged 0 · 4473 tokens · 31s · amazon-title@040dded9
```

- **One CSV in, every listing out.** Each column of your CSV becomes a template variable. Concurrency, retries and a notes column for anything that failed or broke a rule.
- **Every result traces to a prompt version.** Each run snapshots the exact prompt by content hash (`amazon-title@040dded9`), so you always know which wording produced which copy.
- **Marketplace rules built in.** Length limits for Amazon, eBay, Etsy and Shopify SEO fields, plus your brand's banned-word list. `pf check` exits non-zero, so it works in CI.
- **Any model.** Any OpenAI-compatible endpoint: OpenAI, DeepSeek, xAI Grok, OpenRouter, Ollama (local), and more. Switch with `--provider`.
- **Free and open source (MIT).** No account, no telemetry. Your keys and data stay on your machine.

## Install

```bash
go install github.com/aaronalphalabs-ship-it/prompt-factory/cmd/pf@latest
```

Or download a binary from [Releases](../../releases) (macOS, Linux, Windows).

## Quick start

```bash
mkdir listings && cd listings
pf init                                  # pf.json, prompts/amazon-title.md, products.csv
export OPENAI_API_KEY=sk-...             # or DEEPSEEK_API_KEY / XAI_API_KEY + set default_provider

pf run amazon-title --var product="Bamboo cutting board" --var features="juice groove"
pf batch amazon-title --in products.csv --out titles.csv --col title --limit amazon.title --trim
pf check --in titles.csv --col title --limit ebay.title
pf history amazon-title
```

## Prompts

Prompts are Markdown files in `prompts/` using Go template syntax:

```text
Write ONE Amazon product title for {{.product}}.
Key features: {{.features}}
Target buyer: {{.audience | default "online shoppers"}}
```

Keep `prompts/` in git and your whole team shares one library.

## Commands

| Command | What it does |
|---|---|
| `pf init` | Create `pf.json`, an example prompt and an example CSV |
| `pf new <name>` | Create a new prompt |
| `pf list` | List prompts and how many versions have been run |
| `pf run <name> --var k=v` | Render and run one prompt (`--dry` to just render, `--provider` to pick a model) |
| `pf batch <name> --in a.csv` | Run for every CSV row (`--out`, `--col`, `--limit`, `--trim`, `-j`) |
| `pf history <name>` | Every version of a prompt that has been run |
| `pf check --in a.csv --col title --limit amazon.title` | Length and banned-word check; exit code 2 on issues |
| `pf compare` ✦ Pro | Same prompt on several models, judged and ranked |
| `pf license activate <key>` | Activate Pro |

## Configuration (`pf.json`)

```json
{
  "default_provider": "deepseek",
  "providers": [
    {"name": "deepseek", "base_url": "https://api.deepseek.com/v1", "api_key_env": "DEEPSEEK_API_KEY", "model": "deepseek-chat"}
  ],
  "limits": {"amazon.title": 200, "ebay.title": 80, "etsy.title": 140},
  "brand": {"voice": "warm, practical, no hype", "banned_words": ["best", "#1", "guaranteed"]}
}
```

The default limits are a starting point. Marketplaces change rules by category and region, so check your category's style guide and override them.

## ✦ Prompt Factory Pro

For teams that need to pick the right model and keep every listing on-brand.

```bash
pf compare amazon-title --providers openai,deepseek,xai --judge openai --limit amazon.title \
  --var product="LED desk lamp" --var features="3 color modes, USB-C" --out report.md
```

`pf compare` runs one prompt on several models at once. A judge model scores each answer against your rubric and brand voice. Answers that break a length or banned-word rule lose points, and you get a ranked Markdown report: [example](examples/compare-report.md).

- 14-day free trial starts the first time you run a Pro command. No sign-up needed.
- Coming next in Pro: brand voice packs, shared team prompt libraries, and batch A/B across models.
- **[Get Pro → aaronalpha.top/pf](https://aaronalpha.top/pf)**

The free CLI stays complete and MIT-licensed. Pro only adds team and model-selection features on top.

## License

Core CLI: MIT. Pro features need a license key to use after the trial.

Made by [AaronAlpha Labs](https://aaronalpha.top).
