#!/bin/sh

PORT=9000

cat <<EOF | nc -q 1 localhost $PORT
alot of text
comes here
EOF

