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

// Package importer implements the import subcommand and all its provider-specific subcommands.
package importer

// Cmd arguments and flags for import subcommand.
type Cmd struct {
	AWS   awsCmd   `cmd:"" help:"Import AWS resources."`
	Azure azureCmd `cmd:"" help:"Import Azure resources."`
	GCP   gcpCmd   `cmd:"" help:"Import GCP resources."`
}

// Flags contains the flags to be embedded by all the provider-specific subcommands.
type Flags struct {
	Output    string   `help:"Output path for imported resources' YAML manifests." short:"o" type:"path" default:"-"`
	Resources []string `help:"Comma-separated list of cloud resources to import." short:"r" required:""`
}
