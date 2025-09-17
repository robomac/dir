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
// Note: To sort by date-added on Mac, can use mdls to fill those values.
// This is not portable. It prints/outputs file metadata.
// stat -f "%Sc" <filename> shows "Change" which seems to match. B is "birth" and "m" is modify, both seem to show local change times.
// stat -f "Access (atime): %Sa%nModify (mtime): %Sm%nChange (ctime): %Sc%nBirth  (Btime): %SB" file.txt
// Change is change of metadata. Modified is change of file contents.  B - birth is supposed to be file creation time on the file system, AKA crtime or btime.
// ext4 fs equivalent: https://moiseevigor.github.io/software/2015/01/30/get-file-creation-time-on-linux-with-ext4/
// Windows also has create time. From dir /?
/*   /T          Controls which time field displayed or used for sorting
timefield   C  Creation
            A  Last Access
            W  Last Written
*/
// Use a flag for determining whether to grab created - i.e. if displaying, filtering or sorting on it.
// Need to determine OS to find mechanism for it - Windows vs Mac vs Linux perhaps.
// In Help, point out that it is slower.
/*
https://pkg.go.dev/os#Stat

stat (and others) return fileinfo which is fs.FileInfo, which includes a Sys() method for the underlying data source.
e.g. go includes a test function for Darwin -
func atime(fi FileInfo) time.Time {
	return time.Unix(fi.Sys().(*syscall.Stat_t).Atimespec.Unix())
}
The names may have changed, Stat_t type.
See https://go.dev/src/syscall/ ztypes_*.go


import (
	"fmt"
	"os"
	"syscall"
	"time"
)

func main() {
	f, _ := os.Stat("test")
	t := f.Sys().(*syscall.Stat_t).Birthtimespec
	d := time.Unix(t.Sec, t.Nsec)
	fmt.Println(d)
}
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
	Created   time.Time // If supported by the OS, this is when added. Otherwise 0 (time.Time{})
	Accessed  time.Time // If supported by the OS, this is when added. Otherwise 0 (time.Time{})
	IsDir     bool
	Mode      fs.FileMode
	LinkDest  string
	InArchive bool
	_ft       Filetype // Holds the filetype once initialized.  Use .FileType() instead.
	FoundText string   // Only if doing a text search.
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

func FileSizeLen(format sizeformat) int {
	switch format {
	case SIZE_QUANTA:
		return 7
	case SIZE_SEPARATOR:
		return 17
	default:
		return 14
	}
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

// The settings for this are global, in dir.go.
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
	createdTime := ""
	if !f.Created.IsZero() {
		createdTime = f.Created.Format("  (2006-01-02 15:04:05)")
	}
	return fmt.Sprintf("%s%s   %s%s  %s   %s%s%s", colorstr, f.ModeToString(), f.Modified.Format("2006-01-02 15:04:05"), createdTime, f.FileSizeToString(), name, linktext, colorreset)
}

// Set off of the columns map
func (f fileitem) BuildOutput() string {
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
	modifiedTime := f.Modified.Format("2006-01-02 15:04:05")
	accessedTime := ""
	if !f.Accessed.IsZero() {
		accessedTime = f.Accessed.Format("2006-01-02 15:04:05")
	}
	createdTime := ""
	if !f.Created.IsZero() {
		createdTime = f.Created.Format("2006-01-02 15:04:05")
	}
	outputString := colorstr
	for i := 0; i < len(columnDef); i++ { //run a loop and iterate through each character
		switch string(columnDef[i]) {
		case COLUMN_DATEMODIFIED:
			outputString += modifiedTime
		case COLUMN_DATECREATED:
			outputString += createdTime
		case COLUMN_DATEACCESSED:
			outputString += accessedTime
		case COLUMN_FILESIZE:
			outputString += f.FileSizeToString()
		case COLUMN_MODE:
			outputString += f.ModeToString()
		case COLUMN_NAME:
			if namePadding > 0 {
				outputString += fmt.Sprintf("%-*s", namePadding, name)
			} else {
				outputString += name
			}
		case COLUMN_LINK:
			outputString += linktext
		case COLUMN_PATH:
			if namePadding > 0 {
				outputString += fmt.Sprintf("%-*s", namePadding, f.Path)
			} else {
				outputString += f.Path
			}
		default:
			outputString += string(columnDef[i])
		}
	}
	outputString += colorreset
	if listFoundText && len(f.FoundText) > 0 {
		outputString += "\n" + f.FoundText + "\n"
	}
	return outputString
}

func makefileitem(de fs.DirEntry, path string) fileitem {
	var item fileitem
	link, _ := os.Readlink(filepath.Join(path, de.Name()))
	fi, e := de.Info()
	if e == nil {
		item = fileitem{path, fi.Name(), fi.Size(), fi.ModTime(), time.Time{}, time.Time{}, fi.IsDir(), fi.Mode(), link, false, NONE, ""}
		// Only do this on supported system. https://go.dev/doc/install/source#environment  $GOOS == android, darwin, dragonfly, freebsd, illumos, ios, js, linux, netbsd, openbsd, plan9, solaris, wasip1, and windows.
		// If checking for create time, try to fill in here.
		// Possible elements: Birthtimespec,
		item.Created, item.Accessed = createdAndAccessed(fi)
	}
	return item
}
