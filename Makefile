NAME := $(shell basename $(shell go list -m))
SRCS := $(wildcard *.go) $(wildcard */*.go) $(shell find backend/) $(shell find ui/) $(shell find res/) go.mod
ICONS := res/appicon.png res/appicon-256.png

.PHONY: build
build: bin/${NAME}

UNAME_S := $(shell uname -s)

ifeq ($(filter FreeBSD NetBSD,$(UNAME_S)),)
TAGS := migrated_fynedo
else
TAGS := migrated_fynedo,x11
CGO_CFLAGS := -I/usr/local/include
CGO_LDFLAGS := -L/usr/local/lib
endif
export CGO_CFLAGS
export CGO_LDFLAGS

bin/${NAME}: $(SRCS) $(ICONS)
	@mkdir -p bin/
	@echo "Building..."
	go build -tags $(TAGS) -ldflags "-s -w" -o bin/${NAME}

# dylibbundler doesn't seem to pick up on the Python framework dependency,
# so the last 3 cmds move it over manually. This is a bit fragile though
# since it assumes a specific location and version of the dependency
.PHONY: package_macos
package_macos: package/darwin/DiscNegative.app

.PHONY: bundledeps_macos_homebrew
bundledeps_macos_homebrew:
	dylibbundler -od -b -x ./DiscNegative.app/Contents/MacOS/supersonic -d ./DiscNegative.app/Contents/Frameworks/ -p @executable_path/../Frameworks/
	./scripts/copy_python_dep_osx.sh
	codesign --force --deep --preserve-metadata=entitlements,requirements,flags,runtime --sign - "./DiscNegative.app/Contents/MacOS/supersonic"

.PHONY: bundledeps_macos_macports
bundledeps_macos_macports:
	dylibbundler -od -b -x ./DiscNegative.app/Contents/MacOS/supersonic -d ./DiscNegative.app/Contents/Frameworks/ -p @executable_path/../Frameworks/
	codesign --force --deep --preserve-metadata=entitlements,requirements,flags,runtime --sign - "./DiscNegative.app/Contents/MacOS/supersonic"

.PHONY: bundledeps_macos_highsierra
bundledeps_macos_highsierra:
	mkdir ./DiscNegative.app/Contents/Frameworks
	cp -R res/libs/mac_x64/mpv/* ./DiscNegative.app/Contents/Frameworks
	install_name_tool -change "/usr/local/opt/mpv/lib/libmpv.2.dylib" "@executable_path/../Frameworks/libmpv.2.dylib" "./DiscNegative.app/Contents/MacOS/supersonic"

.PHONY: zip_macos
zip_macos: package/darwin/DiscNegative.zip

.PHONY: package_windows
package_windows:
	fyne package -os windows -tags migrated_fynedo -release

.PHONY: package_linux
package_linux:
	fyne package -os linux -tags migrated_fynedo -release

.PHONY: package_freebsd
package_freebsd:
	fyne package -os freebsd -tags migrated_fynedo,x11 -release

package/darwin/DiscNegative.app: $(SRCS) $(ICONS)
	@mkdir -p package/darwin/
	@echo "Packaging for darwin..."
	@rm -rf package/darwin/DiscNegative.app
	MACOSX_DEPLOYMENT_TARGET=10.14.0 fyne package -os darwin -tags migrated_fynedo -release
	mv DiscNegative.app package/darwin/

package/darwin/DiscNegative.zip: package/darwin/DiscNegative.app
	@cd package/darwin/ && zip --symlinks -r DiscNegative.zip DiscNegative.app/

package/iossimulator/DiscNegative.app: $(SRCS) $(ICONS) # just for testing
	# run the app on macOS:
	# xcrun simctl list devices available
	# xcrun simctl boot "iPad Air 11-inch (M4)"
	# xcrun simctl install booted package/iossimulator/DiscNegative.app
	# open "$(xcode-select -p)/../Applications/DeviceHub.app"
	# xcrun simctl launch --console-pty booted io.github.thenktor.discnegative 2>&1 | tee /tmp/discnegative.log
	@mkdir -p package/iossimulator/
	@echo "Packaging for iossimulator..."
	@rm -rf package/iossimulator/DiscNegative.app
	IPHONEOS_DEPLOYMENT_TARGET=15.0 fyne package -os iossimulator -tags migrated_fynedo,iossimulator
	mv DiscNegative.app package/iossimulator/

package/ios/DiscNegative.app: $(SRCS) $(ICONS) # just for testing
	@mkdir -p package/ios/
	@echo "Packaging for ios..."
	@rm -rf package/ios/DiscNegative.app
	IPHONEOS_DEPLOYMENT_TARGET=15.0 fyne package -os ios -tags migrated_fynedo -release
	mv DiscNegative.app package/ios/

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
