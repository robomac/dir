# DIR
## A better "ls"
"dir" is a Go program for listing files, with some features from DOS/Windows and some conveniences thrown in.  Specifically, 

 - Like ls, dir support ANSI colors in the terminal.
 - Recursion into directories is more powerful
 - Additional file types are recognized, *and can be sorted on.*  So all of your archives land together, all of your configuration files are grouped, all Office files.
 - The ability to search for text is *integrated* into 'dir'.
 - And even to search for text in PDF and Microsoft Office (Word/Excel/PowerPoint) files.
	 - PDF Support requires pdftotext be present.
 - As is the ability to list files in most archives (zip/7z/tgz.)
 - And even the ability to have the text search work on files inside an archive.  (i.e. list all files with text "foobar", checking even those inside archives.)

So there's a lot to 'dir'.
## Installing it
The easy cross-platform way is to pull the repo, and run:
* go build
* go install

This will put it into your go/bin directory.  You *may* need to add that directory to your path.
For PDF searching, you'll want to [download the XpdfReader tools](https://www.xpdfreader.com/download.html).  These include [pdftotext](https://www.xpdfreader.com/pdftotext-man.html), which is used to extract text from PDF files.  (Microsoft Office files are [Open Office XML (OOXML)](https://en.wikipedia.org/wiki/Office_Open_XML) and are expanded internally to handle.)
pdftotext may be either in your path or placed alongside the dir executable.  The rest of the suite isn't needed or used.
## Using It
This is the help file:
dir, A better directory lister.
    dir {flags} {start path}{/}{filemask}

    Flags are denoted by -, but many can also be denoted, DOS-style, as switches with /

Filters:
    m{d|s}=v:v  Min/Max values for file data or size.  e.g. -md=2023-02-01:2023-03-31
        Only that date format is accepted; times are not accepted.
        If only one value and no colon is present, it will be the minimium.
        An empty value implies no bound, e.g. -ms=:500000 would look for files less than or 500000 bytes.
    t{c|i|r}=v text search - case sensitive, insensitive or regex.  Don't forget to disable globbing!
        Searches for the specified text in the files, only returning matching files.  This may be SLOW.
        If combined with -z, listing files inside archives, it will do a text scan on files in the archives,
        expanding MS Office and PDF files into $TEMP as necessary, which may also be slow.  
        So use -t{c|i|r} with -z cautiously.
        This is distinct from -t, which prints totals without filenames.
        e.g. dir -ti=rAt *.txt will find txt files with RAT, rat or any combination.
    x=v,v... (or exclude=) Comma-separated list of extensions to skip over.  E.g. avoid text-search on 
        MOV, MP4 files.  Case-insensitive.  This can make text searching a lot faster.

Visibility:
    d{+|-} = List Directories.  + is ONLY list directories, - exludes them.  Default is list files and directories.
    ah- = hide hidden files.  They are shown by default.

Recursion:
    r = recurse subdirectories (i.e. /s in MS-DOS.)
    z = recurse into archives (zip, tgz, tar.gz, 7z files.)  Not all archive formats are supported, 
        not all compression nor nested archives, and of course no support for encrypted archives.
        A single archive can be searched by specifying it, including the extension, plus a slash and the 
        file spec.
        e.g. dir -z foo.zip/* will list all files in foo.zip
        e.g. dir -z ~/Downloads/big.zip/readme* will find all readme* files in big.zip.
        e.g. dir -z ~/Downloads/readme*  will find all readme* files in all archives in Downloads.

Sort Order:
    o{-}{n|t|x|d|s} = sort order.  n = name, t = type, x = extension, d = modified, s = size
        - reverses the order to descending.  (This is -r in ls.)
        e.g. /o-n lists in reverse alpha.
        type lumps by extension classification, if found, and then by extension and name.

Output Formatting:
    s{c|h|r} = file size formatting.
        sc = Use commas as thousands-separators.  In ls, this is -,
        sh = Abbreviate the size to KB, MB or GB as appropriate.  In ls, this is -h.
        sr = Regular size listing - i.e. just the long number.

    G{|-|+} = Color output.  - = no colors, + is "enhanced", using additional file-type colors.
        + (enhanced) is the default if LS_COLORS is defined.  Regular (non-enhanced) would just be "-G"
           
           Note that this ignores extension-configuration of LS_COLORS, e.g. export LS_COLORS=$LS_COLORS:"*.ogg=01;35":"*.mp3=01;35"
           Instead we have a custom extension to it, ac for archives, au for audio and im for image/video files.

    b{+} = bare (filenames only, e.g. for use with xargs or other inputs), one per line.  
        b+ includes the path to the filename.
    t = Totals only, no filenames/listing.


Other output commands:
    debug == Print debug messages.    
    errors == show all error messages; usually they're quiet.
    version == print the version (probably the build date)

    Both -debug and -error should be first on the cmd line, as they don't take effect until parsed.

    Note, if you're coming from DOS, that you may have to quote wildcards to prevent zshell/bash from globbing (interpreting - also called expansion) them.
    Globbing is what lets ~ equate to $HOME, and a lot of other niceties, but zsh pretty aggressively does it by default
    Or preface the command with noglob, perhaps in an alias.  

Example:
    dir -r -z -x=mov,mp4,gif,jpeg,jpg -ti=christmas
        Would search from the current directory down for files with the word "Christmas" with any casing, SKIPPING movie/graphic files,
        but looking inside MS Office (DOCX), PDF and ZIP files.    
## Credits
'dir' imports, but does not distribute:
*  github.com/bodgit/sevenzip (BSD 3-Clause License)
* github.com/gobwas/glob (MIT License)
