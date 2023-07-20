#!/bin/bash -e

rm -f *.log
go get;
go build;

./gh-pma $@