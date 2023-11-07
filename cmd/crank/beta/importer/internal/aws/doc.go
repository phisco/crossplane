/*
Copyright 2023 The Crossplane Authors.

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

// Package aws contains functionality to import AWS resources.
package aws

import (
	"embed"
	"text/template"
)

//go:embed templates
var fs embed.FS

// GetTemplates get the templates for AWS resources.
func GetTemplates() (*template.Template, error) {
	return template.ParseFS(fs, "templates/*")
}
