.PHONY: dev build build-windows release test e2e

VERSION ?= dev
LDFLAGS := -s -w
PLATFORMS := linux/amd64 linux/arm64 linux/arm windows/amd64 darwin/amd64 darwin/arm64

# PocketBase + dashboard on http://127.0.0.1:8090/_/
# (extension JS/CSS changes need a restart since they are embedded)
dev:
	go run . serve

# single binary with everything embedded
build:
	CGO_ENABLED=0 go build -trimpath -o pocketbaseplus .

# Windows 64-bit executable (cross-compiled, runs without dependencies)
build-windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -o pocketbaseplus.exe .

# release archives in dist/ (one per platform, with README and LICENSE) and SHA256SUMS
release:
	rm -rf dist && mkdir -p dist
	@set -e; for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; goarm=; suffix=$$arch; \
		[ $$arch = arm ] && goarm=7 && suffix=armv7; \
		name=pocketbaseplus_$(VERSION)_$${os}_$${suffix}; \
		bin=pocketbaseplus; [ $$os = windows ] && bin=pocketbaseplus.exe; \
		echo "building $$name"; \
		mkdir -p dist/$$name; \
		GOARM=$$goarm CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$$name/$$bin .; \
		cp README.md LICENSE dist/$$name/; \
		if [ $$os = linux ]; then tar -C dist -czf dist/$$name.tar.gz $$name; \
		else (cd dist && python3 -m zipfile -c $$name.zip $$name); fi; \
		rm -rf dist/$$name; \
	done
	cd dist && sha256sum pocketbaseplus_* > SHA256SUMS

test:
	go test ./...

# browser tests (Node 22+): the first run installs Playwright and its Chromium
e2e:
	cd e2e && npm install --no-audit --no-fund && npx playwright install chromium && node run.mjs
