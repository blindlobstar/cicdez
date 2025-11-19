/*
   Copyright 2020 The Compose Specification Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package loader

import (
	"testing"

	"gotest.tools/v3/assert"
	is "gotest.tools/v3/assert/cmp"
)

func TestLoadLocalConfigs(t *testing.T) {
	actual, err := loadYAML(`
name: test-local-configs
services:
  web:
    image: nginx
    local_configs:
      nginx_conf:
        source: ./configs/nginx.conf
        target: /etc/nginx/nginx.conf
      app_config:
        source: ./app/config.yaml
        target: /app/config.yaml
        uid: "1000"
        gid: "1000"
        mode: 0440
`)
	assert.NilError(t, err)
	assert.Check(t, is.Len(actual.Services, 1))

	service := actual.Services["web"]
	assert.Check(t, is.Len(service.LocalConfigs, 2))
	assert.Check(t, is.Equal("./configs/nginx.conf", service.LocalConfigs["nginx_conf"].Source))
	assert.Check(t, is.Equal("/etc/nginx/nginx.conf", service.LocalConfigs["nginx_conf"].Target))
	assert.Check(t, is.Equal("./app/config.yaml", service.LocalConfigs["app_config"].Source))
	assert.Check(t, is.Equal("/app/config.yaml", service.LocalConfigs["app_config"].Target))
	assert.Check(t, is.Equal("1000", service.LocalConfigs["app_config"].UID))
	assert.Check(t, is.Equal("1000", service.LocalConfigs["app_config"].GID))
}

func TestLoadPrebuild(t *testing.T) {
	actual, err := loadYAML(`
name: test-prebuild
services:
  web:
    image: node:18
    build:
      context: .
    prebuild:
      - name: Test Suite
        runs-on: node:18
        commands:
          - name: Install dependencies
            command: npm ci
          - name: Run tests
            command: npm test
      - name: Lint
        commands:
          - name: Run linter
            command: npm run lint
`)
	assert.NilError(t, err)
	assert.Check(t, is.Len(actual.Services, 1))

	service := actual.Services["web"]
	assert.Check(t, is.Len(service.Prebuild, 2))

	// First prebuild job
	assert.Check(t, is.Equal("Test Suite", service.Prebuild[0].Name))
	assert.Check(t, is.Equal("node:18", service.Prebuild[0].RunsOn))
	assert.Check(t, is.Len(service.Prebuild[0].Commands, 2))
	assert.Check(t, is.Equal("Install dependencies", service.Prebuild[0].Commands[0].Name))
	assert.Check(t, is.Equal("npm ci", service.Prebuild[0].Commands[0].Command))
	assert.Check(t, is.Equal("Run tests", service.Prebuild[0].Commands[1].Name))
	assert.Check(t, is.Equal("npm test", service.Prebuild[0].Commands[1].Command))

	// Second prebuild job (without runs-on)
	assert.Check(t, is.Equal("Lint", service.Prebuild[1].Name))
	assert.Check(t, is.Equal("", service.Prebuild[1].RunsOn))
	assert.Check(t, is.Len(service.Prebuild[1].Commands, 1))
}

func TestLoadSensitive(t *testing.T) {
	actual, err := loadYAML(`
name: test-sensitive
services:
  db:
    image: postgres:15
    sensitive:
      app_env:
        target: /app/.env
        format: env
        secrets:
          - source: db_password
            name: DATABASE_PASSWORD
          - source: api_key
            name: API_KEY
        uid: "1000"
        gid: "1000"
        mode: 0440
      postgres_password:
        target: /run/secrets/postgres_password
        format: raw
        secrets:
          - source: db_password
        uid: "999"
        gid: "999"
        mode: 0440
`)
	assert.NilError(t, err)
	assert.Check(t, is.Len(actual.Services, 1))

	service := actual.Services["db"]
	assert.Check(t, is.Len(service.Sensitive, 2))

	// First sensitive config (env format)
	assert.Check(t, is.Equal("/app/.env", service.Sensitive["app_env"].Target))
	assert.Check(t, is.Equal("env", service.Sensitive["app_env"].Format))
	assert.Check(t, is.Len(service.Sensitive["app_env"].Secrets, 2))
	assert.Check(t, is.Equal("db_password", service.Sensitive["app_env"].Secrets[0].Source))
	assert.Check(t, is.Equal("DATABASE_PASSWORD", service.Sensitive["app_env"].Secrets[0].Name))
	assert.Check(t, is.Equal("api_key", service.Sensitive["app_env"].Secrets[1].Source))
	assert.Check(t, is.Equal("API_KEY", service.Sensitive["app_env"].Secrets[1].Name))
	assert.Check(t, is.Equal("1000", service.Sensitive["app_env"].UID))
	assert.Check(t, is.Equal("1000", service.Sensitive["app_env"].GID))

	// Second sensitive config (raw format)
	assert.Check(t, is.Equal("/run/secrets/postgres_password", service.Sensitive["postgres_password"].Target))
	assert.Check(t, is.Equal("raw", service.Sensitive["postgres_password"].Format))
	assert.Check(t, is.Len(service.Sensitive["postgres_password"].Secrets, 1))
	assert.Check(t, is.Equal("db_password", service.Sensitive["postgres_password"].Secrets[0].Source))
	assert.Check(t, is.Equal("999", service.Sensitive["postgres_password"].UID))
}

func TestLoadSensitiveDefaultTarget(t *testing.T) {
	actual, err := loadYAML(`
name: test-sensitive-default-target
services:
  db:
    image: postgres:15
    sensitive:
      admin_password:
        format: raw
        secrets:
          - source: db_password
      db_credentials:
        format: env
        secrets:
          - source: db_user
            name: DB_USER
          - source: db_password
            name: DB_PASSWORD
`)
	assert.NilError(t, err)
	assert.Check(t, is.Len(actual.Services, 1))

	service := actual.Services["db"]
	assert.Check(t, is.Len(service.Sensitive, 2))

	// Verify default target is /run/secrets/{name}
	assert.Check(t, is.Equal("/run/secrets/admin_password", service.Sensitive["admin_password"].Target))
	assert.Check(t, is.Equal("/run/secrets/db_credentials", service.Sensitive["db_credentials"].Target))
}

func TestLoadCicdezFieldsCombined(t *testing.T) {
	actual, err := loadYAML(`
name: test-all-cicdez-fields
services:
  app:
    image: myapp:latest
    build:
      context: .
    prebuild:
      - name: Tests
        runs-on: golang:1.21
        commands:
          - name: Run tests
            command: go test ./...
    local_configs:
      app_conf:
        source: ./configs/app.conf
        target: /etc/app/app.conf
    sensitive:
      app_env:
        target: /app/.env
        format: env
        secrets:
          - source: app_secret
            name: APP_SECRET
`)
	assert.NilError(t, err)
	assert.Check(t, is.Len(actual.Services, 1))

	service := actual.Services["app"]

	// Verify all cicdez fields are present
	assert.Check(t, is.Len(service.Prebuild, 1))
	assert.Check(t, is.Len(service.LocalConfigs, 1))
	assert.Check(t, is.Len(service.Sensitive, 1))

	// Quick sanity checks
	assert.Check(t, is.Equal("Tests", service.Prebuild[0].Name))
	assert.Check(t, is.Equal("./configs/app.conf", service.LocalConfigs["app_conf"].Source))
	assert.Check(t, is.Equal("/app/.env", service.Sensitive["app_env"].Target))
}
