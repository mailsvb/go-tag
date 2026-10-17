# macOS build helpers for the recorder. Windows builds need none of this:
# `go run .` and `fyne package` work as described in the README.
VERSION   ?= 0.0.0
BUILD_NO  ?= 1
BUILD     := build
CLANG     ?= xcrun clang
ADAPTER   := third_party/mediaremote-adapter
FRAMEWORK := $(BUILD)/adapter/MediaRemoteAdapter.framework
FW_BIN    := $(FRAMEWORK)/Versions/A/MediaRemoteAdapter

ADAPTER_SRC := $(addprefix $(ADAPTER)/src/,adapter/env.m adapter/get.m adapter/globals.m \
	adapter/keys.m adapter/now_playing.m adapter/repeat.m adapter/seek.m adapter/send.m \
	adapter/shuffle.m adapter/speed.m adapter/stream.m adapter/test.m private/MediaRemote.m \
	utility/Debounce.m utility/helpers.m)

.PHONY: adapter app driver driver-install driver-uninstall test clean

# MediaRemoteAdapter.framework (the equivalent of upstream's CMake build) and
# the perl script that loads it. Point GOTAG_MEDIAREMOTE_DIR at build/adapter,
# or run go-tag from the repository root, to use them during development.
adapter: $(FW_BIN) $(BUILD)/adapter/mediaremote-adapter.pl

$(FW_BIN): $(ADAPTER_SRC) scripts/MediaRemoteAdapter-Info.plist
	rm -rf $(FRAMEWORK)
	mkdir -p $(FRAMEWORK)/Versions/A/Resources
	$(CLANG) -arch arm64 -mmacosx-version-min=12.0 -dynamiclib -fobjc-arc -fvisibility=default -Os -w \
		-I$(ADAPTER)/include -I$(ADAPTER)/src $(ADAPTER_SRC) \
		-framework Foundation -framework AppKit -framework UniformTypeIdentifiers \
		-install_name @rpath/MediaRemoteAdapter.framework/MediaRemoteAdapter -o $@
	cp scripts/MediaRemoteAdapter-Info.plist $(FRAMEWORK)/Versions/A/Resources/Info.plist
	ln -s A $(FRAMEWORK)/Versions/Current
	ln -s Versions/Current/MediaRemoteAdapter $(FRAMEWORK)/MediaRemoteAdapter
	ln -s Versions/Current/Resources $(FRAMEWORK)/Resources
	codesign --force --sign - $(FRAMEWORK)

$(BUILD)/adapter/mediaremote-adapter.pl: $(ADAPTER)/bin/mediaremote-adapter.pl
	@mkdir -p $(dir $@)
	cp $< $@

# go-tag.app with the adapter and the privacy texts (needs the fyne tool:
# go install fyne.io/tools/cmd/fyne@latest).
app: adapter
	rm -rf go-tag.app
	fyne package --target darwin --release --app-version $(VERSION) --app-build $(BUILD_NO)
	scripts/bundle-macos.sh go-tag.app $(BUILD)/adapter

# The Gocorder audio device (see driver/README.md).
driver:
	$(MAKE) -C driver all

driver-install:
	$(MAKE) -C driver install

driver-uninstall:
	$(MAKE) -C driver uninstall

test:
	go test -race ./...

clean:
	rm -rf $(BUILD) go-tag.app
	$(MAKE) -C driver clean
