#!/bin/sh

git subtree pull -P external/compose-go git@github.com:compose-spec/compose-go.git "$1" --squash
# Update version in go.mod
sed -i '' "s|github.com/compose-spec/compose-go/v2 v[^ ]*|github.com/compose-spec/compose-go/v2 $1|" go.mod
