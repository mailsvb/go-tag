# Builds BlackHole as a custom-named AudioServerPlugIn using only the
# Command Line Tools (no Xcode). BlackHole/ is an unmodified git submodule.
include config.mk

SRC       := BlackHole/BlackHole/BlackHole.c
ICON_SRC  := BlackHole/BlackHole/BlackHole.icns
BUILD     := build
CLANG     ?= xcrun clang
BUNDLE    := $(BUILD)/$(NAME).driver
EXE       := $(BUNDLE)/Contents/MacOS/$(NAME)
HAL_DIR   := /Library/Audio/Plug-Ins/HAL
INSTALLED := $(HAL_DIR)/$(NAME).driver

DEFINES := \
	-DkDriver_Name='"$(NAME)"' \
	-DkPlugIn_BundleID='"$(BUNDLE_ID)"' \
	-DkPlugIn_Icon='"$(NAME).icns"' \
	-DkHas_Driver_Name_Format=false \
	-DkDevice_Name='"$(NAME)"' \
	-DkDevice2_IsHidden=true \
	-DkManufacturer_Name='"$(MANUFACTURER)"' \
	-DkNumber_Of_Channels=$(CHANNELS) \
	-DkCanBeDefaultSystemDevice=false

# Names of the defines above, checked against #ifndef guards by lint-defines.
DEFINE_NAMES := kDriver_Name kPlugIn_BundleID kPlugIn_Icon kHas_Driver_Name_Format \
	kDevice_Name kDevice2_IsHidden kManufacturer_Name kNumber_Of_Channels kCanBeDefaultSystemDevice

# No -fvisibility=hidden: CFPlugIn resolves the factory (BlackHole_Create) by name.
CFLAGS  := $(ARCHS) -mmacosx-version-min=$(MIN_OS) -Os -Wall -Wno-deprecated-declarations
LDFLAGS := -bundle -framework CoreAudio -framework CoreFoundation -framework Accelerate

.PHONY: all build sign install uninstall verify lint-defines clean

all: build sign

build: $(EXE) $(BUNDLE)/Contents/Info.plist $(BUNDLE)/Contents/Resources/$(NAME).icns

$(EXE): $(SRC) config.mk Makefile
	@mkdir -p $(dir $@)
	$(CLANG) $(CFLAGS) $(DEFINES) $(SRC) $(LDFLAGS) -o $@

$(BUNDLE)/Contents/Info.plist: Info.plist.in config.mk
	@mkdir -p $(dir $@)
	sed -e 's/@NAME@/$(NAME)/g' -e 's/@BUNDLE_ID@/$(BUNDLE_ID)/g' \
	    -e 's/@FACTORY_UUID@/$(FACTORY_UUID)/g' -e 's/@VERSION@/$(VERSION)/g' \
	    -e 's/@MIN_OS@/$(MIN_OS)/g' $< > $@
	plutil -lint $@

$(BUNDLE)/Contents/Resources/$(NAME).icns: $(ICON_SRC)
	@mkdir -p $(dir $@)
	cp $< $@

sign: build
	codesign --force --sign "$(SIGN_ID)" --timestamp=none $(BUNDLE)
	codesign --verify --verbose $(BUNDLE)

# Remove-then-copy (never copy over a loaded bundle), then restart coreaudiod.
install: sign
	sudo rm -rf "$(INSTALLED)"
	sudo ditto "$(BUNDLE)" "$(INSTALLED)"
	sudo chown -R root:wheel "$(INSTALLED)"
	sudo killall coreaudiod

uninstall:
	sudo rm -rf "$(INSTALLED)"
	sudo killall coreaudiod

verify:
	codesign -dvv "$(INSTALLED)" 2>&1 | grep -E 'Identifier|Signature|TeamIdentifier'
	@system_profiler SPAudioDataType | grep -A6 "$(NAME):" || (echo "$(NAME) device NOT found"; exit 1)

lint-defines:
	@for d in $(DEFINE_NAMES); do \
		grep -q "^#ifndef $$d$$" $(SRC) || { echo "not overridable: $$d"; exit 1; }; \
	done; echo "all defines are #ifndef-guarded"

clean:
	rm -rf $(BUILD)
