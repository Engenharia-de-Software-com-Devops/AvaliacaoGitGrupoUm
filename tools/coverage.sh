#!/bin/sh
set -eu

python3 -m unittest discover -s tools -p '*_test.py'
go test -coverprofile=/tmp/coverage.out .
python3 tools/line_coverage.py /tmp/coverage.out api.go db.go
