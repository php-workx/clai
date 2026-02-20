const { defineConfig } = require("@playwright/test");

module.exports = defineConfig({
	testDir: __dirname,
	testMatch: ["*.spec.cjs"],
	timeout: 60_000,
	fullyParallel: false,
	workers: 1,
	use: {
		headless: true,
		viewport: { width: 1400, height: 900 },
		screenshot: "only-on-failure",
		trace: "off",
		video: "off",
	},
});
