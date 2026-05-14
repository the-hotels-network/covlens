# HTML report is always written

covlens writes `coverage_report.html` on every successful run, including when there are no measurable Go files to report on (no Go diff, or only deletions). The `--no-html` flag was removed; HTML generation is unconditional. Empty-state runs render a small "nothing to measure" page instead of skipping the file. This guarantees downstream CI tooling can hardcode the artifact path without special-casing empty PRs.

## Considered options

- **Keep `--no-html` + conditional HTML write** (previous behavior): rejected — breaks the implicit "covlens always writes its report" contract; downstream consumers (CI artifact pipelines, badge generators, Slack bots) have to special-case the missing file.
- **Always write HTML, keep `--no-html` as opt-out**: rejected — `--no-html` saves only kilobytes of disk and milliseconds of render time; the flag duplicated intent already covered by `--open`/`--no-open` and added a config knob with no real use case.
- **Always write HTML, drop `--no-html`** (chosen): single contract, simpler CLI, browser auto-open is still controllable via `--open`/`--no-open`. Browser is NOT auto-opened in the empty-state path even when `auto_open: true` — opening a "nothing to measure" tab is friction, not signal.
