/*
Copyright 2023, RoboMac

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

package main

import (
	"io/fs"
	"syscall"
	"time"
)

func createdAndAccessed(fi fs.FileInfo) (time.Time, time.Time) {
	var createdTime syscall.Filetime = fi.Sys().(*syscall.Win32FileAttributeData).CreationTime
	var accessTime syscall.Filetime = fi.Sys().(*syscall.Win32FileAttributeData).LastAccessTime
	return time.Unix(0, createdTime.Nanoseconds()), time.Unix(0, accessTime.Nanoseconds())
}
