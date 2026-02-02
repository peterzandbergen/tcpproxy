# Context

* add code to setup opentelemetry for traces, metrics and logs in separate file in internal. Do not integrate it in the rest of the code yet (tcpproxy/main.go)

## Build instructions
* Use goreleaser release --clean --snapshot to build the executable
* The executable is under dist
* Select the correct os and cpu for the system you are on when running the executable

## Requirements
* The application must support opentelemetry and be able to select between the stdout or the otlp exporter
* The application must log to stderr when opentelemetry is disabled or when the otlp exporter is selected

## Version Control
* Use jj version 0.37.0-11

## Scripts
* Always create scripts for sh, not bash