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

// Package printer contains the definition of the Printer interface and the
// implementation of all the available printers implementing it.
package printer

import (
	"io"

	"github.com/pkg/errors"

	"github.com/crossplane/crossplane/cmd/crank/describe/internal/resource"
)

const (
	errFmtUnknownPrinterType = "unknown printer output type: %s"
)

// Type represents the type of printer.
type Type string

// Implemented PrinterTypes
const (
	TypeDefault Type = "default"
	TypeJSON    Type = "json"
)

// Printer implements the interface which is used by all printers in this package.
type Printer interface {
	Print(io.Writer, *resource.Resource) error
}

// New creates a new printer based on the specified type.
func New(typeStr string) (Printer, error) {
	var p Printer

	switch Type(typeStr) {
	case TypeDefault:
		p = &DefaultPrinter{
			Indent: "  ",
		}
	case TypeJSON:
		p = &JSONPrinter{}
	default:
		return nil, errors.Errorf(errFmtUnknownPrinterType, typeStr)
	}

	return p, nil
}
