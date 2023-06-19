package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/bodgit/sevenzip"
	"github.com/gobwas/glob"
)

/*
Failures:
Not searching inside archives for text if not -z
Not finding text in 7z.
Not going into all archives.

Enhancements to make:
Allow defining type sort order.  Change current order, which has archives first.

-p : ls -p adds a slash after directory names
-m in ls is like bare with commas instead of newlines.

Tried PDF file parsing.  Some (v.14?) worked, some (v1.7?) did not.  Parsers are bad.  Gave up.
Utilize https://www.xpdfreader.com/download.html pdftotext if present.

pdftotext integration
mdfind results integration: mdfind -onlyin <dir> -name <mask> query
other archive text search - build interface
look in office files

*/

// DO NOT DELETE THIS "COMMENT"; it includes the file.
//
//go:embed dirhelp.txt
var helptext string

const versionDate = "2023-06-19"

type sortfield string
type sortorder struct {
	field     sortfield
	ascending bool
}
type sizeformat int
type Attributes string
type InclusionMod string
type searchtype int

const (
	SORT_NAME      sortfield  = "n"
	SORT_DATE      sortfield  = "d" // Sort by last modified.
	SORT_SIZE      sortfield  = "s"
	SORT_TYPE      sortfield  = "e" // Uses mod and knowledge of extensions to group, e.g. image, archive, code, document
	SORT_EXT       sortfield  = "x" // Extension in DOS
	SORT_NATURAL   sortfield  = "o" // Don't sort
	SIZE_NATURAL   sizeformat = 0   // Sizes as unformatted bytes
	SIZE_SEPARATOR sizeformat = 1   // Sizes formatted with localconv non-monetary separator
	SIZE_QUANTA    sizeformat = 2   // Sizes formatted with units/quanta - e.g. GB, TB...
	SEARCH_NONE    searchtype = 0
	SEARCH_CASE    searchtype = 1
	SEARCH_NOCASE  searchtype = 2
	SEARCH_REGEX   searchtype = 3
)

const PROGRAM_NOT_FOUND = "program not found"

type ArchiveType int

const (
	ARCHIVE_NA = iota
	ARCHIVE_ZIP
	ARCHIVE_TGZ
	ARCHIVE_7Z
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
	AUDIO:   ",aac,au,flac,m3u8,mid,midi,mka,mp3,mpc,ogg,ra,wav,axa,oga,spx,xspf,",
	ARCHIVE: ",7z,ace,apk,arj,bz,bz2,cpio,deb,dmg,dz,gz,jar,lz,lzh,lzma,msi,rar,rpm,rz,tar,taz,tbz,tbz2,tgz,tlz,txz,tz,xz,z,Z,zip,zoo,",
	IMAGE:   ",anx,asf,avi,axv,bmp,cgm,dib,dl,emf,flc,fli,flv,gif,gl,jpeg,jpg,m2v,m4v,mkv,mng,mov,mp4,mp4v,mpeg,mpg,nuv,ogm,ogv,ogx,pbm,pcx,pdn,pgm,png,ppm,qt,rm,rmvb,svg,svgz,tga,tif,tiff,vob,wmv,xbm,xcf,xpm,xwd,yuv,",
	// The following are "Enhanced" options.
	DOCUMENT: ",doc,docx,ebk,epub,html,htm,markdown,mbox,mbp,md,mobi,msg,odt,ofx,one,pdf,ppt,pptx,ps,pub,tex,txt,vsdx,xls,xlsx,",
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
	listInArchives      bool = false
	listhidden          bool = true
	directory_header    bool = true // Print name of directory.  Usually with size_calculations
	pathIsArchive       bool = false
	size_calculations   bool = true // Print directory byte totals
	recurse_directories bool = false
	mindate             time.Time
	maxdate             time.Time
	minsize             int64 = -1
	maxsize             int64 = math.MaxInt64
	matcher             glob.Glob
	start_directory     string
	file_mask           string
	filenameParsed      bool       = false
	haveGlobber                    = false
	case_sensitive      bool       = false
	filesizes_format    sizeformat = SIZE_NATURAL
	use_colors          bool       = false
	use_enhanced_colors bool       = true // only applies if use_colors is on.
	text_search_type    searchtype = SEARCH_NONE
	text_regex          *regexp.Regexp
	PdftotextPath       string = "*" // Uninitialized
	TotalFiles          int
	TotalBytes          int64
)

func ternaryString(condition bool, s1 string, s2 string) string {
	if condition {
		return s1
	}
	return s2
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

// We only want to check for pdftotext once, only if doing text searches,
// and only if a PDF is found.  This runs in that case.
func resolveCommand(cmd string) string {
	// See if it's in the execution directory
	var path string
	var err error

	executablePath, err := os.Executable()
	if err == nil {
		path = filepath.Dir(executablePath)
	}
	path = filepath.Join(path, cmd)
	_, e := os.Stat(path)
	if e == nil {
		return path
	}
	if !errors.Is(err, os.ErrNotExist) {
		conditionalPrint(show_errors, "Found but could not open %s: %s\n", cmd, err.Error())
	}
	path, err = exec.LookPath(cmd)
	if err == nil {
		return path
	}
	return ""
}

// Does this file meet current conditions for inclusion?
func fileMeetsConditions(target fileitem) bool {
	if (!listdirectories) && target.IsDir {
		return false
	}
	if (!listfiles) && !target.IsDir {
		return false
	}

	filename := target.Name
	if (!listhidden) && filename[0] == '.' {
		return false
	}

	if !mindate.IsZero() && target.Modified.Before(mindate) {
		return false
	}
	if !maxdate.IsZero() && target.Modified.After(maxdate) {
		return false
	}
	if target.Size < minsize || target.Size > maxsize {
		return false
	}

	// If we don't have the globber, return true.  Otherwise match it.
	if haveGlobber {
		testString := ternaryString(case_sensitive, filename, strings.ToUpper(filename))
		if !matcher.Match(testString) {
			return false
		}
	}

	t_ext := target.Extension()
	if text_search_type != SEARCH_NONE {
		if target.InArchive {
			if !archiveFileTextSearch(target) {
				return false
			}
		} else if t_ext == "DOCX" || t_ext == "PPTX" || t_ext == "XLSX" || t_ext == "VSDX" {
			conditionalPrint(debug_messages, "Embedded Zip text search on %s.\n", target.Name)
			embeddedFiles, err := filesInZipArchive(filepath.Join(target.Path, target.Name), false)
			if err != nil {
				conditionalPrint(show_errors, "Could not unzip %s: %s\n", target.Name, err.Error())
				return false
			}
			found := false
			for _, f := range embeddedFiles.MatchedFiles {
				var data []byte
				data, err = extractZipFileBytes(f.Path, f.Name, 0, int(f.Size))
				found = text_regex.Match(data)
				if found {
					break
				}
			}
			if err != nil { // Try brute forcè
				found = diskFileTextSearch(target)
			}
			if !found {
				return false
			}
			// We want to fall through to brute-force on any error.  Error may be PROGRAM_NOT_FOUND
		} else if s, e := PDFText(filepath.Join(target.Path, target.Name), false); e == nil {
			if !text_regex.Match([]byte(s)) {
				return false
			}
		} else if !diskFileTextSearch(target) {
			return false
		}
	}

	return true
}

// Returns an error if not opened or no utility (pdftotext)
func PDFText(filepath string, ignoreExtension bool) (string, error) {
	// Due to limitations of Go, I'm doing a fitness check here.
	extension := strings.ToUpper(filepath[strings.LastIndex(filepath, ".")+1:])
	if !ignoreExtension && extension != "PDF" {
		return "", errors.New("not a pdf file")
	}

	// Have we already checked?
	if PdftotextPath == "" {
		return "", errors.New(PROGRAM_NOT_FOUND)
	}
	// Or do we need to initialize this value?
	if PdftotextPath == "*" {
		PdftotextPath = resolveCommand("pdftotext")
		if len(PdftotextPath) == 0 {
			conditionalPrint(debug_messages, "Could not find pdftotext.  PDF text will not be found.\n")
			return "", errors.New(PROGRAM_NOT_FOUND)
		}
	}
	// pdftotext uses - to send output to stdout.
	cmd := exec.Command(PdftotextPath, filepath, "-")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		log.Fatalf("cmd.Run() failed with %s\n", err)
	}
	if stderr.Len() > 0 {
		fmt.Printf("Got errors: %s", stderr.String())
	}
	return stdout.String(), err
}

// Load and search one file in the zip, with a maximum size.
func archiveFileTextSearch(target fileitem) bool {
	var data []byte
	var err error
	if target.Size > 1000000 {
		return false
	}
	switch FileIsArchiveType(target.Path) {
	case ARCHIVE_ZIP:
		data, err = extractZipFileBytes(target.Path, target.Name, 0, int(target.Size))
	case ARCHIVE_7Z:
		data, err = extract7ZFileBytes(target.Path, target.Name, 0, int(target.Size))
	case ARCHIVE_TGZ:
		data, err = extractTgzFileBytes(target.Path, target.Name, 0, int(target.Size))
	default:
		// No handler found.
		return false
	}
	if err != nil {
		return false
	}
	var t_ext string = target.Extension()
	if t_ext == "DOCX" || t_ext == "PPTX" || t_ext == "XLSX" || t_ext == "VSDX" || t_ext == "PDF" {
		// Write to a temp file so we can more easily uncompress the docx or run a util on the PDF
		var err error
		var pfile *os.File
		pfile, err = os.CreateTemp("", target.Name)
		if err == nil {
			pfilename := pfile.Name()
			pfile.Write(data)
			pfile.Close()
			defer os.Remove(pfilename)
			data = nil
			if t_ext == "PDF" {
				s, e := PDFText(pfile.Name(), true)
				if e == nil {
					return text_regex.Match([]byte(s))
				}
			} else { // Handle Office files - decompress and check
				embeddedFiles, err := filesInZipArchive(pfile.Name(), false)
				if err == nil {
					for _, f := range embeddedFiles.MatchedFiles {
						var data []byte
						data, err = extractZipFileBytes(f.Path, f.Name, 0, int(f.Size))
						if err == nil {
							if text_regex.Match(data) {
								return true
							}
						}
					}
				}
			}
		} // temp file creation success
	} // office or pdf file
	return text_regex.Match(data)
}

// Searches the file in chunks.
// Returns true if the file has the text.  False on error or not found.
func diskFileTextSearch(target fileitem) bool {
	found_text := false
	// Load file in blocks of 200KB for speed and memory.
	file, err := os.Open(filepath.Join(target.Path, target.Name))
	if err != nil {
		conditionalPrint(show_errors, "Could not open file for text search: %s - %s\n", target.Name, err.Error())
		return false
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	// Any "Go" purist who thought generics are a bad idea... would fail an interview at any productive company.
	// Min() and Max() should not be this hard.  I understand the philosophy, but those philosophers are idiots
	// who don't deserve paying jobs.
	chunkSize := 20000
	overlapSize := 400
	if chunkSize > int(target.Size) {
		chunkSize = int(target.Size)
		overlapSize = 0
	}

	searchBuffer := make([]byte, chunkSize+overlapSize)

	for !found_text {
		n, err := reader.Read(searchBuffer[overlapSize:])

		if err != nil && err.Error() != "EOF" {
			conditionalPrint(show_errors, "Could not open file for text search: %s - %s\n", target.Name, err.Error())
			return false
		}
		found_text = text_regex.Match(searchBuffer)

		// Check for EOF
		if (n < chunkSize) || n == int(target.Size) {
			break
		}
	}
	return found_text
}

type ListingSet struct {
	//	Matched files, to sort/format
	Subdirs        []string // Subdirectories to recurse through
	Archives       []string
	MatchedFiles   []fileitem
	Filecount      int
	Directorycount int
	Bytesfound     int64
}

func extractZipFileBytes(zippath string, filename string, offset int, length int) ([]byte, error) {
	var buffer = make([]byte, length)
	zipReader, err := zip.OpenReader(zippath)
	if err != nil {
		if show_errors {
			fmt.Printf("Error: Could not open %s.  %s\n", filename, err.Error())
		}
		return nil, err
	}
	defer zipReader.Close()

	for _, fileInZip := range zipReader.File {
		if fileInZip.Name != filename {
			continue
		}
		readCloser, err := fileInZip.Open()
		if err != nil {
			return nil, err
		}
		defer readCloser.Close()
		// Pseudo-seek - read buffer size until we get there.
		curPos := 0
		for curPos < offset {
			readAmount := length
			if readAmount+curPos > offset {
				readAmount = offset - curPos
				newBuf := make([]byte, readAmount)
				readCloser.Read(newBuf)
			} else {
				readCloser.Read(buffer)
			}
			curPos += length
		}
		// Pseudo-Seek done.  Uggah.
		readCloser.Read(buffer)
		break
	}
	return buffer, err
}

func extract7ZFileBytes(zippath string, filename string, offset int, length int) ([]byte, error) {
	zipReader, err := sevenzip.OpenReader(zippath)
	if err != nil {
		if show_errors {
			fmt.Printf("Error: Could not open %s.  %s\n", filename, err.Error())
		}
		return nil, err
	}
	var buffer = make([]byte, length)

	for _, fileInZip := range zipReader.File {
		if fileInZip.Name != filename {
			continue
		}
		readCloser, err := fileInZip.Open()
		if err != nil {
			return nil, err
		}
		defer readCloser.Close()
		// Pseudo-seek - read buffer size until we get there.
		curPos := 0
		for curPos < offset {
			readAmount := length
			if readAmount+curPos > offset {
				readAmount = offset - curPos
				newBuf := make([]byte, readAmount)
				readCloser.Read(newBuf)
			} else {
				readCloser.Read(buffer)
			}
			curPos += length
		}
		// Pseudo-Seek done.  Uggah.
		readCloser.Read(buffer)
		break
	}
	return buffer, err
}

func extractTgzFileBytes(zippath string, filename string, offset int, length int) ([]byte, error) {
	var gzReader *gzip.Reader
	var tarReader *tar.Reader
	var buffer = make([]byte, length)

	file, err := os.Open(zippath)
	if err == nil {
		defer file.Close()
		gzReader, err = gzip.NewReader(file)
	}
	if err == nil {
		defer gzReader.Close()
		tarReader = tar.NewReader(gzReader)
	}
	if err != nil {
		if show_errors {
			fmt.Printf("Error: Could not open %s.  %s\n", filename, err.Error())
		}
		return nil, err
	}

	// Locate file
	head, err := tarReader.Next()
	for head != nil && err == nil {
		if head.Name != filename {
			head, err = tarReader.Next()
			continue
		}
		break
	}
	// Seek to offset
	curPos := 0
	for curPos < offset {
		readAmount := length
		if readAmount+curPos > offset {
			readAmount = offset - curPos
			newBuf := make([]byte, readAmount)
			tarReader.Read(newBuf)
		} else {
			tarReader.Read(buffer)
		}
		curPos += length
	}
	// Pseudo-Seek done.  Uggah.  Read data
	tarReader.Read(buffer)
	return buffer, err
}

func FileIsArchiveType(filename string) ArchiveType {
	extension := strings.ToLower(filename[strings.LastIndex(filename, ".")+1:])
	if extension == "zip" {
		return ARCHIVE_ZIP
	} else if extension == "tgz" || extension == "gz" {
		return ARCHIVE_TGZ
	} else if extension == "7z" {
		return ARCHIVE_7Z
	}
	return ARCHIVE_NA
}

func filesInZipArchive(filename string, checkConditions bool) (ListingSet, error) {
	var ls ListingSet
	zipReader, err := zip.OpenReader(filename)
	if err != nil {
		if show_errors {
			fmt.Printf("Error: Could not open %s.  %s\n", filename, err.Error())
		}
		return ls, err
	}
	defer zipReader.Close()

	for _, fileInZip := range zipReader.File {
		var item fileitem = fileitem{filename, fileInZip.Name, int64(fileInZip.UncompressedSize64), fileInZip.ModTime(),
			fileInZip.FileInfo().IsDir(), fileInZip.Mode(), "", true, NONE}
		if !checkConditions || fileMeetsConditions(item) {
			ls.MatchedFiles = append(ls.MatchedFiles, item)
			if item.IsDir {
				ls.Directorycount++
			} else {
				ls.Filecount++
				ls.Bytesfound += item.Size
			}
		}
	}
	return ls, err
}

func filesIn7ZArchive(filename string) (ListingSet, error) {
	var ls ListingSet
	zipReader, err := sevenzip.OpenReader(filename)
	if err != nil {
		if show_errors {
			fmt.Printf("Error: Could not open %s.  %s\n", filename, err.Error())
		}
		return ls, err
	}
	defer zipReader.Close()

	for _, fileInZip := range zipReader.File {
		var item fileitem = fileitem{filename, fileInZip.Name, fileInZip.FileInfo().Size(),
			fileInZip.Modified, fileInZip.FileInfo().IsDir(), fileInZip.Mode(), "", true, NONE}
		if fileMeetsConditions(item) {
			ls.MatchedFiles = append(ls.MatchedFiles, item)
			if item.IsDir {
				ls.Directorycount++
			} else {
				ls.Filecount++
				ls.Bytesfound += item.Size
			}
		}
	}
	return ls, err
}

func filesInTgzArchive(filename string) (ListingSet, error) {
	var ls ListingSet
	var gzReader *gzip.Reader
	var tarReader *tar.Reader

	file, err := os.Open(filename)
	if err == nil {
		defer file.Close()
		gzReader, err = gzip.NewReader(file)
	}
	if err == nil {
		defer gzReader.Close()
		tarReader = tar.NewReader(gzReader)
	}
	if err != nil {
		if show_errors {
			fmt.Printf("Error: Could not open %s.  %s\n", filename, err.Error())
		}
		return ls, err
	}

	head, err := tarReader.Next()
	for head != nil && err == nil {
		var item fileitem = fileitem{filename, head.Name, head.Size, head.ModTime, false, head.FileInfo().Mode(), "", true, NONE}
		if fileMeetsConditions(item) {
			ls.MatchedFiles = append(ls.MatchedFiles, item)
			if item.IsDir {
				ls.Directorycount++
			} else {
				ls.Filecount++
				ls.Bytesfound += item.Size
			}
		}
		head, err = tarReader.Next()
	}
	return ls, err
}

func filesInDirectory(target string) ListingSet {
	var ls ListingSet
	var files []fs.DirEntry

	pFile, err := os.Open(target)
	if err == nil {
		defer pFile.Close()
		files, err = pFile.ReadDir(0)
	}
	// Iterate through all files, matching and then sort
	if err == nil {
		for _, f := range files {
			fi := makefileitem(f, target)
			if fileMeetsConditions(fi) {
				ls.MatchedFiles = append(ls.MatchedFiles, fi)
				if f.IsDir() {
					ls.Directorycount++
				} else {
					ls.Filecount++
					i, e := f.Info()
					if e == nil {
						ls.Bytesfound += i.Size()
					}
				}
			}
			// Must be outside of fileMeetsConditions().  Note we cannot use
			// filetype, because archives may be executable.
			if fi.IsArchive() && listInArchives {
				ls.Archives = append(ls.Archives, fi.Name)
			}
			if fi.IsDir && listdirectories && (listhidden || fi.Name[0] != '.') {
				ls.Subdirs = append(ls.Subdirs, fi.Name)
			}

		}
	}
	return ls
}

/******* Core Code *******/
// Recursive if necessary listing of files.
func list_directory(target string, recursed bool, isArchive bool) (err error) {
	var ls ListingSet

	conditionalPrint(debug_messages, "Analyzing directory %s\n", target)
	// Iterate through all files, matching and then sort
	if err == nil {
		if isArchive {
			switch FileIsArchiveType(target) {
			case ARCHIVE_ZIP:
				ls, err = filesInZipArchive(target, true)
				conditionalPrint(debug_messages, "Archive %s type zip\n", target)
			case ARCHIVE_TGZ:
				ls, err = filesInTgzArchive(target)
				conditionalPrint(debug_messages, "Archive %s type tgz\n", target)
			case ARCHIVE_7Z:
				ls, err = filesIn7ZArchive(target)
				conditionalPrint(debug_messages, "Archive %s type 7z\n", target)
			}
		} else {
			ls = filesInDirectory(target)
		}
	}
	if err == nil {
		sort.Slice(ls.MatchedFiles, func(i, j int) bool {
			first := ls.MatchedFiles[i]
			second := ls.MatchedFiles[j]
			firstName := ternaryString(case_sensitive, first.Name, strings.ToUpper(first.Name))
			secondName := ternaryString(case_sensitive, second.Name, strings.ToUpper(second.Name))
			if !sortby.ascending {
				first = ls.MatchedFiles[j]
				second = ls.MatchedFiles[i]
			}
			if (directories_first) && (first.IsDir != second.IsDir) {
				return first.IsDir
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
	TotalBytes += ls.Bytesfound
	TotalFiles += ls.Filecount
	// Output results.  Don't print directory header or footer if no files in a recursed directory
	if (!recursed || len(ls.MatchedFiles) > 0) && directory_header {
		fmt.Printf("\n   Directory of %s\n", target)
		if listfiles {
			fmt.Printf("\n")
		}
	}
	if listfiles || listdirectories {
		for _, f := range ls.MatchedFiles {
			fmt.Println(f.ToString())
		}
	}
	if (!recursed || len(ls.MatchedFiles) > 0) && size_calculations {
		fmt.Printf("   %4d Files (%s bytes) and %4d Directories.\n", ls.Filecount, FileSizeToString(ls.Bytesfound), ls.Directorycount)
		if !recursed {
			fmt.Printf("   %4d Total Files (%s Total Bytes) listed.", TotalFiles, FileSizeToString(TotalBytes))
		}
	}

	if listInArchives && len(ls.Archives) > 0 {
		conditionalPrint(debug_messages, "Listing in Archives %s\n", ls.Archives)
		sort.Strings(ls.Archives)
		for _, d := range ls.Archives {
			list_directory(filepath.Join(target, d), true, true)
		}
	}
	// Handle sub directories
	if recurse_directories {
		sort.Strings(ls.Subdirs)
		for _, d := range ls.Subdirs {
			list_directory(filepath.Join(target, d), true, false)
		}
	}
	return err
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
	list_directory(start_directory, false, pathIsArchive)
}
