NAME := $(shell basename $(shell go list -m))
SRCS := $(wildcard *.go) $(wildcard */*.go) $(shell find backend/) $(shell find ui/) $(shell find res/) go.mod
ICONS := res/appicon.png res/appicon-256.png

IOS_XCFRAMEWORKS := Avcodec Avfilter Avformat Avutil Mbedcrypto Mbedtls Mbedx509 Mpv Swresample Swscale

BUILDINFO := codeberg.org/unixery/buildinfo

# Version information is derived from git (same approach as ../lyrunderground):
# the latest git tag becomes the app version and the commit count the build
# number. Both are embedded into the binary via ldflags (see res/metadata.go).
# if inside a git working tree, set some vars
ifeq ($(shell git rev-parse --is-inside-work-tree 2>/dev/null),true)
GIT_SHA := $(shell git rev-parse --short HEAD)
GIT_TAG := $(shell git describe --tags 2>/dev/null)
GIT_DIRTY := $(if $(strip $(shell git status --porcelain)),true,false)
else
GIT_SHA := n/a
GIT_TAG := n/a
GIT_DIRTY := n/a
endif
# optional: if empty, set dummy version
ifeq ($(strip $(GIT_TAG)),)
GIT_TAG := v0.0.0
endif
# optional: warn on dirty working tree, append -dirty to commit id
ifeq ($(GIT_DIRTY),true)
#$(warning Git working tree is dirty)
GIT_SHA := $(GIT_SHA)-dirty
endif

# Package metadata version. `git describe` yields values like v1.2.3-4-gabcde,
# which fyne rejects, so anything but a plain tag falls back to 0.0.0.
APP_VERSION := $(firstword $(subst -, ,$(patsubst v%,%,$(GIT_TAG))))
ifneq (,$(or $(filter n/a,$(APP_VERSION)),$(if $(APP_VERSION),,x)))
APP_VERSION := 0.0.0
endif

# Monotonic build number for the package metadata (FyneApp.toml Build field):
# the commit count only grows, which is what e.g. macOS expects for bundle
# build numbers.
APP_BUILD := $(shell git rev-list --count HEAD 2>/dev/null)
ifeq ($(APP_BUILD),)
APP_BUILD := 1
endif

# Linker flags that embed the git metadata into the binary.
GO_LDFLAGS := -X '$(BUILDINFO).GitSha=$(GIT_SHA)' -X '$(BUILDINFO).GitTag=$(GIT_TAG)' -s -w
# `fyne package` builds the binary itself and only picks up linker flags from
# GOFLAGS. Every -X has to be a separate "-ldflags=" field inside GOFLAGS,
# because fyne reads them from there and joins them again.
FYNE_GOFLAGS := -ldflags=-X=$(BUILDINFO).GitSha=$(GIT_SHA) -ldflags=-X=$(BUILDINFO).GitTag=$(GIT_TAG)
FYNE := GOFLAGS="$(FYNE_GOFLAGS)" fyne

# The binaries embed GIT_SHA/GIT_TAG via ldflags, but make only compares
# file mtimes, so a commit or tag alone would not trigger a rebuild.
# A state file bridges the gap; all binaries depend on it:
#
#   git-state (phony) -> bin/.git-state -> binaries
#
# git-state is .PHONY, so it is always out-of-date, which makes the
# bin/.git-state recipe run on every build. The recipe only rewrites
# the file when the value actually changed, so the file's mtime only
# changes then. The binaries depend on the real file, not the phony
# target: make compares mtimes, so they are rebuilt only when the file
# was rewritten. (Depending on the phony target directly would rebuild
# on every invocation.)
GIT_STATE := bin/.git-state

.PHONY: git-state
git-state:

$(GIT_STATE): git-state
	@mkdir -p bin
	@echo "$(GIT_SHA) $(GIT_TAG)" > $(GIT_STATE).tmp
	@if cmp -s $(GIT_STATE).tmp $(GIT_STATE) 2>/dev/null; then \
		rm -f $(GIT_STATE).tmp; \
	else \
		mv $(GIT_STATE).tmp $(GIT_STATE); \
	fi

# Print the package version derived from the latest git tag (APP_VERSION
# above), e.g. for passing to the Inno Setup installer via ISCC /DMyAppVersion
# in CI (see win_inno_installscript.iss and .github/workflows).
.PHONY: print-version
print-version:
	@echo "$(APP_VERSION)"

.PHONY: build
build: bin/$(NAME)

UNAME_S := $(shell uname -s)

ifeq ($(filter FreeBSD NetBSD,$(UNAME_S)),)
TAGS := migrated_fynedo
else
TAGS := migrated_fynedo,x11
endif

bin/$(NAME): $(SRCS) $(ICONS) $(GIT_STATE)
	@mkdir -p bin/
	@echo "Building..."
	go build -tags $(TAGS) -ldflags "$(GO_LDFLAGS)" -o bin/$(NAME)

# FyneApp.toml is generated from the template (it is not tracked by git, since
# tools like `fyne package` modify it, e.g. by incrementing Build). Version and
# build number come from the git state, so packaged builds follow new tags.
FyneApp.toml: FyneApp.toml.template $(GIT_STATE)
	sed -e 's/@APP_VERSION@/$(APP_VERSION)/' -e 's/@APP_BUILD@/$(APP_BUILD)/' FyneApp.toml.template > FyneApp.toml

.PHONY: package_macos
package_macos: FyneApp.toml package/darwin/DiscNegative.app

.PHONY: zip_macos
zip_macos: FyneApp.toml package/darwin/DiscNegative.zip

.PHONY: package_windows
package_windows: FyneApp.toml
	$(FYNE) package -os windows -tags migrated_fynedo -release

.PHONY: package_linux
package_linux: FyneApp.toml
	$(FYNE) package -os linux -tags migrated_fynedo -release

.PHONY: package_freebsd
package_freebsd: FyneApp.toml package/freebsd/DiscNegative.tar.xz

# dylibbundler doesn't seem to pick up on the Python framework dependency,
# so the last 3 cmds move it over manually. This is a bit fragile though
# since it assumes a specific location and version of the dependency
.PHONY: bundledeps_macos_homebrew
bundledeps_macos_homebrew: package/darwin/DiscNegative.app
	dylibbundler -od -b -x package/darwin/DiscNegative.app/Contents/MacOS/discnegative -d package/darwin/DiscNegative.app/Contents/Frameworks/ -p @executable_path/../Frameworks/
	./scripts/copy_python_dep_osx.sh
	codesign --force --deep --preserve-metadata=entitlements,requirements,flags,runtime --sign - "package/darwin/DiscNegative.app/Contents/MacOS/discnegative"

.PHONY: bundledeps_macos_macports
bundledeps_macos_macports: package/darwin/DiscNegative.app
	dylibbundler -od -b -x package/darwin/DiscNegative.app/Contents/MacOS/discnegative -d package/darwin/DiscNegative.app/Contents/Frameworks/ -p @executable_path/../Frameworks/
	codesign --force --deep --preserve-metadata=entitlements,requirements,flags,runtime --sign - "package/darwin/DiscNegative.app/Contents/MacOS/discnegative"

.PHONY: bundledeps_macos_highsierra
bundledeps_macos_highsierra: package/darwin/DiscNegative.app
	mkdir package/darwin/DiscNegative.app/Contents/Frameworks
	cp -R res/libs/mac_x64/mpv/* package/darwin/DiscNegative.app/Contents/Frameworks
	install_name_tool -change "/usr/local/opt/mpv/lib/libmpv.2.dylib" "@executable_path/../Frameworks/libmpv.2.dylib" "package/darwin/DiscNegative.app/Contents/MacOS/discnegative"

.PHONY: bundledeps_iossimulator
bundledeps_iossimulator: package/iossimulator/DiscNegative.app # only when using *media-kit/libmpv-darwin-build*, just for testing
	mkdir -p package/iossimulator/DiscNegative.app/Frameworks
	for name in $(IOS_XCFRAMEWORKS); do \
		src="/opt/xcframework/media-kit-ios/$$name.xcframework/ios-arm64_x86_64-simulator/$$name.framework"; \
		cp -a "$$src" "package/iossimulator/DiscNegative.app/Frameworks/"; \
	done
	install_name_tool -add_rpath '@executable_path/Frameworks' package/iossimulator/DiscNegative.app/main
	codesign --force --deep --sign - --timestamp=none package/iossimulator/DiscNegative.app

.PHONY: bundledeps_ios
bundledeps_ios: package/ios/DiscNegative.app # only when using *media-kit/libmpv-darwin-build*, just for testing
	mkdir -p package/ios/DiscNegative.app/Frameworks
	for name in $(IOS_XCFRAMEWORKS); do \
		src="/opt/xcframework/media-kit-ios/$$name.xcframework/ios-arm64/$$name.framework"; \
		cp -a "$$src" "package/ios/DiscNegative.app/Frameworks/"; \
	done
	install_name_tool -add_rpath '@executable_path/Frameworks' package/ios/DiscNegative.app/main
	codesign --force --deep --sign - --timestamp=none package/ios/DiscNegative.app

package/darwin/DiscNegative.app: FyneApp.toml $(SRCS) $(ICONS) $(GIT_STATE)
	@mkdir -p package/darwin/
	@echo "Packaging for darwin (version $(APP_VERSION), build $(APP_BUILD))..."
	@rm -rf package/darwin/DiscNegative.app
	MACOSX_DEPLOYMENT_TARGET=10.14.0 $(FYNE) package -os darwin -tags migrated_fynedo -release
	mv DiscNegative.app package/darwin/

package/darwin/DiscNegative.zip: package/darwin/DiscNegative.app
	@cd package/darwin/ && zip --symlinks -r DiscNegative.zip DiscNegative.app/

package/iossimulator/DiscNegative.app: FyneApp.toml $(SRCS) $(ICONS) $(GIT_STATE) # just for testing
	# run the app on macOS:
	# xcrun simctl list devices available
	# xcrun simctl boot "iPad Air 11-inch (M4)"
	# xcrun simctl install booted package/iossimulator/DiscNegative.app
	# open "$(xcode-select -p)/../Applications/DeviceHub.app"
	# xcrun simctl launch --console-pty booted io.github.thenktor.discnegative 2>&1 | tee /tmp/discnegative.log
	@mkdir -p package/iossimulator/
	@echo "Packaging for iossimulator..."
	@rm -rf package/iossimulator/DiscNegative.app
	IPHONEOS_DEPLOYMENT_TARGET=15.0 $(FYNE) package -os iossimulator -tags migrated_fynedo,iossimulator
	mv DiscNegative.app package/iossimulator/

package/ios/DiscNegative.app: FyneApp.toml $(SRCS) $(ICONS) $(GIT_STATE) # just for testing
	# xcrun devicectl list devices
	# xcrun devicectl device install app --device 12345678-0001112220ABCDEF package/ios/DiscNegative.app
	@mkdir -p package/ios/
	@echo "Packaging for ios..."
	@rm -rf package/ios/DiscNegative.app
	IPHONEOS_DEPLOYMENT_TARGET=15.0 $(FYNE) package -os ios -tags migrated_fynedo -release
	mv DiscNegative.app package/ios/

package/freebsd/DiscNegative.tar.xz: FyneApp.toml $(SRCS) $(ICONS) $(GIT_STATE)
	@mkdir -p package/freebsd/
	@echo "Packaging for freebsd (version $(APP_VERSION), build $(APP_BUILD))..."
	@rm -rf package/freebsd/DiscNegative.tar.xz
	$(FYNE) package -os freebsd -tags migrated_fynedo,x11 -release
	mv DiscNegative.tar.xz package/freebsd/

res/appicon.png: res/appicon.svg
	magick -background none "res/appicon.svg[1024x1024]" -define png:compression-level=9 "res/appicon.png" 2>/dev/null

res/appicon-256.png: res/appicon.svg
	magick -background none "res/appicon.svg[1024x1024]" -define png:compression-level=9 "res/appicon-256.png" 2>/dev/null

.PHONY: lint
lint:
	golangci-lint run

.PHONY: clean
clean:
	go clean
	rm -rf bin/
	rm -rf package/
	rm -rf DiscNegative.app DiscNegative.zip DiscNegative.tar.xz
	rm -f FyneApp.toml
