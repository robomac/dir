package main

import (
	_ "embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gobwas/glob"
)

/*
Enhancements to make:
Size - if more than 6 digits, take to 5+{KB|MB|GB}.  3.2 format.  Override with parameter.
*/

// DO NOT DELETE THIS "COMMENT"; it includes the file.
//
//go:embed dirhelp.txt
var helptext string

const versionDate = "2023-06-11"

type sortfield string
type sortorder struct {
	field     sortfield
	ascending bool
}
type Attributes string
type InclusionMod string

const (
	SORT_NAME    sortfield    = "n"
	SORT_DATE    sortfield    = "d" // Sort by last modified.
	SORT_SIZE    sortfield    = "s"
	SORT_TYPE    sortfield    = "e" // Extension in DOS
	SORT_NATURAL sortfield    = "o" // Don't sort
	DIR          Attributes   = "D"
	HIDDEN       Attributes   = "H"
	READONLY     Attributes   = "R"
	INCLUDE      InclusionMod = "+"
	EXCLUDE      InclusionMod = "-"
)

const (
	// By convension, but not typically part of LS_COLORS, archives are bold red, audio is cyan, media and some others are bold magenta.
	audioExtensions = ",aac,au,flac,mid,midi,mka,mp3,mpc,ogg,ra,wav,axa,oga,spx,xspf,"
	// dmg is clearly an archive, so added here.
	archiveExtensions = ",dmg,tar,tgz,arj,taz,lzh,lzma,tlz,txz,zip,z,Z,dz,gz,lz,xz,bz2,bz,tbz,tbz2,tz,deb,rpm,jar,rar,ace,zoo,cpio,7z,rz,"
	imageExtensions   = ",jpg,jpeg,gif,bmp,pbm,pgm,ppm,tga,xbm,xpm,tif,tiff,png,svg,svgz,mng,pcx,mov,mpg,mpeg,m2v,mkv,ogm,mp4,m4v,mp4v,vob,qt,nuv,wmv,asf,rm,rmvb,flc,avi,fli,flv,gl,dl,xcf,xwd,yuv,cgm,emf,axv,anx,ogv,ogx,"
)

var ( // Color settings.
	// For reference: red 31, green 32, yellow 33, blue 34, ...
	color_reset      = "0"
	color_dir        = "1;36"
	color_file       = "37"
	color_executable = "31"
	color_symlink    = "35"
	color_archives   = "01;31"
	color_images     = "01;35"
	color_audio      = "00;36"
)
var ( // Runtime configuration
	show_errors              = false
	debug_messages           = false
	bare                bool = false // Only print filenames
	include_path             = false // Turn on in bare+ mode
	sortby                   = sortorder{SORT_NAME, true}
	directories_first        = true
	listdirectories     bool = true
	listfiles           bool = true
	listhidden          bool = true
	directory_header    bool = true
	size_calculations   bool = true
	recurse_directories bool = false
	matcher             glob.Glob
	start_directory     string
	file_mask           string
	filenameParsed      bool = false
	haveGlobber              = false
	case_sensitive      bool = false
	use_colors          bool = false
	use_enhanced_colors bool = true // only applies if use_colors is on.
)

func ternaryString(condition bool, s1 string, s2 string) string {
	if condition {
		return s1
	}
	return s2
}

/******* File structure and file operations *******/

// Our basic list unit.
type fileitem struct {
	Path     string // Path to file, not including name
	Name     string // Name including any extention
	Size     int64
	Modified time.Time
	Isdir    bool
	mode     fs.FileMode
	LinkDest string
}

// Does this file meet current conditions for inclusion?
func fileMeetsConditions(target fs.DirEntry) bool {
	if (!listdirectories) && target.IsDir() {
		return false
	}
	filename := target.Name()
	if (!listhidden) && filename[0] == '.' {
		return false
	}
	// If we don't have the globber, return true.  Otherwise match it.
	if haveGlobber {
		if !case_sensitive {
			// The glob pattern should already have been upper-cased.
			return matcher.Match(strings.ToUpper(filename))
		}
		return matcher.Match(filename)
	}
	return true
}

func (f fileitem) Extension() string {
	lastdot := strings.LastIndex(f.Name, ".")
	if lastdot <= 1 {
		return ""
	}
	return strings.ToUpper(f.Name[lastdot+1:])
}

func (f fileitem) ModeToString() string {
	// Three sets - owner, group, default.
	var rwx strings.Builder
	if f.Isdir {
		rwx.WriteString("d")
	} else if len(f.LinkDest) > 0 {
		rwx.WriteString("l")
	} else {
		rwx.WriteString("-")
	}
	for i := 2; i >= 0; i-- {
		bits := f.mode >> (i * 3)
		if bits&4 != 0 {
			rwx.WriteString("r")
		} else {
			rwx.WriteString("-")
		}
		if bits&2 != 0 {
			rwx.WriteString("w")
		} else {
			rwx.WriteString("-")
		}
		if i == 0 && f.mode&os.ModeSticky != 0 {
			if bits&1 != 0 {
				rwx.WriteString("t")
			} else {
				rwx.WriteString("T")
			}
		} else {
			if bits&1 != 0 {
				rwx.WriteString("x")
			} else {
				rwx.WriteString("-")
			}
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
		if f.Isdir {
			colorstr = colorSetString(color_dir)
		} else if len(f.LinkDest) > 1 {
			colorstr = colorSetString(color_symlink)
		} else if f.mode&0111 != 0 { // i.e. any executable bit set
			colorstr = colorSetString(color_executable)
		} else if use_enhanced_colors && strings.Contains(audioExtensions, ","+strings.ToLower(f.Extension()+",")) {
			colorstr = colorSetString(color_audio)
		} else if use_enhanced_colors && strings.Contains(imageExtensions, ","+strings.ToLower(f.Extension()+",")) {
			colorstr = colorSetString(color_images)
		} else if use_enhanced_colors && strings.Contains(archiveExtensions, ","+strings.ToLower(f.Extension()+",")) {
			colorstr = colorSetString(color_archives)
		}
		if len(colorstr) > 0 {
			colorreset = colorSetString(color_reset)
		}
	}
	return fmt.Sprintf("%s%s   %s %14d   %s%s%s", colorstr, f.ModeToString(), f.Modified.Format("2006-01-02 15:04:05"), f.Size, name, linktext, colorreset)
}

func makefileitem(de fs.DirEntry, path string) fileitem {
	var item fileitem
	link, _ := os.Readlink(filepath.Join(path, de.Name()))
	i, e := de.Info()
	if e == nil {
		item = fileitem{path, i.Name(), i.Size(), i.ModTime(), i.IsDir(), i.Mode(), link}
	}
	return item
}

/******* HANDLING COLORS *******/
/* General description of the LS_COLORS format:  It is a two-letter index and up to three digits separated by semicolons.
   Style;foreground color; background color.  They occupy different numeric spaces.
   Style: 00=none, 01=bold, 04=underscore, 05=blink, 07=reverse, 08=concealed.
   Color: 30=black, 31=red, 32=green, 33=yellow, 34=blue, 35=magenta, 36=cyan, 37=white.
*/

func colorSetString(colorstr string) string {
	return fmt.Sprintf("\033[%sm", colorstr)
}

// Read the LS_COLORS variable and turn into our settings for coloring.
func mapColors() {
	lscolors := os.Getenv("LS_COLORS")
	if len(lscolors) > 6 {
		use_colors = true
	}
	colorDirectives := strings.Split(lscolors, ":")
	for _, directive := range colorDirectives {
		components := strings.Split(directive, "=")
		if len(components) < 2 {
			continue
		}
		switch components[0] {
		case "ac":
			color_archives = components[1]
		case "au":
			color_audio = components[1]
		case "di":
			color_dir = components[1]
		case "ex":
			color_executable = components[1]
		case "fi":
			color_file = components[1]
		case "im":
			color_images = components[1]
		case "ln":
			color_symlink = components[1]
		}
	}
}

/******* Core Code *******/
// Recursive if necessary listing of files.
func list_directory(target string, recursed bool) (err error) {
	var files []fs.DirEntry   //	Matched files, to sort/format
	var subdirs []fs.DirEntry // Subdirectories to recurse through
	var matchedFiles []fileitem
	filecount := 0
	directorycount := 0
	var bytesfound int64

	conditionalPrint(debug_messages, "Analyzing directory %s\n", target)
	pFile, err := os.Open(target)
	if err == nil {
		defer pFile.Close()
		files, err = pFile.ReadDir(0)
	}
	// Iterate through all files, matching and then sort
	if err == nil {
		for _, f := range files {
			// TODO
			// Need to have both short name, for listing and sorting...
			// and full name, for the subdirs values so they can be navigated to.
			if fileMeetsConditions(f) {
				matchedFiles = append(matchedFiles, makefileitem(f, target))
				if f.IsDir() {
					directorycount++
				} else {
					filecount++
					i, e := f.Info()
					if e == nil {
						bytesfound += i.Size()
					}
				}
			}
			if f.IsDir() && listdirectories {
				// TODO: SHould this be the DirEntry or should it be target + os.sep + f.Name()?
				subdirs = append(subdirs, f)
			}
		}
		sort.Slice(matchedFiles, func(i, j int) bool {
			first := matchedFiles[i]
			second := matchedFiles[j]
			if !sortby.ascending {
				first = matchedFiles[j]
				second = matchedFiles[i]
			}
			if (directories_first) && (first.Isdir != second.Isdir) {
				return first.Isdir
			}
			switch sortby.field {
			case SORT_NAME:
				if case_sensitive {
					return first.Name < second.Name
				} else {
					return strings.ToUpper(first.Name) < strings.ToUpper(second.Name)
				}
			case SORT_DATE:
				return first.Modified.Before(second.Modified)
			case SORT_SIZE:
				return first.Size < second.Size
			case SORT_TYPE:
				if first.Extension() == second.Extension() {
					if case_sensitive {
						return first.Name < second.Name
					} else {
						return strings.ToUpper(first.Name) < strings.ToUpper(second.Name)
					}
				}
				return first.Extension() < second.Extension()
			}
			return first.Name < second.Name
		})
	}
	// Output results.  Don't print directory header or footer if no files in a recursed directory
	if (!recursed || len(matchedFiles) > 0) && directory_header {
		fmt.Printf("\n   Directory of %s\n", target)
		if listfiles {
			fmt.Printf("\n")
		}
	}
	if listfiles {
		for _, f := range matchedFiles {
			fmt.Println(f.ToString())
		}
	}
	if (!recursed || len(matchedFiles) > 0) && size_calculations {
		fmt.Printf("   %4d Files (%d bytes) and %4d Directories.\n", filecount, bytesfound, directorycount)
	}

	// Handle sub directories
	if recurse_directories {
		sort.Slice(subdirs, func(i, j int) bool { return subdirs[i].Name() < subdirs[j].Name() })
		for _, d := range subdirs {
			list_directory(filepath.Join(target, d.Name()), true)
		}
	}
	return err
}

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
			}
		}
	}
	// We have a mask.  Build the globber
	file_mask = fileMask
	haveGlobber = true //	 We don't yet have it... we have to process all the parameters to see if case-sensitive first.
	filenameParsed = true
	conditionalPrint(debug_messages, "Parameter %s parsed to directory %s, file mask %s.\n", param, start_directory, file_mask)
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
			case "ox", "ot":
				sortby = sortorder{SORT_TYPE, true}
			case "o-x", "o-t":
				sortby = sortorder{SORT_TYPE, false}
			case "os":
				sortby = sortorder{SORT_SIZE, true}
			case "o-s":
				sortby = sortorder{SORT_SIZE, false}
			case "d-":
				listdirectories = false
			case "d+":
				listfiles = false
			case "h-":
				listhidden = false
			case "r":
				recurse_directories = true
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
			case "t":
				listfiles = false
			case "G-":
				use_colors = false
			case "G":
				use_colors = true
				use_enhanced_colors = false
			case "G+":
				use_colors = true
				use_enhanced_colors = true
			case "debug":
				debug_messages = true
			case "error":
				show_errors = true
			case "version":
				fmt.Println(versionDate)
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

func main() {
	mapColors() // This must come before parseCmdLine(), to allow suppression.
	parseCmdLine()
	if len(start_directory) == 0 || start_directory == "." {
		start_directory, _ = os.Getwd()
	}
	list_directory(start_directory, false)
}
