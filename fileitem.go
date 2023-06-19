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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Our basic list unit.
type fileitem struct {
	Path      string // Path to file, not including name
	Name      string // Name including any extention
	Size      int64
	Modified  time.Time
	IsDir     bool
	Mode      fs.FileMode
	LinkDest  string
	InArchive bool
	_ft       Filetype // Holds the filetype once initialized.  Use .FileType() instead.
}

// BSD often has executable archives.  Weird concept, throws the basics off.
// So we need more granularity.
func (f fileitem) IsArchive() bool {
	return strings.Contains(Extensions[ARCHIVE], ","+strings.ToLower(f.Extension()+","))
}

// Returns the extension based file type, or DIR/SYMLINK/EXE if appropriate.
// The rest of the fileitem should already be filled in.
func (f *fileitem) FileType() Filetype {
	if f._ft != NONE {
		return f._ft
	}
	if f.IsDir {
		f._ft = DIRECTORY
	} else if f.Mode&0111 != 0 { // i.e. any executable bit set
		f._ft = EXECUTABLE
	} else {
		for ft := AUDIO; ft <= CODE; ft++ {
			if strings.Contains(Extensions[ft], ","+strings.ToLower(f.Extension()+",")) {
				f._ft = ft
				break
			}
		}
	}
	// Hidden comes last, because it's less important than others for colors.
	if f._ft == NONE && f.Name[0] == '.' {
		f._ft = HIDDEN
	}
	if f._ft == NONE { // If not set yet, at least we tried
		f._ft = DEFAULT
	}
	return f._ft
}

// Returns an upper-case version of the file extension (part after last dot), if any.
func (f fileitem) Extension() string {
	lastdot := strings.LastIndex(f.Name, ".")
	return ternaryString(lastdot <= 1, "", strings.ToUpper(f.Name[lastdot+1:]))
}

func FileSizeToString(fSize int64) string {
	switch filesizes_format {
	case SIZE_QUANTA:
		// Determine quanta first.
		if fSize > 1024*1024*1024 {
			return fmt.Sprintf("%6.2fG", float64(fSize)/1073741824)
		} else if fSize > 1024*1024 {
			return fmt.Sprintf("%6.2fM", float64(fSize)/(1024*1024))
		} else if fSize > 1024 {
			return fmt.Sprintf("%6.2fK", float64(fSize)/(1024))
		}
		return fmt.Sprintf("%7d", fSize)
	case SIZE_SEPARATOR:
		// Insert sep every three digits.
		bytesStr := fmt.Sprintf("%d", fSize)
		curPos := 3
		if len(bytesStr) > curPos {
			bytesStr = bytesStr[:len(bytesStr)-curPos] + "," + bytesStr[len(bytesStr)-curPos:]
		}
		curPos = 7
		if len(bytesStr) > curPos {
			bytesStr = bytesStr[:len(bytesStr)-curPos] + "," + bytesStr[len(bytesStr)-curPos:]
		}
		curPos = 11
		if len(bytesStr) > curPos {
			bytesStr = bytesStr[:len(bytesStr)-curPos] + "," + bytesStr[len(bytesStr)-curPos:]
		}
		return fmt.Sprintf("%17s", bytesStr)
	default: // Includes SIZE_NATURAL
		return fmt.Sprintf("%14d", fSize)
	}
}

func (f fileitem) FileSizeToString() string {
	return FileSizeToString(f.Size)
}

func (f fileitem) ModeToString() string {
	// Three sets - owner, group, default.
	var rwx strings.Builder
	rwx.WriteString(ternaryString(f.IsDir, "d", ternaryString(len(f.LinkDest) > 0, "l", "-")))
	for i := 2; i >= 0; i-- {
		bits := f.Mode >> (i * 3)
		rwx.WriteString(ternaryString(bits&4 != 0, "r", "-"))
		rwx.WriteString(ternaryString(bits&2 != 0, "w", "-"))
		if i == 0 && f.Mode&os.ModeSticky != 0 {
			rwx.WriteString(ternaryString(bits&1 != 0, "t", "T"))
		} else {
			rwx.WriteString(ternaryString(bits&1 != 0, "x", "-"))
		}
	}
	return rwx.String()
}

func (f fileitem) ToString() string {
	name := f.Name
	if include_path {
		name = filepath.Join(f.Path, f.Name)
	}
	if bare {
		return name
	}
	colorstr := ""
	colorreset := ""
	linktext := ternaryString(len(f.LinkDest) > 0, "-> "+f.LinkDest, "")

	if use_colors {
		colorstr = colorSetString(f.FileType())
		if !use_enhanced_colors && f.FileType() >= DOCUMENT && f.FileType() < DIRECTORY {
			colorstr = colorSetString(DEFAULT) // Because not enhanced.
		}
		colorreset = colorSetString(NONE)
	}
	return fmt.Sprintf("%s%s   %s  %s   %s%s%s", colorstr, f.ModeToString(), f.Modified.Format("2006-01-02 15:04:05"), f.FileSizeToString(), name, linktext, colorreset)
}

func makefileitem(de fs.DirEntry, path string) fileitem {
	var item fileitem
	link, _ := os.Readlink(filepath.Join(path, de.Name()))
	i, e := de.Info()
	if e == nil {
		item = fileitem{path, i.Name(), i.Size(), i.ModTime(), i.IsDir(), i.Mode(), link, false, NONE}
	}
	return item
}
