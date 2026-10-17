# Driver identity. Changing NAME changes the device UID ("$(NAME)_UID"),
# which the recorder uses to find the device.
NAME         ?= Gocorder
BUNDLE_ID    ?= dev.local.gocorder.driver
# Generated once with uuidgen; must differ from upstream BlackHole's factory UUID.
FACTORY_UUID ?= 9C3D4A19-43FC-440A-8897-8FA322441421
MANUFACTURER ?= gocorder
CHANNELS     ?= 2
VERSION      ?= 0.7.1-gocorder.1

# "-" = ad-hoc signature. Set to "Developer ID Application: Name (TEAMID)" if needed.
SIGN_ID      ?= -
ARCHS        ?= -arch arm64
MIN_OS       ?= 12.0
