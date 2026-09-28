PROJECT_NAME := Pulumi Provider Osano

PACK             := osano
PACKDIR          := sdk
PROJECT          := github.com/jflavan/pulumi-osano
NODE_MODULE_NAME := @jflavan/pulumi-osano
NUGET_PKG_NAME   := Community.Pulumi.Osano

PROVIDER        := pulumi-resource-${PACK}
PROVIDER_PATH   := provider
VERSION_PATH    := ${PROVIDER_PATH}/version.Version

PULUMI          := pulumi

SCHEMA_FILE     := provider/cmd/pulumi-resource-osano/schema.json
# Each SDK ships the README for its own language to its package registry (npm, PyPI, NuGet, pkg.go.dev).
PACKAGE_READMES := docs/package-readmes
export GOPATH   := $(shell go env GOPATH)

WORKING_DIR     := $(shell pwd)
TESTPARALLELISM := 4
# The race detector needs cgo. Set RACE= to run the tests on a toolchain without it.
RACE            ?= -race

# Override during CI using `make [TARGET] PROVIDER_VERSION=""` or by setting a PROVIDER_VERSION environment variable
# Local & branch builds will just used this fixed default version unless specified
PROVIDER_VERSION ?= 0.1.0-alpha.0+dev
# Use this normalised version everywhere rather than the raw input to ensure consistency.
VERSION_GENERIC = $(shell pulumictl convert-version --language generic --version "$(PROVIDER_VERSION)")

# Need to pick up locally pinned pulumi-langage-* plugins.
export PULUMI_IGNORE_AMBIENT_PLUGINS = true

# Keep a cheap target first: the CodeQL autobuild runs `make` with no target.
ensure::
	go mod tidy

# The provider sets language.go.importBasePath itself; the version is dropped so the committed
# schema does not change with every build version.
$(SCHEMA_FILE): provider
	$(PULUMI) package get-schema $(WORKING_DIR)/bin/${PROVIDER} | \
		jq 'del(.version)' > $(SCHEMA_FILE)

# Codegen generates the schema file and *generates* all sdks. This is a local process and
# does not require the ability to build all SDKs.
#
# To build the SDKs, use `make build_sdks`
codegen: $(SCHEMA_FILE) sdk/dotnet sdk/go sdk/nodejs sdk/python sdk/java

sdk/nodejs: $(SCHEMA_FILE)
	rm -rf $@
	$(PULUMI) package gen-sdk --language nodejs $(SCHEMA_FILE) --version "${VERSION_GENERIC}"
	cp ${PACKAGE_READMES}/nodejs.md ${PACKDIR}/nodejs/README.md

sdk/java: $(SCHEMA_FILE)
	rm -rf $@
	$(PULUMI) package gen-sdk --language java $(SCHEMA_FILE)
	find sdk/java -name '*.java' -type f -exec perl -pi -e 's/[ \t]+$$//' {} +
	# Generated settings.gradle references a non-existent 'lib' module; drop it for a single-module build.
	@if [ "$$(uname)" = "Darwin" ]; then \
		sed -i '' '/^include("lib")/d' sdk/java/settings.gradle; \
	else \
		sed -i '/^include("lib")/d' sdk/java/settings.gradle; \
	fi
	# Post-process build.gradle for Maven Central publishing
	# pulumi-java-gen doesn't support all POM metadata fields, so we use a Python script for reliable patching
	@echo "Post-processing Java SDK build.gradle for Maven Central..."
	@python3 scripts/patch-java-build-gradle.py sdk/java/build.gradle

sdk/python: $(SCHEMA_FILE)
	rm -rf $@
	# The provider enables pyproject.toml generation, which takes the package version from --version.
	$(PULUMI) package gen-sdk --language python $(SCHEMA_FILE) --version "${VERSION_GENERIC}"
	@python3 scripts/normalize-python-sdk.py ${PACKDIR}/python
	cp ${PACKAGE_READMES}/python.md ${PACKDIR}/python/README.md
	# setuptools packages LICENSE* files it finds next to pyproject.toml into the sdist and wheel.
	cp LICENSE ${PACKDIR}/python/LICENSE

sdk/dotnet: $(SCHEMA_FILE)
	rm -rf $@
	$(PULUMI) package gen-sdk --language dotnet $(SCHEMA_FILE) --version "${VERSION_GENERIC}"
	cp ${PACKAGE_READMES}/dotnet.md ${PACKDIR}/dotnet/README.md
	@python3 scripts/patch-dotnet-csproj.py sdk/dotnet/Community.Pulumi.Osano.csproj
	# The generator downloads the schema's logoUrl into logo.png (the NuGet package icon). Use the
	# committed copy instead so codegen output never depends on what that URL serves.
	cp assets/logo.png ${PACKDIR}/dotnet/logo.png

# The Go SDK's go.mod starts from the provider's, minus the toolchain line: consumers should not be
# made to download this repository's toolchain, only to meet the Pulumi SDK's own minimum.
sdk/go: ${SCHEMA_FILE}
	rm -rf $@
	$(PULUMI) package gen-sdk --language go ${SCHEMA_FILE} --version "${VERSION_GENERIC}"
	GO_PKG_DIR=${PACKDIR}/go/osano; \
	mkdir -p $$GO_PKG_DIR; \
	cp ${PACKAGE_READMES}/go.md $$GO_PKG_DIR/README.md; \
	cp go.mod $$GO_PKG_DIR/go.mod; \
	cd $$GO_PKG_DIR && \
		go mod edit -module=github.com/jflavan/pulumi-osano/sdk/go/osano -toolchain=none && \
		go mod tidy

.PHONY: provider
provider: bin/${PROVIDER}

# Provider source files to track for rebuilds
PROVIDER_SRC := $(shell find provider -name '*.go')

bin/${PROVIDER}: $(PROVIDER_SRC)
	cd provider && go build -o $(WORKING_DIR)/bin/${PROVIDER} -ldflags "-X ${PROJECT}/${VERSION_PATH}=${VERSION_GENERIC}" $(PROJECT)/${PROVIDER_PATH}/cmd/$(PROVIDER)

.PHONY: provider_debug
provider_debug:
	(cd provider && go build -o $(WORKING_DIR)/bin/${PROVIDER} -gcflags="all=-N -l" -ldflags "-X ${PROJECT}/${VERSION_PATH}=${VERSION_GENERIC}" $(PROJECT)/${PROVIDER_PATH}/cmd/$(PROVIDER))

.PHONY: test_provider
test_provider:
	cd provider && go test -short $(RACE) -v -count=1 -cover -timeout 2h -parallel ${TESTPARALLELISM} -coverprofile="coverage.txt" ./...

# Builds the .NET package in Release configuration: it is what `make build_dotnet` publishes.
dotnet_sdk: sdk/dotnet
	cd ${PACKDIR}/dotnet/&& \
		cp ../../${PACKAGE_READMES}/dotnet.md README.md && \
		echo "${VERSION_GENERIC}" > version.txt && \
		dotnet build -c Release

# Compiles the generated Go SDK and the program that consumes it through a replace directive, so a
# release never tags a Go module version that does not compile.
go_sdk: sdk/go
	cd ${PACKDIR}/go/osano && go build ./... && go vet ./...
	cd examples/quickstart/go && go build -o /dev/null .

nodejs_sdk: sdk/nodejs
	cd ${PACKDIR}/nodejs/ && \
		yarn install && \
		yarn run tsc
	cp ${PACKDIR}/nodejs/README.md LICENSE ${PACKDIR}/nodejs/package.json ${PACKDIR}/nodejs/yarn.lock ${PACKDIR}/nodejs/bin/

python_sdk: sdk/python
	cd ${PACKDIR}/python/ && \
		rm -rf ./bin/ ../python.bin/ && cp -R . ../python.bin && mv ../python.bin ./bin && \
		python3 -m venv venv && \
		./venv/bin/python -m pip install build && \
		cd ./bin && \
		../venv/bin/python -m build .

# The generated build.gradle reads the package version from the environment.
java_sdk:: export PACKAGE_VERSION := $(VERSION_GENERIC)
java_sdk:: sdk/java
	cd sdk/java/ && \
		gradle --console=plain build

.PHONY: build
build:: provider build_sdks

.PHONY: build_sdks
build_sdks: dotnet_sdk go_sdk nodejs_sdk python_sdk java_sdk

.PHONY: build_cookie_consent_examples build_examples
build_cookie_consent_examples: dotnet_sdk nodejs_sdk
	dotnet build examples/cookie-consent/csharp/CookieConsent.csproj
	# Compiles the same example for every supported .NET version (net8.0 and net10.0).
	dotnet build tests/dotnet/SdkCompatibility.csproj
	cd examples/cookie-consent/typescript && yarn install --frozen-lockfile && yarn run tsc --noEmit

.PHONY: build_quickstart_examples
build_quickstart_examples:
	cd examples/quickstart/go && go build -o /dev/null .

build_examples: build_cookie_consent_examples build_quickstart_examples

# lint reports, as CI does; lint_fix also rewrites files.
.PHONY: lint lint_fix
lint:
	golangci-lint --path-prefix provider --config .golangci.yml run

lint_fix:
	golangci-lint --path-prefix provider --config .golangci.yml run --fix

install:: install_nodejs_sdk install_dotnet_sdk
	cp $(WORKING_DIR)/bin/${PROVIDER} ${GOPATH}/bin

# Compiles every e2e build-tag set without running it, so a broken e2e suite fails without live credentials.
.PHONY: test_e2e_compile
test_e2e_compile:
	go vet -tags 'e2e consentread' ./tests/...
	go vet -tags 'e2e consentwrite' ./tests/...
	go vet -tags 'e2e subjectverification' ./tests/...
	go vet -tags 'e2e pipeline' ./tests/...

# Runs the provider through real Pulumi engine operations (a Pulumi YAML program's up, preview,
# refresh, and destroy) against an in-process mock of the Osano Customer REST API. No credentials
# are needed; the suite builds and installs the provider plugin itself and needs the pulumi CLI on PATH.
.PHONY: test_pipeline_e2e
test_pipeline_e2e:
	OSANO_RUN_PIPELINE_E2E=1 go test -tags 'e2e pipeline' -count=1 -timeout 20m -v ./tests/e2e/pipeline/...

.PHONY: test_scripts
test_scripts:
	python3 -m unittest discover -s scripts -p 'test_*.py'

# Reports known vulnerabilities reachable from the provider binary (advisory; needs network access).
.PHONY: vulncheck
vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

test_all:: test test_e2e_compile test_scripts test_pipeline_e2e

install_dotnet_sdk::
	rm -rf $(WORKING_DIR)/nuget/$(NUGET_PKG_NAME).*.nupkg
	mkdir -p $(WORKING_DIR)/nuget
	find . -name '*.nupkg' -print -exec cp -p {} ${WORKING_DIR}/nuget \;

install_python_sdk::
	#target intentionally blank

install_go_sdk::
	#target intentionally blank

install_java_sdk::
	#target intentionally blank

install_nodejs_sdk::
	-yarn unlink --cwd $(WORKING_DIR)/sdk/nodejs/bin
	yarn link --cwd $(WORKING_DIR)/sdk/nodejs/bin

test:: test_provider

.PHONY: generate_schema
generate_schema: ${SCHEMA_FILE} # Required by CI

.PHONY: build_go install_go_sdk
generate_go: sdk/go # Required by CI
build_go: go_sdk # Required by CI

.PHONY: build_java install_java_sdk
generate_java: sdk/java # Required by CI
build_java: java_sdk # Required by CI

.PHONY: build_python install_python_sdk
generate_python: sdk/python # Required by CI
build_python: python_sdk # Required by CI

.PHONY: build_nodejs install_nodejs_sdk
generate_nodejs: sdk/nodejs # Required by CI
build_nodejs: nodejs_sdk # Required by CI

.PHONY: build_dotnet install_dotnet_sdk
generate_dotnet: sdk/dotnet # Required by CI
build_dotnet: dotnet_sdk # Required by CI
