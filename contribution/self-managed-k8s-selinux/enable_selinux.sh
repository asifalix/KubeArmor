#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2021 Authors of KubeArmor

realpath() {
    CURR=$PWD

    cd "$(dirname "$0")"
    LINK=$(readlink "$(basename "$0")")

    while [ "$LINK" ]; do
        cd "$(dirname "$LINK")"
        LINK=$(readlink "$(basename "$1")")
    done

    REALPATH="$PWD/$(basename "$1")"
    echo "$REALPATH"

    cd $CURR
}

FEDORA_HOME=`dirname $(realpath "$0")`

# before enabling selinux in k8s, you should install docker, k8s first
sudo sed -i 's/SELINUX=permissive/SELINUX=enforcing/g' /etc/selinux/config
sudo setenforce 1
sudo setsebool container_manage_cgroup 1

# add { "selinux-enabled": true } to /etc/docker/daemon.json
sudo cp $FEDORA_HOME/docker/daemon.json /etc/docker/daemon.json

sudo systemctl daemon-reload && sudo systemctl restart docker
sudo chmod 666 /var/run/docker.sock

# change contexts
sudo chcon -R -t svirt_sandbox_file_t /etc/kubernetes/
sudo chcon -R -t svirt_sandbox_file_t /var/lib/etcd
