#!/bin/sh

PORT=9000
NMESSAGES=10

echo_message() {
cat <<EOF | nc -q 1 localhost $PORT
alot of text
comes here
EOF
}

for i in $(seq 1 100); do
    echo_message &
    # sleep 0.001
done


