#!/bin/sh

WDIR=$(dirname $(readlink -f "$0"))

(
    cd "$WDIR/.."
    go test -v -test.fullpath=true -run ^TestProxy$ github.com/myhops/tcpproxy/internal/proxy
)