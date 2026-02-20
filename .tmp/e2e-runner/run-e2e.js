#!/usr/bin/env node
const fs = require('fs');
const path = require('path');
const yaml = require('js-yaml');
const { chromium } = require('playwright');

function parseArgs(argv) {
  const out = {
    url: 'http://127.0.0.1:8080',
    shell: 'bash',
    plans: [
      path.resolve('tests/e2e/example-test-plan.yaml'),
      path.resolve('tests/e2e/suggestions-tests.yaml'),
    ],
    outDir: path.resolve('.tmp/e2e-runs'),
  };
  for (let i = 2; i < argv.length; i++) {
    const a = argv[i];
    if (a === '--url') out.url = argv[++i];
    else if (a === '--shell') out.shell = argv[++i];
    else if (a === '--plans') out.plans = argv[++i].split(',').map((p) => path.resolve(p.trim())).filter(Boolean);
    else if (a === '--out') out.outDir = path.resolve(argv[++i]);
  }
  return out;
}

function durationMs(value) {
  if (typeof value === 'number') return value;
  if (!value) return 0;
  const s = String(value).trim();
  const m = s.match(/^(\d+)(ms|s)$/);
  if (!m) return 0;
  const n = Number(m[1]);
  return m[2] === 's' ? n * 1000 : n;
}

function keyName(raw) {
  const k = String(raw).trim();
  const map = {
    Up: 'ArrowUp',
    Down: 'ArrowDown',
    Left: 'ArrowLeft',
    Right: 'ArrowRight',
    Esc: 'Escape',
    Return: 'Enter',
    Del: 'Delete',
    Space: 'Space',
  };
  return map[k] || k;
}

function pressSpecToPlaywright(spec) {
  const parts = String(spec).split('+').map((p) => p.trim()).filter(Boolean);
  if (parts.length === 1) return keyName(parts[0]);
  const mods = [];
  let key = parts[parts.length - 1];
  for (const p of parts.slice(0, -1)) {
    const v = p.toLowerCase();
    if (v === 'ctrl' || v === 'control') mods.push('Control');
    else if (v === 'alt' || v === 'option') mods.push('Alt');
    else if (v === 'shift') mods.push('Shift');
    else if (v === 'cmd' || v === 'command' || v === 'meta') mods.push('Meta');
  }
  key = keyName(key);
  return `${mods.join('+')}+${key}`;
}

function appliesToShell(test, shell) {
  if (test.shell && test.shell !== shell) return false;
  if (Array.isArray(test.shells) && !test.shells.includes(shell)) return false;
  return true;
}

async function terminalText(page) {
  const txt = await page.evaluate(() => {
    const rows = Array.from(document.querySelectorAll('.xterm-rows > div'));
    if (rows.length > 0) {
      return rows.map((r) => (r.textContent || '').replace(/\u00a0/g, ' ')).join('\n');
    }
    return (document.body && document.body.innerText ? document.body.innerText : '').replace(/\u00a0/g, ' ');
  });
  return txt;
}

async function focusTerminal(page) {
  try {
    await page.waitForSelector('textarea.xterm-helper-textarea', { timeout: 5000 });
    await page.click('textarea.xterm-helper-textarea');
  } catch {
    await page.click('body');
  }
}

async function runCommand(page, cmd) {
  await focusTerminal(page);
  await page.keyboard.press('Control+u');
  await page.keyboard.type(String(cmd), { delay: 4 });
  await page.keyboard.press('Enter');
  await page.waitForTimeout(220);
}

async function runStep(page, step) {
  if (step.type !== undefined) {
    await focusTerminal(page);
    await page.keyboard.type(String(step.type), { delay: 4 });
    return;
  }
  if (step.press !== undefined) {
    await focusTerminal(page);
    await page.keyboard.press(pressSpecToPlaywright(step.press));
    return;
  }
  if (step.wait !== undefined) {
    await page.waitForTimeout(durationMs(step.wait));
  }
}

async function assertExpect(page, expectItem) {
  const [kind, value] = Object.entries(expectItem)[0] || [];
  if (!kind) return { ok: true };
  const text = await terminalText(page);

  if (kind === 'screen_contains') {
    return { ok: text.includes(String(value)), detail: `expected screen to contain: ${value}` };
  }
  if (kind === 'screen_not_contains') {
    return { ok: !text.includes(String(value)), detail: `expected screen not to contain: ${value}` };
  }
  if (kind === 'screen_matches') {
    const re = new RegExp(String(value), 'm');
    return { ok: re.test(text), detail: `expected screen to match: ${value}` };
  }
  if (kind === 'element_visible') {
    const v = String(value);
    const ok = await page.evaluate((val) => {
      const selectors = [`#${val}`, `.${val}`, `[data-testid="${val}"]`, `[aria-label*="${val}"]`];
      for (const sel of selectors) {
        const el = document.querySelector(sel);
        if (!el) continue;
        const style = window.getComputedStyle(el);
        if (style && style.display !== 'none' && style.visibility !== 'hidden') return true;
      }
      return false;
    }, v);
    return { ok, detail: `expected visible element: ${v}` };
  }

  return { ok: true, detail: `ignored expectation kind: ${kind}` };
}

function planTests(planDoc) {
  return Array.isArray(planDoc.tests) ? planDoc.tests : [];
}

async function runOneTest(browser, cfg, test, idx, total, artifactDir) {
  const page = await browser.newPage({ viewport: { width: 1400, height: 900 } });
  const name = test.name || `unnamed-${idx}`;
  const id = `${String(idx + 1).padStart(3, '0')}-${name.replace(/[^a-zA-Z0-9._-]+/g, '_').slice(0, 80)}`;
  const shot = path.join(artifactDir, `${id}.png`);

  try {
    await page.goto(cfg.url, { waitUntil: 'domcontentloaded', timeout: 30000 });
    await page.waitForTimeout(350);

    if (Array.isArray(test.setup)) {
      for (const cmd of test.setup) {
        await runCommand(page, cmd);
      }
    }

    if (Array.isArray(test.steps)) {
      for (const step of test.steps) {
        await runStep(page, step);
      }
    }

    const fails = [];
    if (Array.isArray(test.expect)) {
      for (const ex of test.expect) {
        const r = await assertExpect(page, ex);
        if (!r.ok) fails.push(r.detail);
      }
    }

    if (fails.length > 0) {
      await page.screenshot({ path: shot, fullPage: true });
      return { ok: false, name, id, reason: fails.join(' | '), screenshot: shot };
    }

    return { ok: true, name, id };
  } catch (err) {
    try { await page.screenshot({ path: shot, fullPage: true }); } catch {}
    return { ok: false, name, id, reason: String(err && err.message ? err.message : err), screenshot: shot };
  } finally {
    await page.close();
  }
}

async function main() {
  const cfg = parseArgs(process.argv);
  const artifactDir = path.join(cfg.outDir, `artifacts-${cfg.shell}`);
  fs.mkdirSync(cfg.outDir, { recursive: true });
  fs.mkdirSync(artifactDir, { recursive: true });

  const selected = [];
  for (const planPath of cfg.plans) {
    const doc = yaml.load(fs.readFileSync(planPath, 'utf8'));
    for (const t of planTests(doc)) {
      if (appliesToShell(t, cfg.shell)) {
        selected.push({ ...t, __plan: path.basename(planPath) });
      }
    }
  }

  const browser = await chromium.launch({ headless: true });
  const results = [];
  const started = Date.now();

  for (let i = 0; i < selected.length; i++) {
    const t = selected[i];
    process.stdout.write(`[${cfg.shell}] ${i + 1}/${selected.length} ${t.name}\n`);
    const r = await runOneTest(browser, cfg, t, i, selected.length, artifactDir);
    r.plan = t.__plan;
    results.push(r);
    process.stdout.write(`  -> ${r.ok ? 'PASS' : 'FAIL'}${r.ok ? '' : `: ${r.reason}`}\n`);
  }

  await browser.close();

  const passed = results.filter((r) => r.ok).length;
  const failed = results.length - passed;
  const summary = {
    shell: cfg.shell,
    url: cfg.url,
    plans: cfg.plans,
    total: results.length,
    passed,
    failed,
    duration_ms: Date.now() - started,
    failures: results.filter((r) => !r.ok),
  };

  const outJson = path.join(cfg.outDir, `results-${cfg.shell}.json`);
  fs.writeFileSync(outJson, JSON.stringify({ summary, results }, null, 2));

  console.log(`SUMMARY shell=${cfg.shell} total=${summary.total} passed=${summary.passed} failed=${summary.failed} duration_ms=${summary.duration_ms}`);
  console.log(`RESULTS_JSON ${outJson}`);

  process.exit(failed > 0 ? 1 : 0);
}

main().catch((err) => {
  console.error(err);
  process.exit(2);
});
