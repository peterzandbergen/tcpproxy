# Session Start

**IMPORTANT: At the start of each session, read `transfer/LATEST.md` for context from previous work.**

# Context

* add code to setup opentelemetry for traces, metrics and logs in separate file in internal. Do not integrate it in the rest of the code yet (tcpproxy/main.go)

## Build instructions
* Use goreleaser release --clean --snapshot to build the executable
* Alternatively build with -o under /tmp
* The executable is under dist
* Select the correct os and cpu for the system you are on when running the executable

## Requirements
* The application must support opentelemetry and be able to select between the stdout or the otlp exporter
* The application must log to stderr when opentelemetry is disabled or when the otlp exporter is selected

## Version Control
* Use jj version 0.37.0-11

## Scripts
* Always create scripts for sh, not bash

## Session Start and Transfer
* Read `transfer/LATEST.md` at the start of each session for context
* Transfer documents are stored in `transfer/` with date-based names (e.g., `2026-02-02.md`)
* `transfer/LATEST.md` symlink points to the most recent document
* Create a new transfer document at the end of each session