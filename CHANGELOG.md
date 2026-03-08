# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.4.0] - 2026-03-08

Initial public release of clai — an intelligent terminal assistant that integrates Claude into your shell.

### Added
- Shell integration for Zsh, Bash, and Fish with install/uninstall commands
- Inline command suggestions with fish-like ghost text and visual picker menus
- gRPC daemon (`claid`) for low-latency suggestions and session management
- Command history tracking with session isolation and short-ID lookups
- V2 suggestion scorer with adaptive learning from accept/dismiss feedback
- Workflow engine for multi-step Claude-assisted command sequences
- Full-text search and template-based command discovery
- Voice-to-command support via Claude
- Cross-platform builds via GoReleaser with Homebrew tap
- Docker-based cross-distro test infrastructure (Alpine, Ubuntu, Debian, Fedora)
- Playwright E2E test harness for terminal interaction testing
