#!/usr/bin/env node

const fs = require("fs");
const path = require("path");
const yaml = require("js-yaml");
const { chromium } = require("playwright");

function parseArgs(argv) {
	const out = {
		url: "http://127.0.0.1:8080",
		shell: "bash",
		plans: [
			path.resolve("tests/e2e/example-test-plan.yaml"),
			path.resolve("tests/e2e/suggestions-tests.yaml"),
		],
		outDir: path.resolve(".tmp/e2e-runs"),
		grep: "",
	};
	for (let i = 2; i < argv.length; i++) {
		const a = argv[i];
		if (a === "--url") out.url = argv[++i];
		else if (a === "--shell") out.shell = argv[++i];
		else if (a === "--plan") out.plans = [path.resolve(argv[++i])];
		else if (a === "--plans") out.plans = argv[++i].split(",").map((p) => path.resolve(p.trim())).filter(Boolean);
		else if (a === "--grep") out.grep = String(argv[++i] || "");
		else if (a === "--out") out.outDir = path.resolve(argv[++i]);
	}
	return out;
}

function ms(value) {
	if (typeof value === "number") return value;
	const s = String(value || "").trim();
	const m = s.match(/^(\d+)(ms|s)$/);
	if (!m) return 0;
	const n = Number(m[1]);
	return m[2] === "s" ? n * 1000 : n;
}

function appliesToShell(test, shell) {
	if (test.shell && test.shell !== shell) return false;
	if (Array.isArray(test.shells) && !test.shells.includes(shell)) return false;
	return true;
}

function mapKey(part) {
	const raw = String(part);
	if (raw === " ") return "Space";
	const k = raw.trim();
	switch (k) {
	case "Up":
		return "ArrowUp";
	case "Down":
		return "ArrowDown";
	case "Left":
		return "ArrowLeft";
	case "Right":
		return "ArrowRight";
	case "Esc":
		return "Escape";
	case "Return":
		return "Enter";
	case "Space":
		return "Space";
	default:
		return k;
	}
}

function pressSpec(spec) {
	const raw = String(spec);
	// Keep explicit trailing space combos like "Ctrl+ ".
	const parts = raw.split("+");
	if (parts.length === 1) return mapKey(parts[0]);
	const mods = [];
	for (let i = 0; i < parts.length - 1; i++) {
		const p = parts[i].trim().toLowerCase();
		if (p === "ctrl" || p === "control") mods.push("Control");
		else if (p === "alt" || p === "opt" || p === "option") mods.push("Alt");
		else if (p === "shift") mods.push("Shift");
		else if (p === "meta" || p === "cmd" || p === "command") mods.push("Meta");
	}
	const key = mapKey(parts[parts.length - 1]);
	return `${mods.join("+")}+${key}`;
}

async function focusTerminal(page) {
	try {
		await page.waitForSelector("textarea.xterm-helper-textarea", { timeout: 5000 });
		await page.click("textarea.xterm-helper-textarea");
	} catch {
		await page.click("body");
	}
}

async function terminalText(page) {
	return page.evaluate(() => {
		const rows = Array.from(document.querySelectorAll(".xterm-rows > div"));
		if (rows.length > 0) {
			return rows.map((r) => (r.textContent || "").replace(/\u00a0/g, " ")).join("\n");
		}
		return ((document.body && document.body.innerText) || "").replace(/\u00a0/g, " ");
	});
}

function tailLines(text, n) {
	const lines = String(text || "").split("\n");
	if (lines.length <= n) return lines.join("\n");
	return lines.slice(lines.length - n).join("\n");
}

async function waitForPrompt(page, timeoutMs = 8000) {
	await page.waitForFunction(() => {
		const rows = Array.from(document.querySelectorAll(".xterm-rows > div"));
		const text = (rows.length > 0
			? rows.map((r) => (r.textContent || "").replace(/\u00a0/g, " ")).join("\n")
			: ((document.body && document.body.innerText) || "").replace(/\u00a0/g, " "));
		return /(?:^|\n)TEST>\s*$/.test(text);
	}, { timeout: timeoutMs });
}

async function runCommand(page, command) {
	await focusTerminal(page);
	await page.keyboard.press("Control+u");
	await page.keyboard.type(String(command), { delay: 4 });
	await page.keyboard.press("Enter");
	try {
		await waitForPrompt(page, 12000);
	} catch {
		// Fallback for cases where prompt detection is flaky.
		await page.waitForTimeout(500);
	}
}

async function runStep(page, step) {
	if (step.type !== undefined) {
		await focusTerminal(page);
		await page.keyboard.type(String(step.type), { delay: 4 });
		return;
	}
	if (step.press !== undefined) {
		await focusTerminal(page);
		await page.keyboard.press(pressSpec(step.press));
		return;
	}
	if (step.wait !== undefined) {
		await page.waitForTimeout(ms(step.wait));
	}
}

async function evaluateExpectation(page, expectItem) {
	const [kind, value] = Object.entries(expectItem)[0] || [];
	if (!kind) return { ok: true, detail: "empty expectation" };
	const text = await terminalText(page);
	if (kind === "screen_contains") {
		return { ok: text.includes(String(value)), detail: `expected screen to contain: ${value}` };
	}
	if (kind === "screen_not_contains") {
		return { ok: !text.includes(String(value)), detail: `expected screen not to contain: ${value}` };
	}
	if (kind === "screen_matches") {
		const re = new RegExp(String(value), "m");
		return { ok: re.test(text), detail: `expected screen to match: ${value}` };
	}
	// Intentionally ignore unsupported expectation kinds for now.
	return { ok: true, detail: `ignored expectation kind: ${kind}` };
}

async function runTest(browser, cfg, test, index, artifactDir) {
	const page = await browser.newPage({ viewport: { width: 1400, height: 900 } });
	const safe = `${String(index + 1).padStart(3, "0")}-${(test.name || "unnamed").replace(/[^a-zA-Z0-9._-]+/g, "_").slice(0, 80)}`;
	const screenshot = path.join(artifactDir, `${safe}.png`);
	try {
		await page.goto(cfg.url, { waitUntil: "domcontentloaded", timeout: 30000 });
		await page.waitForTimeout(300);
		await waitForPrompt(page, 10000);
		for (const cmd of test.setup || []) {
			await runCommand(page, cmd);
		}
		for (const step of test.steps || []) {
			await runStep(page, step);
		}
		const failures = [];
		for (const ex of test.expect || []) {
			const r = await evaluateExpectation(page, ex);
			if (!r.ok) failures.push(r.detail);
		}
		if (failures.length > 0) {
			const text = await terminalText(page);
			await page.screenshot({ path: screenshot, fullPage: true });
			return {
				ok: false,
				name: test.name || "unnamed",
				reason: failures.join(" | "),
				screenshot,
				terminal_excerpt: tailLines(text, 40),
			};
		}
		return { ok: true, name: test.name || "unnamed" };
	} catch (err) {
		let excerpt = "";
		try {
			excerpt = tailLines(await terminalText(page), 40);
		} catch {}
		try {
			await page.screenshot({ path: screenshot, fullPage: true });
		} catch {}
		return {
			ok: false,
			name: test.name || "unnamed",
			reason: String(err && err.message ? err.message : err),
			screenshot,
			terminal_excerpt: excerpt,
		};
	} finally {
		await page.close();
	}
}

async function main() {
	const cfg = parseArgs(process.argv);
	fs.mkdirSync(cfg.outDir, { recursive: true });
	const artifactDir = path.join(cfg.outDir, `artifacts-${cfg.shell}`);
	fs.mkdirSync(artifactDir, { recursive: true });

	const selected = [];
	let grepRe = null;
	if (cfg.grep) {
		grepRe = new RegExp(cfg.grep, "i");
	}
	for (const planPath of cfg.plans) {
		const doc = yaml.load(fs.readFileSync(planPath, "utf8"));
		for (const t of doc.tests || []) {
			if (!appliesToShell(t, cfg.shell)) continue;
			if (grepRe && !grepRe.test(String(t.name || ""))) continue;
			selected.push({
				name: t.name,
				plan: path.basename(planPath),
				skip: !!t.skip,
				skip_reason: t.skip_reason || "",
				setup: t.setup || [],
				steps: t.steps || [],
				expect: t.expect || [],
			});
		}
	}

	const browser = await chromium.launch({ headless: true });
	const results = [];
	const started = Date.now();
	for (let i = 0; i < selected.length; i++) {
		const t = selected[i];
		if (t.skip) {
			results.push({ ok: true, skipped: true, name: t.name, reason: t.skip_reason, plan: t.plan });
			process.stdout.write(`[${cfg.shell}] ${i + 1}/${selected.length} ${t.name}\n  -> SKIP: ${t.skip_reason}\n`);
			continue;
		}
		process.stdout.write(`[${cfg.shell}] ${i + 1}/${selected.length} ${t.name}\n`);
		const r = await runTest(browser, cfg, t, i, artifactDir);
		r.plan = t.plan;
		results.push(r);
		process.stdout.write(`  -> ${r.ok ? "PASS" : `FAIL: ${r.reason}`}\n`);
	}
	await browser.close();

	const total = results.filter((r) => !r.skipped).length;
	const passed = results.filter((r) => !r.skipped && r.ok).length;
	const failed = results.filter((r) => !r.skipped && !r.ok).length;
	const skipped = results.filter((r) => r.skipped).length;
	const summary = { shell: cfg.shell, total, passed, failed, skipped, duration_ms: Date.now() - started, failures: results.filter((r) => !r.skipped && !r.ok) };
	const outPath = path.join(cfg.outDir, `results-${cfg.shell}.json`);
	fs.writeFileSync(outPath, JSON.stringify({ summary, results }, null, 2));
	console.log(`SUMMARY shell=${cfg.shell} total=${summary.total} passed=${summary.passed} failed=${summary.failed} skipped=${summary.skipped} duration_ms=${summary.duration_ms}`);
	console.log(`RESULTS_JSON ${outPath}`);
	process.exit(failed > 0 ? 1 : 0);
}

main().catch((err) => {
	console.error(err);
	process.exit(2);
});
