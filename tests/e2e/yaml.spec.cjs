const fs = require("fs");
const path = require("path");
const yaml = require("js-yaml");
const { test, expect } = require("@playwright/test");
const { createPlaywrightTests } = require("./terminal_case_runner.cjs");

const e2eShell = process.env.E2E_SHELL || "bash";
const e2eURL = process.env.E2E_URL || "http://127.0.0.1:8080";
const plansEnv = process.env.E2E_PLANS || "tests/e2e/example-test-plan.yaml";
const planPaths = plansEnv.split(",").map((p) => p.trim()).filter(Boolean).map((p) => path.resolve(p));

const selectedTests = [];
for (const planPath of planPaths) {
	const doc = yaml.load(fs.readFileSync(planPath, "utf8"));
	for (const t of doc.tests || []) {
		selectedTests.push({
			plan: path.basename(planPath),
			name: String(t.name || "unnamed"),
			skip: !!t.skip,
			skip_reason: t.skip_reason || "skipped",
			setup: t.setup || [],
			steps: t.steps || [],
			expect: t.expect || [],
			shell: t.shell,
			shells: t.shells,
		});
	}
}

createPlaywrightTests({
	test,
	expect,
	shell: e2eShell,
	url: e2eURL,
	testCases: selectedTests,
	suiteLabel: "yaml.spec",
});
