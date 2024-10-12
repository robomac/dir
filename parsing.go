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

// Holds the command parsing of dir.

import (
	_ "embed"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gobwas/glob"
)

// Format-Print only if cond == true
func conditionalPrint(cond bool, format string, a ...any) {
	if cond {
		fmt.Printf(format, a...)
	}
}

// Choices are:
//    Default: current directory, all files, no filtering.
//    Passed value is a directory name - list all files in it.
//    Passed value is a file name or wildcard pattern - list matching files.
//	  Passed value has both.  i.e. the beginning is a directory to start in,
//  	 with a wildcard or filename at the end.  Has a slash + content.

func parseFileName(param string) {
	fileMask := param
	conditionalPrint((show_errors || debug_messages) && (len(start_directory) > 0 || filenameParsed),
		"  *** WARNING: Multiple filename parameters found.  Had %s %s, now %s.\nShould you quote to avoid globbing?\n",
		start_directory, file_mask, param)
	conditionalPrint(debug_messages, "Parsing file name %s\n", param)
	if strings.HasPrefix(param, "~") {
		home, _ := os.UserHomeDir()
		param = strings.Replace(param, "~", home, 1)
	}
	// Do we need to deal with a directory specification?
	if strings.Contains(param, "/") {
		// We have a start directory.  Do we have a file pattern?  See if this opens.
		d, err := os.Stat(param)
		if err == nil {
			if d.IsDir() {
				start_directory = param
				// No filemask.  Done
				conditionalPrint(debug_messages, "Parsed %s to directory, no file.\n", param)
				return
			}
		}
		// Try with just the end.
		dirPath := param[:strings.LastIndex(param, "/")]
		d, err = os.Stat(dirPath)
		if err == nil {
			if d.IsDir() {
				start_directory = dirPath
				fileMask = param[strings.LastIndex(param, "/")+1:]
			} else {
				extension := "," + dirPath[strings.LastIndex(dirPath, ".")+1:] + ","
				if strings.Contains(Extensions[ARCHIVE], extension) {
					// Flag this as the source file to be read.
					pathIsArchive = true
					start_directory = dirPath
					fileMask = param[strings.LastIndex(param, "/")+1:]
				}
			}
		}
	}
	// Is this actually a directory name itself?
	d, err := os.Stat(param)
	if err == nil && d.IsDir() {
		start_directory = param
		return
	}
	// We have a mask.  Build the globber
	file_mask = fileMask
	haveGlobber = true //	 We don't yet have it... we have to process all the parameters to see if case-sensitive first.
	filenameParsed = true
	conditionalPrint(debug_messages, "Parameter %s parsed to directory %s, file mask %s.\n", param, start_directory, file_mask)
}

func parseDateRange(v string) (time.Time, time.Time) {
	var err error
	dateRange := strings.Split(v, ":")
	if len(dateRange) == 0 {
		conditionalPrint(show_errors, "Invalid date range: %s\n", v)
		return mindate, maxdate
	}
	mindate, err = time.Parse("2006-01-02", dateRange[0])
	if err != nil {
		conditionalPrint(show_errors, "Invalid date range: %s - %s\n", v, err.Error())
	}
	if (len(dateRange) > 1) && (len(dateRange[1]) > 1) {
		maxdate, err = time.Parse("2006-01-02", dateRange[1])
		if err != nil {
			conditionalPrint(show_errors, "Invalid date range: %s - %s\n", v, err.Error())
		}
		maxdate = maxdate.Add((time.Hour * 24) - time.Duration(maxdate.Hour()))
	}
	return mindate, maxdate
}

func parseSizeRange(v string) {
	var err error
	sizeRange := strings.Split(v, ":")
	if len(sizeRange) == 0 {
		conditionalPrint(show_errors, "Invalid size range: %s\n", v)
		return
	}
	if len(sizeRange[0]) > 0 {
		minsize, err = strconv.ParseInt(sizeRange[0], 10, 64)
		if err != nil {
			conditionalPrint(show_errors, "Invalid size range: %s - %s\n", v, err.Error())
		}
	}
	if len(sizeRange) > 1 && len(sizeRange[1]) > 0 {
		maxsize, err = strconv.ParseInt(sizeRange[1], 10, 64)
		if err != nil {
			conditionalPrint(show_errors, "Invalid size range: %s - %s\n", v, err.Error())
		}
	}
}

func parseCmdLine() {
	var args = os.Args[1:] // 0 is program name
	// args is all strings that are space-separated.
	// The filename is the only thing that doesn't start with - or /
	for i, s := range args {
		conditionalPrint(debug_messages, "Processing argument %d: %s.\n", i, s)

		// Can't use / as flag separator if /Users, e.g., is valid
		// There are no one-or-two character / folders.  But check for /usr vs /o-n
		// So -*, /x, /xx and /x{+_}x are legal as parameters
		isParam := strings.HasPrefix(s, "-") || s == "/?" || s == "/help"
		if (!isParam) && (strings.HasPrefix(s, "/")) {
			if len(s) == 3 {
				isParam = true
			} else if (len(s) == 4) && (strings.Contains(s, "-") || strings.Contains(s, "+")) {
				isParam = true
			}
		}

		if isParam {
			// Linux apps often allow params to be combined on a line.  That could be
			// tricky for /on or other multi-character flags
			// sort: o{-}{ndstx} (t and x are both extension)
			// header: v+ or v-
			// Version 1 just handles them separate
			p := s[1:]
			values := ""
			if strings.Contains(p, "=") {
				pieces := strings.Split(p, "=")
				p = pieces[0]
				values = pieces[1]
			}
			switch p {
			case "?", "h", "help", "-help", "-h":
				fmt.Println(helptext)
				os.Exit(0)
			case "on":
				sortby = sortorder{SORT_NAME, true}
			case "o-n":
				sortby = sortorder{SORT_NAME, false}
			case "od":
				sortby = sortorder{SORT_DATE, true}
			case "o-d":
				sortby = sortorder{SORT_DATE, false}
			case "oc":
				sortby = sortorder{SORT_CREATED, true}
			case "o-c":
				sortby = sortorder{SORT_CREATED, false}
			case "oa":
				sortby = sortorder{SORT_ACCESSED, true}
			case "o-a":
				sortby = sortorder{SORT_ACCESSED, false}
			case "ox":
				sortby = sortorder{SORT_EXT, true}
			case "o-x":
				sortby = sortorder{SORT_EXT, false}
			case "ot":
				sortby = sortorder{SORT_TYPE, true}
			case "o-t":
				sortby = sortorder{SORT_TYPE, false}
			case "os":
				sortby = sortorder{SORT_SIZE, true}
			case "o-s":
				sortby = sortorder{SORT_SIZE, false}
			case "ah-":
				listhidden = false
			case "cs":
				case_sensitive = true
			case "b+":
				bare = true
				include_path = true
				size_calculations = false
				directory_header = false
			case "b":
				bare = true
				size_calculations = false
				directory_header = false
				include_path = false
			case "c": // Change column definition for output
				columnDef = values
			case "d+":
				listfiles = false
				listdirectories = true
			case "d-":
				listdirectories = false
			case "debug":
				debug_messages = true
			case "error", "errors":
				show_errors = true
			case "G-":
				use_colors = false
			case "G":
				use_colors = true
				use_enhanced_colors = false
			case "G+":
				use_colors = true
				use_enhanced_colors = true
			case "ma": // Accessed Date
				parseDateRange(values)
				minmaxdatetype = "a"
			case "mc": // Created Date
				parseDateRange(values)
				minmaxdatetype = "c"
			case "md": // Parse dates, compare to Time.IsZero()
				parseDateRange(values)
				minmaxdatetype = "m"
			case "ms": // Parse sizes
				parseSizeRange(values)
			case "r":
				recurse_directories = true
			case "sc": // Use commas (local sep) in file sizes
				filesizes_format = SIZE_SEPARATOR
			case "sh": // Use GB,TB, etc. in file sizes
				filesizes_format = SIZE_QUANTA
			case "sr": // Standard default sizes - bytes with no formatting
				filesizes_format = SIZE_NATURAL
			case "t":
				listfiles = false
			case "tc": // Case-sensitive search
				text_search_type = SEARCH_CASE
				text_regex = regexp.MustCompile(values)
			case "ti": // Case-insensitive search
				text_search_type = SEARCH_NOCASE
				text_regex = regexp.MustCompile("(?i)" + values)
			case "tr": // Regex search
				text_search_type = SEARCH_REGEX
				text_regex = regexp.MustCompile(values)
			case "version", "v":
				fmt.Println(versionDate)
				os.Exit(0)
			case "exclude", "x":
				exclude_exts = strings.Split(strings.ToUpper(values), ",")
			case "z":
				listInArchives = true
			}
		} else {
			parseFileName(s)
		}
	}
	if haveGlobber {
		mask := file_mask
		if !case_sensitive {
			mask = strings.ToUpper(mask)
		}
		matcher = glob.MustCompile(mask)
	}
}
