#!/bin/bash

env_file=/etc/metadata_env
config_dir=$(mktemp -d)
sudo mount -L config-2 $config_dir
cat $config_dir/openstack/latest/meta_data.json | jq -r '. | keys[]' | while read key; do value=$(jq -r ".[\"$key\"]" $config_dir/openstack/latest/meta_data.json); echo "METADATA_$(echo ${key} | tr a-z A-Z | tr - _)=${value}"; done | sort | uniq | sudo tee $env_file
