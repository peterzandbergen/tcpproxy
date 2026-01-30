#!/bin/sh

PORT=9001

socat TCP-LISTEN:$PORT,fork,reuseaddr EXEC:"tee /dev/stderr"