#!/bin/sh

mkdir -p ./bin
cd ./bin

echo "Downloading BusyBox"
wget https://busybox.net/downloads/binaries/1.30.0-i686/busybox
chmod u+x busybox

echo "Creating symlinks"
for i in $(busybox --list)
do
    if [ "$i" != "busybox" ]
    then
        ln -s busybox $i
    fi
done

echo "OK"
