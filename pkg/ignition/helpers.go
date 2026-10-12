/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package ignition provides shared Ignition v3.1.0 helper functions used by
// both the bootstrap and controlplane providers.
package ignition

import (
	"encoding/base64"

	config_types "github.com/coreos/ignition/v2/config/v3_1/types"
)

// Base64Encode encodes a string as base64.
func Base64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// DataURL wraps content as a base64-encoded data: URL for use in Ignition file sources.
func DataURL(content string) string {
	return "data:text/plain;base64," + Base64Encode(content)
}

// DataURLCharset wraps content as a base64-encoded data: URL with charset=utf-8.
func DataURLCharset(content string) string {
	return "data:text/plain;charset=utf-8;base64," + Base64Encode(content)
}

// CreateFile builds an Ignition file entry with the given path, user, source content, mode, and overwrite flag.
func CreateFile(path, user, content string, mode int, overwrite bool) config_types.File {
	return config_types.File{
		Node: config_types.Node{
			Path:      path,
			Overwrite: &overwrite,
			User:      config_types.NodeUser{Name: &user},
		},
		FileEmbedded1: config_types.FileEmbedded1{
			Append: []config_types.Resource{},
			Contents: config_types.Resource{
				Source: &content,
			},
			Mode: &mode,
		},
	}
}

// CreateUnit builds an enabled Ignition systemd unit.
func CreateUnit(name, contents string) config_types.Unit {
	enabled := true
	return config_types.Unit{
		Contents: &contents,
		Enabled:  &enabled,
		Name:     name,
	}
}
