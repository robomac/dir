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
Allow defining type sort order.  Change current order, which has archives first.
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
	SORT_NAME    sortfield = "n"
	SORT_DATE    sortfield = "d" // Sort by last modified.
	SORT_SIZE    sortfield = "s"
	SORT_TYPE    sortfield = "e" // Uses mod and knowledge of extensions to group, e.g. image, archive, code, document
	SORT_EXT     sortfield = "x" // Extension in DOS
	SORT_NATURAL sortfield = "o" // Don't sort
)

type Filetype int

const ( // Filetypes
	NONE  Filetype = iota // starts at 0, also used for reset
	AUDIO                 // 1 ...
	ARCHIVE
	IMAGE
	DOCUMENT // Enhanced start here
	DATA
	CONFIG
	CODE
	DIRECTORY // No extensions
	EXECUTABLE
	SYMLINK // No extensions
	HIDDEN  // Prefix, not suffix.  Matches DEFAULT unless set otherwise.  Last so other types override on colors.
	DEFAULT
)

func (ft Filetype) String() string {
	return [...]string{"None", "Audio", "Archive", "Image/Video", "Document", "Data", "Configuration", "Source Code", "Directory", "Executable", "SymLink", "Hidden", "Default"}[ft]
}

// Notes: See https://docs.fileformat.com for a great list.  Some are value judgements.
var Extensions = map[Filetype]string{
	AUDIO:   ",aac,au,flac,mid,midi,mka,mp3,mpc,ogg,ra,wav,axa,oga,spx,xspf,",
	ARCHIVE: ",7z,ace,apk,arj,bz,bz2,cpio,deb,dmg,dz,gz,jar,lz,lzh,lzma,rar,rpm,rz,tar,taz,tbz,tbz2,tgz,tlz,txz,tz,xz,z,Z,zip,zoo,",
	IMAGE:   ",anx,asf,avi,axv,bmp,cgm,dib,dl,emf,flc,fli,flv,gif,gl,jpeg,jpg,m2v,m4v,mkv,mng,mov,mp4,mp4v,mpeg,mpg,nuv,ogm,ogv,ogx,pbm,pcx,pgm,png,ppm,qt,rm,rmvb,svg,svgz,tga,tif,tiff,vob,wmv,xbm,xcf,xpm,xwd,yuv,",
	// The following are "Enhanced" options.
	DOCUMENT: ",doc,docx,ebk,epub,html,htm,markdown,mbox,mbp,md,mobi,msg,odt,ofx,one,pdf,ppt,pptx,ps,pub,tex,txt,xls,xlsx,",
	DATA:     ",cdb,csv,dat,db3,dbf,graphql,json,log,rpt,sdf,sql,xml,",
	CONFIG:   ",adp,ant,cfg,confit,ini,prefs,rc,tcl,yaml,",
	CODE:     ",ahk,applescript,asm,au3,bas,bash,bat,c,cmake,cmd,coffee,cpp,cs,cxx,dockerfile,elf,es,exe,go,gradle,groovy,gvy,h,hpp,hxx,inc,ino,java,js,kt,ktm,kts,lua,m,mak,mm,perl,ph,php,pl,pp,ps1,psm1,py,rake,rb,rbw,rbuild,rbx,rs,ru,ruby,scpt,sh,ts,tsx,v,vb,vbs,vhd,vhdl,zsh,",
}

// Could use a slice here, since it's indexing in by int, but naming the spots makes it clearer.
var FileTypeSortOrder = map[Filetype]int{DIRECTORY: 0, HIDDEN: 1, NONE: 2, DEFAULT: 3, CODE: 4, EXECUTABLE: 5, CONFIG: 6,
	DATA: 7, DOCUMENT: 8, AUDIO: 9, IMAGE: 10, ARCHIVE: 11}

// By convention, but not typically part of LS_COLORS, archives are bold red, audio is cyan, media and some others are bold magenta.
// Colors that get mapped to extensions.
// 00=none, 01=bold, 04=underscore, 05=blink, 07=reverse, 08=concealed.
// FG: 30=black, 31=red, 32=green, 33=yellow, 34=blue, 35=magenta, 36=cyan, 37=white,
// BG: 40=black 41=red 42=green 43=yellow 44=blue 45=magenta 46=cyan 47=white
var FileColors = map[Filetype]string{
	NONE: "0", DIRECTORY: "1;36", DEFAULT: "37",
	EXECUTABLE: "31", SYMLINK: "35", ARCHIVE: "01;31", IMAGE: "01;35", AUDIO: "00;36",
	// Extensions
	DOCUMENT: "01;32", DATA: "32", CONFIG: "01;37", CODE: "01;34",
}

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
	_ft      Filetype // Holds the filetype once initialized.  Use .FileType() instead.
}

// Returns the extension based file type, or DIR/SYMLINK/EXE if appropriate.
// The rest of the fileitem should already be filled in.
func (f *fileitem) FileType() Filetype {
	if f._ft != NONE {
		return f._ft
	}
	if f.Isdir {
		f._ft = DIRECTORY
	} else if f.mode&0111 != 0 { // i.e. any executable bit set
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

// Does this file meet current conditions for inclusion?
func fileMeetsConditions(target fs.DirEntry) bool {
	if (!listdirectories) && target.IsDir() {
		return false
	}
	if (!listfiles) && !target.IsDir() {
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
	return ternaryString(lastdot <= 1, "", strings.ToUpper(f.Name[lastdot+1:]))
}

func (f fileitem) ModeToString() string {
	// Three sets - owner, group, default.
	var rwx strings.Builder
	rwx.WriteString(ternaryString(f.Isdir, "d", ternaryString(len(f.LinkDest) > 0, "l", "-")))
	for i := 2; i >= 0; i-- {
		bits := f.mode >> (i * 3)
		rwx.WriteString(ternaryString(bits&4 != 0, "r", "-"))
		rwx.WriteString(ternaryString(bits&2 != 0, "w", "-"))
		if i == 0 && f.mode&os.ModeSticky != 0 {
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
	return fmt.Sprintf("%s%s   %s %14d   %s%s%s", colorstr, f.ModeToString(), f.Modified.Format("2006-01-02 15:04:05"), f.Size, name, linktext, colorreset)
}

func makefileitem(de fs.DirEntry, path string) fileitem {
	var item fileitem
	link, _ := os.Readlink(filepath.Join(path, de.Name()))
	i, e := de.Info()
	if e == nil {
		item = fileitem{path, i.Name(), i.Size(), i.ModTime(), i.IsDir(), i.Mode(), link, NONE}
	}
	return item
}

/******* HANDLING COLORS *******/
/* General description of the LS_COLORS format:  It is a two-letter index and up to three digits separated by semicolons.
   Style;foreground color; background color.  They occupy different numeric spaces.
   Style: 00=none, 01=bold, 04=underscore, 05=blink, 07=reverse, 08=concealed.
   Color: 30=black, 31=red, 32=green, 33=yellow, 34=blue, 35=magenta, 36=cyan, 37=white.
*/

func colorSetString(ftype Filetype) string {
	if len(FileColors[ftype]) == 0 {
		ftype = DEFAULT
	}
	return fmt.Sprintf("\033[%sm", FileColors[ftype])
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
		var ft Filetype
		switch components[0] {
		case "ac":
			ft = ARCHIVE
		case "au":
			ft = AUDIO
		case "di":
			ft = DIRECTORY
		case "ex":
			ft = EXECUTABLE
		case "fi":
			ft = DEFAULT
		case "im":
			ft = IMAGE
		case "ln":
			ft = SYMLINK
		}
		if ft != NONE { // i.e. it was set; we don't change "reset"
			FileColors[ft] = components[1]
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
			firstName := ternaryString(case_sensitive, first.Name, strings.ToUpper(first.Name))
			secondName := ternaryString(case_sensitive, second.Name, strings.ToUpper(second.Name))
			if !sortby.ascending {
				first = matchedFiles[j]
				second = matchedFiles[i]
			}
			if (directories_first) && (first.Isdir != second.Isdir) {
				return first.Isdir
			}
			switch sortby.field {
			case SORT_NAME:
				return firstName < secondName
			case SORT_DATE:
				return first.Modified.Before(second.Modified)
			case SORT_SIZE:
				return first.Size < second.Size
			case SORT_TYPE:
				if first.FileType() != second.FileType() {
					return FileTypeSortOrder[first.FileType()] < FileTypeSortOrder[second.FileType()]
				}
				if first.Extension() != second.Extension() {
					return first.Extension() < second.Extension()
				}
				return firstName < secondName
			case SORT_EXT:
				if first.Extension() == second.Extension() {
					return firstName < secondName
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
	if listfiles || listdirectories {
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
			case "d-":
				listdirectories = false
			case "d+":
				listfiles = false
				listdirectories = true
			case "ah-":
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
	if debug_messages {
		for c := NONE; c <= DEFAULT; c++ {
			fmt.Printf("Color for %16s is %s\n", c.String(), FileColors[c])
		}
	}

	if len(start_directory) == 0 || start_directory == "." {
		start_directory, _ = os.Getwd()
	}
	list_directory(start_directory, false)
}
