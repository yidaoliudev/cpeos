#!/bin/bash
#set -eux
export GOOS=linux
export CGO_ENABLED=0
export GOARCH=amd64

work_path=$GOPATH"/src/sd-wan-cpeos"
path=`pwd`

if [ ! -d $work_path ]; then
    ln -s $path $work_path
fi

echo "path:"$path

rm -rf flow-cpeos
mkdir flow-cpeos

function checkRet(){
    if [ $? -eq 0 ]; then
        return 0
    else
        exit 100
    fi
}

function makecpeos() {
    #make cpe
    cd $work_path"/service"
    echo "make cpe"
    go build -o cpe
    checkRet
    cp cpe $path"/flow-cpeos"

    #tar
    cd $path
    rm -rf flow-cpeos.tar.gz
    tar -zcvf flow-cpeos-x86.tar.gz flow-cpeos
}

makecpeos

"$@"

