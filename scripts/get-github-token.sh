#!/bin/sh

bw get item "Github Access Token tcpproxy" | yq -p json '.login.password'

