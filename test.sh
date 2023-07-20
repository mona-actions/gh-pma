#!/bin/bash -e

rm -f *.log
rm -f *.csv
go get;
go build;

./gh-pma $@