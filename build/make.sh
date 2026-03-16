#!/bin/bash
export GOARCH=arm
export GOOS=linux
go build -o ../bin/emu ../cmd/emu.go

# copy image to breakout
scp ../bin/emu pi@raspberrypi:/tmp
scp ../config/config.yaml pi@raspberrypi:/tmp
#scp -i ~/OneDrive/ssh-keys/pi/x  ../config/tadl.yaml pi@heatpump:/tmp

echo '# logon on raspberry, eg:'
echo '# ssh pi@raspberrypi'

echo '# install emu on target system'
echo '# chmod 755 /tmp/emu ;/tmp/tadl --config /tmp/emu.yaml'