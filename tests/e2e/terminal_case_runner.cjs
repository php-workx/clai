const ms = (value) => {
	if (typeof value === "number") return value;
	const s = String(value || "").trim();
	const m = s.match(/^(\d+)(ms|s)$/);
	if (!m) return 0;
	const n = Number(m[1]);
	return m[2] === "s" ? n * 1000 : n;
};

const appliesToShell = (testCase, shell) => {
	if (testCase.shell && testCase.shell !== shell) return false;
	if (Array.isArray(testCase.shells) && !testCase.shells.includes(shell)) return false;
	return true;
};

const mapKey = (part) => {
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
};

const pressSpec = (spec) => {
	const raw = String(spec);
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
};

const focusTerminal = async (page) => {
	try {
		await page.waitForSelector("textarea.xterm-helper-textarea", { timeout: 5000 });
		await page.click("textarea.xterm-helper-textarea");
	} catch {
		await page.click("body");
	}
};

const terminalText = async (page) => page.evaluate(() => {
	const rows = Array.from(document.querySelectorAll(".xterm-rows > div"));
	if (rows.length > 0) {
		return rows.map((r) => (r.textContent || "").replace(/\u00a0/g, " ")).join("\n");
	}
	return ((document.body && document.body.innerText) || "").replace(/\u00a0/g, " ");
});

const waitForPrompt = async (page, timeoutMs = 8000) => {
	await page.waitForFunction(() => {
		const rows = Array.from(document.querySelectorAll(".xterm-rows > div"));
		const text = (rows.length > 0
			? rows.map((r) => (r.textContent || "").replace(/\u00a0/g, " ")).join("\n")
			: ((document.body && document.body.innerText) || "").replace(/\u00a0/g, " "));
		return /(?:^|\n)TEST>\s*$/.test(text);
	}, { timeout: timeoutMs });
};

const runCommand = async (page, command) => {
	await focusTerminal(page);
	await page.keyboard.press("Control+u");
	await page.keyboard.type(String(command), { delay: 4 });
	await page.keyboard.press("Enter");
	try {
		await waitForPrompt(page, 12_000);
	} catch {
		await page.waitForTimeout(500);
	}
};

const runStep = async (page, step) => {
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
};

const evaluateExpectations = async (page, expectations) => {
	const failures = [];
	const text = await terminalText(page);
	for (const ex of expectations || []) {
		const [kind, value] = Object.entries(ex)[0] || [];
		if (!kind) continue;
		const needle = String(value);
		if (kind === "screen_contains" && !text.includes(needle)) {
			failures.push(`expected screen to contain: ${needle}`);
		} else if (kind === "screen_not_contains" && text.includes(needle)) {
			failures.push(`expected screen not to contain: ${needle}`);
		} else if (kind === "screen_matches") {
			const re = new RegExp(needle, "m");
			if (!re.test(text)) {
				failures.push(`expected screen to match: ${needle}`);
			}
		}
	}
	return failures;
};

const createPlaywrightTests = ({ test, expect, shell, url, testCases, suiteLabel }) => {
	const selected = (testCases || []).filter((tc) => appliesToShell(tc, shell));
	for (const tc of selected) {
		const name = suiteLabel
			? `${tc.name} [${suiteLabel}]`
			: tc.name;
		test(name, async ({ page }) => {
			test.skip(!!tc.skip, tc.skip_reason || "skipped");

			await page.goto(url, { waitUntil: "domcontentloaded", timeout: 30_000 });
			await page.waitForTimeout(300);
			await waitForPrompt(page, 10_000);

			for (const cmd of tc.setup || []) {
				await runCommand(page, cmd);
			}
			for (const step of tc.steps || []) {
				await runStep(page, step);
			}

			const failures = await evaluateExpectations(page, tc.expect || []);
			expect(failures.join(" | ")).toBe("");
		});
	}
};

module.exports = {
	createPlaywrightTests,
	appliesToShell,
};
