NAME := $(shell basename $(shell go list -m))
SRCS := $(wildcard *.go) $(wildcard */*.go) $(shell find backend/) $(shell find ui/) $(shell find res/)

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

bin/${NAME}: $(SRCS)
	@mkdir -p bin/
	@echo "Building..."
	go build -tags $(TAGS) -ldflags "-s -w" -o bin/${NAME}

# dylibbundler doesn't seem to pick up on the Python framework dependency,
# so the last 3 cmds move it over manually. This is a bit fragile though
# since it assumes a specific location and version of the dependency
.PHONY: package_macos
package_macos: Supersonic.app

.PHONY: bundledeps_macos_homebrew
bundledeps_macos_homebrew:
	dylibbundler -od -b -x ./Supersonic.app/Contents/MacOS/supersonic -d ./Supersonic.app/Contents/Frameworks/ -p @executable_path/../Frameworks/
	./scripts/copy_python_dep_osx.sh
	codesign --force --deep --preserve-metadata=entitlements,requirements,flags,runtime --sign - "./Supersonic.app/Contents/MacOS/supersonic"

.PHONY: bundledeps_macos_macports
bundledeps_macos_macports:
	dylibbundler -od -b -x ./Supersonic.app/Contents/MacOS/supersonic -d ./Supersonic.app/Contents/Frameworks/ -p @executable_path/../Frameworks/
	codesign --force --deep --preserve-metadata=entitlements,requirements,flags,runtime --sign - "./Supersonic.app/Contents/MacOS/supersonic"

.PHONY: bundledeps_macos_highsierra
bundledeps_macos_highsierra:
	mkdir ./Supersonic.app/Contents/Frameworks
	cp -R res/libs/mac_x64/mpv/* ./Supersonic.app/Contents/Frameworks
	install_name_tool -change "/usr/local/opt/mpv/lib/libmpv.2.dylib" "@executable_path/../Frameworks/libmpv.2.dylib" "./Supersonic.app/Contents/MacOS/supersonic"

.PHONY: zip_macos
zip_macos: Supersonic.zip

.PHONY: package_windows
package_windows:
	fyne package -os windows -tags migrated_fynedo

.PHONY: package_linux
package_linux:
	fyne package -os linux -tags migrated_fynedo

.PHONY: package_freebsd
package_freebsd:
	fyne package -os freebsd -tags migrated_fynedo,x11

Supersonic.app:
	MACOSX_DEPLOYMENT_TARGET=10.13.2 fyne package -os darwin -tags migrated_fynedo

Supersonic.zip: Supersonic.app
	zip --symlinks -r Supersonic.zip Supersonic.app/

.PHONY: lint
lint:
	golangci-lint run

.PHONY: clean
clean:
	go clean
	rm -rf bin/
	rm -rf tar/
	rm -rf Supersonic.app Supersonic.zip Supersonic.tar.xz
