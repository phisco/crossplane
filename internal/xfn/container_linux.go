//go:build linux

/*
Copyright 2022 The Crossplane Authors.

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

package xfn

import (
	"kernel.org/pub/linux/libs/security/libcap/cap"
)

// / HasCapSetUID returns true if this process has CAP_SETUID.
func HasCapSetUID() bool {
	pc := cap.GetProc()
	setuid, _ := pc.GetFlag(cap.Effective, cap.SETUID)
	return setuid
}

// HasCapSetGID returns true if this process has CAP_SETGID.
func HasCapSetGID() bool {
	pc := cap.GetProc()
	setgid, _ := pc.GetFlag(cap.Effective, cap.SETGID)
	return setgid
}
