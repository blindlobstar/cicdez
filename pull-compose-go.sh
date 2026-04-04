#!/bin/sh

git subtree pull -P external/compose-go git@github.com:compose-spec/compose-go.git "$1" --squash
